package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/demo-realtime-agent/voiceagent/internal/dispatcher"
	"github.com/demo-realtime-agent/voiceagent/internal/events"
	"github.com/demo-realtime-agent/voiceagent/internal/llm"
	"github.com/demo-realtime-agent/voiceagent/internal/stt"
	"github.com/demo-realtime-agent/voiceagent/internal/tools"
	"github.com/demo-realtime-agent/voiceagent/internal/transport"
	"github.com/demo-realtime-agent/voiceagent/internal/tts"
)

// ---------------------------------------------------------------------------
// Dynamic settings — shared across Twilio and browser sessions.
// ---------------------------------------------------------------------------

type Settings struct {
	Prompt          string `json:"prompt"`
	VoiceProvider   string `json:"voice_provider"`
	VoiceID         string `json:"voice_id"`
	VoiceModel      string `json:"voice_model"`
	OpeningSentence string `json:"opening_sentence"`
}

var (
	settingsMu     sync.RWMutex
	activeSettings = Settings{
		Prompt: `Tu es Léa, l'assistant vocal intelligent de Legalplace.
Ton objectif est d'aider l'utilisateur à réserver un appel ou une démonstration avec notre équipe.

# RÈGLES DE FORMATAGE STRICTES (POUR LA SYNTHÈSE VOCALE)
- TU PARLES À L'ORAL. Tes réponses seront lues par un synthétiseur vocal.
- INTERDICTION ABSOLUE d'utiliser du Markdown (*, **, #, ` + "`" + `).
- INTERDICTION ABSOLUE de faire des listes à puces ou numérotées. Fais des phrases fluides.
- N'utilise AUCUN emoji.
- Tu écris les nombres en toute lettre.
- Fais des phrases TRÈS COURTES (1 ou 2 phrases maximum par tour). Garde un rythme dynamique.

# INSTRUCTIONS DE COMPORTEMENT
- Sois chaleureux, naturel et efficace.
- Ne propose JAMAIS de date ou d'heure au hasard. Tu dois TOUJOURS utiliser tes outils pour vérifier le calendrier.
- Si l'utilisateur te donne un jour flou (ex: "la semaine prochaine"), demande-lui quel jour l'arrange le plus avant de chercher.
- Une fois l'heure choisie, demande-lui son prénom et son email pour finaliser la réservation.

# UTILISATION DES OUTILS (TOOL CALLING)
Tu as accès à deux outils : 'check_availability' (pour voir les créneaux) et 'book_meeting' (pour réserver).
- [RÈGLE CRITIQUE] : Quand tu décides d'utiliser un outil, tu DOIS dire une phrase d'attente très courte juste avant.
- Si tu utilises 'check_availability', dis UNIQUEMENT : "Laissez-moi regarder le calendrier." ou "Je vérifie les disponibilités."
- Si tu utilises 'book_meeting', dis UNIQUEMENT : "Je bloque le créneau pour vous." ou "Je valide la réservation."

# CONTEXTE DE L'APPEL
Tu viens de décrocher. C'est toi qui lances la conversation.
Phrase de départ obligatoire : "Bonjour, c'est Léa. Je peux vous aider à planifier une discussion avec notre équipe, quel jour vous arrangerait ?"`,
		VoiceProvider:   "elevenlabs",
		VoiceID:         "3C1zYzXNXNzrB66ON8rj",
		VoiceModel:      "eleven_flash_v2_5",
		OpeningSentence: "Bonjour, c'est Léa. Je peux vous aider à planifier une discussion avec notre équipe, quel jour vous arrangerait ?",
	}
)

// GetSettings returns the current settings.
func GetSettings() Settings {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return activeSettings
}

// SetSettings updates the settings; takes effect on the next call.
func SetSettings(s Settings) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	activeSettings = s
}

// GetSystemPrompt returns the current system prompt (for backwards compatibility).
func GetSystemPrompt() string {
	return GetSettings().Prompt
}

// SetSystemPrompt updates the system prompt (for backwards compatibility).
func SetSystemPrompt(p string) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	activeSettings.Prompt = p
}

// ---------------------------------------------------------------------------
// Session
// ---------------------------------------------------------------------------

