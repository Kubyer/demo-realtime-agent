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
	Prompt          string   `json:"prompt"`
	VoiceProvider   string   `json:"voice_provider"`
	VoiceID         string   `json:"voice_id"`
	VoiceModel      string   `json:"voice_model"`
	VoiceStability  *float64 `json:"el_stability,omitempty"`
	VoiceSimilarity *float64 `json:"el_similarity,omitempty"`
	VoiceStyle      *float64 `json:"el_style,omitempty"`
	VoiceSpeed      *float64 `json:"el_speed,omitempty"`
	OpeningSentence string   `json:"opening_sentence"`
	LLMProvider     string   `json:"llm_provider"`
	LLMModel        string   `json:"llm_model"`
}

func float64Ptr(v float64) *float64 { return &v }

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
- Tu écris les nombres en toutes lettres.
- Fais des phrases TRÈS COURTES (1 ou 2 phrases maximum par tour). Garde un rythme dynamique.

# INSTRUCTIONS DE COMPORTEMENT
- Sois chaleureux, naturel et efficace.
- Si l'utilisateur te donne un jour flou (ex: "la semaine prochaine"), demande-lui quel jour précis l'arrange le mieux avant de chercher.
- Une fois l'heure choisie, demande son prénom et son email pour finaliser la réservation.
- [RÈGLE ABSOLUE] Ne jamais inventer, supposer ou compléter le nom ou l'email de l'utilisateur. S'il ne les a pas fournis explicitement dans cette conversation, demande-les avant tout appel à book_meeting.

# UTILISATION DES OUTILS (TOOL CALLING)
Tu as accès à deux outils : check_availability (pour voir les créneaux) et book_meeting (pour réserver).
- [RÈGLE ABSOLUE] Il est FORMELLEMENT INTERDIT de confirmer, suggérer ou sous-entendre qu'un créneau est disponible sans avoir PRÉALABLEMENT appelé check_availability. Même si l'utilisateur propose un créneau précis ("14h30 lundi"), tu DOIS d'abord appeler check_availability avant de répondre.
- [RÈGLE ABSOLUE] Ne jamais inventer ou confirmer une date sans utiliser le calendrier des jours disponibles injecté dans ce prompt.
- [RÈGLE ABSOLUE] Quand tu confirmes une heure choisie, répète EXACTEMENT l'heure telle qu'elle apparaît dans le résultat de check_availability (ex : si le résultat dit "2026-05-12T14:30:00", dis "quatorze heures trente"). Ne recalcule jamais un chiffre depuis ce que l'utilisateur a dit — les erreurs de conversion numérique (ex: dire "quinze" au lieu de "quatorze") sont inacceptables.
- Quand tu utilises un outil, dis une courte phrase d'attente juste avant : "Je vérifie les disponibilités." ou "Je bloque le créneau pour vous."

# CONTEXTE DE L'APPEL
Tu viens de décrocher. C'est toi qui lances la conversation.
Phrase de départ obligatoire : "Bonjour, c'est Léa de Legalplace. Quel jour vous conviendrait pour un rendez-vous ?"`,
		VoiceProvider:   "elevenlabs",
		VoiceID:         "3C1zYzXNXNzrB66ON8rj",
		VoiceModel:      "eleven_turbo_v2_5",
		VoiceStability:  float64Ptr(0.35),
		VoiceSimilarity: float64Ptr(0.85),
		VoiceStyle:      float64Ptr(0.20),
		OpeningSentence: "Bonjour, c'est Léa de Legalplace. Quel jour vous conviendrait pour un rendez-vous ?",
		LLMProvider:     "groq",
		LLMModel:        "openai/gpt-oss-20b",
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
	callCfg    CallConfig    // immutable snapshot of all pipeline params
	qaStore    *CallQAStore  // nil = QA disabled
	log        *slog.Logger

	// Per-turn cancellation: barge-in kills the active LLM/TTS turn so the
	// main loop can pick up the user's new utterance immediately.
	turnCancelMu sync.Mutex
	turnCancel   context.CancelFunc
}

// Done returns a channel closed when the session has fully shut down.
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) setTurnCancel(cancel context.CancelFunc) {
	s.turnCancelMu.Lock()
	defer s.turnCancelMu.Unlock()
	s.turnCancel = cancel
}

func (s *Session) cancelCurrentTurn() {
	s.turnCancelMu.Lock()
	cancel := s.turnCancel
	s.turnCancel = nil
	s.turnCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

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
	CalendlyAPIKey string
	GeminiAPIKey   string
	DB             *sql.DB
	// Source is "twilio" or "browser"; determines STT audio format.
	Source  string
	QAStore *CallQAStore // nil = post-call QA disabled
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
	callCfg := buildCallConfig(cfg.Source, settings, cfg)
	recorder := NewRecorder(cfg.Source)

	sim := tools.NewSimulator(cfg.CalendlyAPIKey, hub)

	llmAPIKey, llmBaseURL := cfg.GroqAPIKey, ""
	if settings.LLMProvider == "gemini" {
		llmAPIKey = cfg.GeminiAPIKey
		llmBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
	}
	llmModel := settings.LLMModel
	if llmModel == "" {
		llmModel = cfg.GroqModel
	}
	groqClient := llm.NewGroqClient(llmAPIKey, llmModel, sim, llm.DefaultTools(), log, llmBaseURL)

	var ttsClient tts.Client
	if settings.VoiceProvider == "cartesia" {
		ttsClient = tts.NewCartesiaClient(cfg.CartesiaWSURL, cfg.CartesiaAPIKey, settings.VoiceID, log)
	} else if settings.VoiceProvider == "gradium" {
		gCfg := tts.GradiumConfig{
			APIKey:  cfg.GradiumAPIKey,
			VoiceID: settings.VoiceID,
		}
		ttsClient = tts.NewGradiumClientFromConfig(gCfg, log)
	} else if settings.VoiceProvider == "gemini_tts" {
		ttsClient = tts.NewGeminiTTSClient(tts.GeminiTTSConfig{
			APIKey:    cfg.GeminiAPIKey,
			Model:     settings.VoiceModel,
			VoiceName: settings.VoiceID,
		}, log)
	} else {
		elCfg := tts.ElevenLabsConfig{
			APIKey:          cfg.ElevenLabsAPIKey,
			VoiceID:         settings.VoiceID,
			Model:           settings.VoiceModel,
			Stability:       settings.VoiceStability,
			SimilarityBoost: settings.VoiceSimilarity,
			Style:           settings.VoiceStyle,
			Speed:           settings.VoiceSpeed,
		}
		ttsClient = tts.NewElevenLabsClientFromConfig(elCfg, log)
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
		callCfg:    callCfg,
		qaStore:    cfg.QAStore,
		log:        log,
	}

	calls.Start(id, cfg.Source)
	calls.SetConfig(id, callCfg)

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
		paths, _ := s.recorder.Save(s.ID)
		if s.qaStore != nil && paths.HasAny() {
			if rec, ok := s.calls.Get(s.ID); ok {
				s.qaStore.TriggerQA(s.ID, paths, rec, s.callCfg)
			}
		}
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

	// Fix #4: Separate contexts — sttCtx keeps STT alive for the full session.
	// playCtx is used by the LLM/TTS pipeline and can be cancelled on barge-in
	// without killing speech recognition.
	sttCtx, sttCancel := context.WithCancel(ctx)
	defer sttCancel()

	go s.dispatcher.Run(ctx)

	interimCh := make(chan stt.Result, 8)
	finalCh := make(chan stt.Result, 4)

	go func() {
		if err := s.stt.Stream(sttCtx, audioCh, interimCh, finalCh); err != nil && sttCtx.Err() == nil {
			s.log.Error("session: STT stream error", "err", err, "session_id", s.ID)
		}
	}()

	// Fix #1: Debounced barge-in — only trigger when interim text has ≥2 words.
	// This prevents coughs, "uhh", or background noise from killing the agent.
	go func() {
		for {
			select {
			case result, ok := <-interimCh:
				if !ok {
					return
				}
				if bargeinShouldFire(result.Text) {
					s.log.Info("session: barge-in triggered", "text", result.Text)
					s.dispatcher.SignalBargein()
					s.cancelCurrentTurn()
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Snapshot settings at session start.
	currentSettings := GetSettings()

	now := time.Now()
	daysFr := []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	monthsFr := []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	dateStr := fmt.Sprintf("%s %d %s %d", daysFr[now.Weekday()], now.Day(), monthsFr[now.Month()-1], now.Year())
	timeStr := now.Format("15:04")
	// Build a 14-day lookup table so the LLM never has to compute date arithmetic
	// (it consistently makes off-by-one errors for "lundi prochain" etc.).
	var calStr string
	for i := 1; i <= 14; i++ {
		d := now.AddDate(0, 0, i)
		calStr += fmt.Sprintf("\n  %s %d %s %d", daysFr[d.Weekday()], d.Day(), monthsFr[d.Month()-1], d.Year())
	}
	promptWithDate := currentSettings.Prompt +
		"\n\n# DATE ET HEURE ACTUELLES" +
		"\nAujourd'hui nous sommes le " + dateStr + " et il est " + timeStr + "." +
		"\nRÈGLE STRICTE : le mot 'aujourd'hui' désigne UNIQUEMENT le " + dateStr + "." +
		" Pour tout créneau situé à une autre date, utilise 'demain', 'après-demain' ou le nom du jour avec la date (ex: 'vendredi 8 mai')." +
		" Ne jamais confondre la date d'aujourd'hui avec la date d'un créneau proposé." +
		"\nProchains jours (référence — utilise ce tableau pour convertir 'lundi prochain', 'la semaine prochaine', etc.) :" +
		calStr
	
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

			// Per-turn context: cancelled by barge-in so the LLM/TTS pipeline
			// stops immediately and the main loop can start the new turn.
			turnCtx, turnCancel := context.WithCancel(ctx)
			s.setTurnCancel(turnCancel)

			// TTS goroutine: consume sentenceCh → ElevenLabs → dispatcher.
			go s.runTTSTurn(turnCtx, sentenceCh, chunkID, tSTTFinal, &ttfaMs)

			// Filler audio (priority 2) dispatched immediately while LLM thinks.
			go s.playFiller(ctx, chunkID+"-filler")

			// Intercept stream to broadcast tokens immediately.
			// Uses turnCtx-aware send so a cancelled turn doesn't leak goroutines.
			go func() {
				defer close(sentenceCh)
				var firstToken bool
				var fullText string
				for text := range rawCh {
					if !firstToken {
						firstToken = true
						ttftMs.Store(time.Since(tSTTFinal).Milliseconds())
					}
					select {
					case sentenceCh <- text:
					case <-turnCtx.Done():
						// drain rawCh so no goroutine blocks on a full channel
						for range rawCh {
						}
						return
					}
					fullText += text
					s.hub.BroadcastPlaying(chunkID, fullText, "assistant")
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

			// LLM stream — sends sentences to rawCh; blocks until done or cancelled.
			tLLMStart := time.Now()
			if err := s.llm.StreamLoop(turnCtx, history, rawCh); err != nil && ctx.Err() == nil && turnCtx.Err() == nil {
				s.log.Error("session: LLM error", "err", err, "session_id", s.ID)
			}
			close(rawCh)
			turnCancel()
			s.setTurnCancel(nil)

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

// bargeinShouldFire returns true when the interim transcript is substantial
// enough to justify interrupting the agent. Requiring ≥2 words prevents
// coughs, filler sounds ("uhh"), and background noise from triggering barge-in.
func bargeinShouldFire(text string) bool {
	words := 0
	inWord := false
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			inWord = false
		} else if !inWord {
			inWord = true
			words++
		}
	}
	return words >= 2
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