// Session owns all goroutines and components for a single call leg.
type Session struct {
	ID     string
	cancel context.CancelFunc
	done   chan struct{} // closed when the run goroutine exits

	transport  transport.AudioTransport
	dispatcher *dispatcher.AudioDispatcher
	stt        *stt.Client
	llm        *llm.GroqClient
	tts        tts.Client
	hub        *events.Hub
	calls      CallStorer
	recorder   *Recorder
	log        *slog.Logger
}

// Done returns a channel closed when the session has fully shut down.
func (s *Session) Done() <-chan struct{} { return s.done }

// Config carries the per-session API credentials and transport options.
type Config struct {
	SonioxAPIKey      string
	SonioxWSURL       string
	GroqAPIKey        string
	GroqModel         string
	ElevenLabsAPIKey  string
	ElevenLabsVoiceID string
	ElevenLabsModel   string
	CartesiaAPIKey    string
	CartesiaWSURL     string
	GradiumAPIKey     string
	CalendlyAPIKey    string
	DB                *sql.DB
	// Source is "twilio" or "browser"; determines STT audio format.
	Source string
}

// NewSession wires all components for a single call and starts the goroutine tree.
func NewSession(
	id string,
	tr transport.AudioTransport,
	hub *events.Hub,
	calls CallStorer,
	cfg Config,
	log *slog.Logger,
) *Session {
	ctx, cancel := context.WithCancel(context.Background())

	var audioCfg stt.AudioConfig
	if cfg.Source == "browser" {
		audioCfg = stt.BrowserAudio
	} else {
		audioCfg = stt.TwilioAudio
	}

	settings := GetSettings()
	recorder := NewRecorder(cfg.Source)

	sim := tools.NewSimulator(cfg.CalendlyAPIKey, hub)
	groqClient := llm.NewGroqClient(cfg.GroqAPIKey, cfg.GroqModel, sim, llm.DefaultTools(), log)

	var ttsClient tts.Client
	if settings.VoiceProvider == "cartesia" {
		ttsClient = tts.NewCartesiaClient(cfg.CartesiaWSURL, cfg.CartesiaAPIKey, settings.VoiceID, log)
	} else if settings.VoiceProvider == "gradium" {
		gCfg := tts.GradiumConfig{
			APIKey: cfg.GradiumAPIKey,
			VoiceID: settings.VoiceID,
		}
		ttsClient = tts.NewGradiumClientFromConfig(gCfg, log)
	} else {
		ttsClient = tts.NewElevenLabsClient(cfg.ElevenLabsAPIKey, settings.VoiceID, settings.VoiceModel, log)
	}
	sttClient := stt.NewClient(cfg.SonioxAPIKey, cfg.SonioxWSURL, audioCfg, log)
	disp := dispatcher.New(tr, hub, log)

	s := &Session{
		ID:         id,
		cancel:     cancel,
		done:       make(chan struct{}),
		transport:  tr,
		dispatcher: disp,
		stt:        sttClient,
		llm:        groqClient,
		tts:        ttsClient,
		hub:        hub,
		calls:      calls,
		recorder:   recorder,
		log:        log,
	}

	calls.Start(id, cfg.Source)

	go func() {
		defer close(s.done)
		s.run(ctx)
	}()
	return s
}

// Stop cancels all goroutines for this session.
func (s *Session) Stop() { s.cancel() }

func (s *Session) run(ctx context.Context) {
	s.hub.BroadcastSessionStart(s.ID)
	defer func() {
		s.hub.BroadcastSessionEnd(s.ID)
		s.calls.End(s.ID)
		s.recorder.Save(s.ID)
	}()
	s.log.Info("session started", "session_id", s.ID)

	audioCh, err := s.transport.ReadStream(ctx)
	if err != nil {
		s.log.Error("session: ReadStream failed", "err", err, "session_id", s.ID)
		return
	}
	var recordCh <-chan []byte
	audioCh, recordCh = teeAudio(ctx, audioCh)
	go func() {
		for chunk := range recordCh {
			s.recorder.WriteUser(chunk)
		}
	}()

	go s.dispatcher.Run(ctx)

	interimCh := make(chan stt.Result, 8)
	finalCh := make(chan stt.Result, 4)

	go func() {
		if err := s.stt.Stream(ctx, audioCh, interimCh, finalCh); err != nil && ctx.Err() == nil {
			s.log.Error("session: STT stream error", "err", err, "session_id", s.ID)
		}
	}()

	// Signal barge-in on every interim STT result while audio is playing.
	go func() {
		for {
			select {
			case _, ok := <-interimCh:
				if !ok {
					return
				}
				s.dispatcher.SignalBargein()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Snapshot settings at session start.
	currentSettings := GetSettings()
	
	now := time.Now()
	days := []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	months := []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	dateStr := fmt.Sprintf("%s %d %s %d", days[now.Weekday()], now.Day(), months[now.Month()-1], now.Year())
	timeStr := now.Format("15:04")
	promptWithDate := currentSettings.Prompt + "\n\n# DATE ET HEURE ACTUELLES\nNous sommes le " + dateStr + " et il est " + timeStr + ". Utilise cette information si l'utilisateur te demande 'On est quel jour' ou parle de dates relatives."
	
	history := llm.NewHistory(promptWithDate)

	// If an opening sentence is configured, speak it immediately.
	if currentSettings.OpeningSentence != "" {
		s.log.Info("session: sending opening sentence", "text", currentSettings.OpeningSentence)

		history.Append(openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: currentSettings.OpeningSentence,
		})
		s.calls.AppendTurn(s.ID, TurnEntry{Role: "assistant", Text: currentSettings.OpeningSentence, AudioStartMs: s.recorder.OffsetMs()})

		chunkID := s.hub.NextChunkID()
		s.hub.BroadcastFinal(chunkID, currentSettings.OpeningSentence, "assistant")

		sentenceCh := make(chan string, 1)
		sentenceCh <- currentSettings.OpeningSentence
		close(sentenceCh)

		// Priority 1 TTS for the opening sentence.
		go s.runTTSTurn(ctx, sentenceCh, chunkID, time.Now(), nil)
	}

	for {
		select {
		case result, ok := <-finalCh:
			if !ok {
				s.log.Info("session: finalCh closed, ending session", "session_id", s.ID)
				return
			}
			tSTTFinal := time.Now()
			s.log.Info("session: STT final",
				"text", result.Text,
				"session_id", s.ID,
				"stt_final_at_ms", tSTTFinal.UnixMilli(),
			)

			history.Append(openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: result.Text,
			})
			s.hub.BroadcastFinal(s.hub.NextChunkID(), result.Text, "user")
			s.calls.AppendTurn(s.ID, TurnEntry{Role: "user", Text: result.Text, AudioStartMs: s.recorder.OffsetMs()})

			// sentenceCh carries text from the LLM to the TTS pipeline.
			sentenceCh := make(chan string, 4)
			rawCh := make(chan string, 16)
			chunkID := s.hub.NextChunkID()

			var ttfaMs atomic.Int64
			var ttftMs atomic.Int64
			startIdx := len(history.Snapshot())

			// TTS goroutine: consume sentenceCh → Cartesia → dispatcher.
			go s.runTTSTurn(ctx, sentenceCh, chunkID, tSTTFinal, &ttfaMs)

			// Filler audio (priority 2) dispatched immediately while LLM thinks.
			go s.playFiller(ctx, chunkID+"-filler")

			// Intercept stream to broadcast tokens immediately
			go func() {
				defer close(sentenceCh)
				var firstToken bool
				var fullText string
				for text := range rawCh {
					if !firstToken {
						firstToken = true
						ttftMs.Store(time.Since(tSTTFinal).Milliseconds())
					}
					sentenceCh <- text
					fullText += text
					s.hub.BroadcastPlaying(chunkID, fullText, "assistant") // Progressive broadcast
				}
				
				// Wait a bit for TTS to set TTFA if needed
				for i := 0; i < 50; i++ {
					if ttfaMs.Load() != 0 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				finalTtfa := ttfaMs.Load()
				finalTtft := ttftMs.Load()
				
				s.hub.BroadcastMetrics(events.MetricsPayload{
					TTFTMs: finalTtft,
					TTFAMs: finalTtfa,
					E2EMs:  finalTtfa,
				})
				
				s.hub.BroadcastFinal(chunkID, fullText, "assistant")
			}()

			// LLM stream — sends sentences to rawCh; blocks until done.
			tLLMStart := time.Now()
			if err := s.llm.StreamLoop(ctx, history, rawCh); err != nil && ctx.Err() == nil {
				s.log.Error("session: LLM error", "err", err, "session_id", s.ID)
			}
			close(rawCh)

			// Wait a bit to let TTS update ttfaMs if very fast
			time.Sleep(10 * time.Millisecond)

			s.log.Info("session: LLM turn complete",
				"llm_duration_ms", time.Since(tLLMStart).Milliseconds(),
				"session_id", s.ID,
			)

			// Record the assistant turn from the last history entry.
			s.recordAssistantTurn(history, chunkID, startIdx, &ttfaMs, &ttftMs)

		case <-ctx.Done():
			return
		}
	}
}

// recordAssistantTurn reads the most recent assistant message from history
// and appends it to the call transcript.
func (s *Session) recordAssistantTurn(history *llm.History, chunkID string, startIdx int, ttfaMs *atomic.Int64, ttftMs *atomic.Int64) {
	msgs := history.Snapshot()
	for i := len(msgs) - 1; i >= startIdx; i-- {
		m := msgs[i]
		if m.Role == openai.ChatMessageRoleAssistant && m.Content != "" {
			var lat, ttft, e2e *int64
			latVal := ttfaMs.Load()
			if latVal != 0 {
				lat = &latVal
				e2e = &latVal
			}
			ttftVal := ttftMs.Load()
			if ttftVal != 0 {
				ttft = &ttftVal
			}
			s.calls.AppendTurn(s.ID, TurnEntry{
				Role: "assistant", Text: m.Content, AudioStartMs: s.recorder.OffsetMs(),
				TTSLatency: lat, TTFTMs: ttft, E2EMs: e2e,
			})
			return
		}
	}
}

// runTTSTurn connects to Cartesia, streams the sentence channel, and forwards
// the resulting audio to the dispatcher at tool-result priority (priority 1).
func (s *Session) runTTSTurn(ctx context.Context, sentenceCh <-chan string, chunkID string, tSTTFinal time.Time, ttfaMs *atomic.Int64) {
	tTTSDial := time.Now()
	audioCh, err := s.tts.Stream(ctx, sentenceCh)
	if err != nil {
		s.log.Error("session: TTS dial error", "err", err)
		return
	}
	tTTSConnected := time.Now()
	ms := tTTSConnected.Sub(tSTTFinal).Milliseconds()
	if ttfaMs != nil {
		ttfaMs.Store(ms)
	}

	var recordCh <-chan []byte
	audioCh, recordCh = teeAudio(ctx, audioCh)
	go func() {
		for chunk := range recordCh {
			s.recorder.WriteAssistant(chunk)
		}
	}()

	s.log.Info("session: TTS connected",
		"chunk_id", chunkID,
		"tts_dial_ms", tTTSConnected.Sub(tTTSDial).Milliseconds(),
		"stt_to_tts_connect_ms", tTTSConnected.Sub(tSTTFinal).Milliseconds(),
	)

	src := dispatcher.AudioSource{Audio: audioCh, ChunkID: chunkID}
	select {
	case s.dispatcher.ToolResultCh <- src:
		s.log.Info("session: audio queued to dispatcher",
			"chunk_id", chunkID,
			"e2e_latency_ms", time.Since(tSTTFinal).Milliseconds(),
		)
	case <-ctx.Done():
	}
}

// playFiller sends pre-generated silence / filler mulaw audio to the dispatcher
// at the lowest priority so it plays only if nothing else is queued.
func (s *Session) playFiller(ctx context.Context, chunkID string) {
	src := dispatcher.AudioSource{Audio: fillerAudio(), ChunkID: chunkID}
	select {
	case s.dispatcher.FillerCh <- src:
	case <-ctx.Done():
	}
}

// fillerAudio returns a channel emitting pre-chunked mulaw 8kHz frames
// (200ms of silence as placeholder until a real recording is embedded).
func fillerAudio() <-chan []byte {
	ch := make(chan []byte, 16)
	go func() {
		defer close(ch)
		frame := make([]byte, 160)
		for i := range frame {
			frame[i] = 0x7f // mulaw silence
		}
		for i := 0; i < 10; i++ { // 10 × 20ms = 200ms
			chunk := make([]byte, len(frame))
			copy(chunk, frame)
			ch <- chunk
		}
	}()
	return ch
}
