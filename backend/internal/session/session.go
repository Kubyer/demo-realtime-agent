package session

import (
	"context"
	"log/slog"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/legalplace/voiceagent/internal/dispatcher"
	"github.com/legalplace/voiceagent/internal/events"
	"github.com/legalplace/voiceagent/internal/llm"
	"github.com/legalplace/voiceagent/internal/stt"
	"github.com/legalplace/voiceagent/internal/tools"
	"github.com/legalplace/voiceagent/internal/transport"
	"github.com/legalplace/voiceagent/internal/tts"
)

const systemPrompt = `Tu es un assistant IA vocal. Sois concis, clair et professionnel. Réponds toujours en français.`

// Session owns all goroutines and components for a single call leg.
type Session struct {
	ID     string
	cancel context.CancelFunc
	done   chan struct{} // closed when the run goroutine exits

	transport  transport.AudioTransport
	dispatcher *dispatcher.AudioDispatcher
	stt        *stt.Client
	llm        *llm.GroqClient
	tts        *tts.Client
	hub        *events.Hub
	log        *slog.Logger
}

// Done returns a channel closed when the session has fully shut down.
func (s *Session) Done() <-chan struct{} { return s.done }

// Config carries the per-session API credentials.
type Config struct {
	SonioxAPIKey    string
	SonioxWSURL     string
	GroqAPIKey      string
	GroqModel       string
	CartesiaAPIKey  string
	CartesiaWSURL   string
	CartesiaVoiceID string
}

// NewSession wires all components for a single call and starts the goroutine tree.
func NewSession(
	id string,
	tr transport.AudioTransport,
	hub *events.Hub,
	cfg Config,
	log *slog.Logger,
) *Session {
	ctx, cancel := context.WithCancel(context.Background())

	sim := tools.NewSimulator()
	groqClient := llm.NewGroqClient(cfg.GroqAPIKey, cfg.GroqModel, sim, llm.DefaultTools(), log)
	ttsClient := tts.NewClient(cfg.CartesiaWSURL, cfg.CartesiaAPIKey, cfg.CartesiaVoiceID, log)
	sttClient := stt.NewClient(cfg.SonioxAPIKey, cfg.SonioxWSURL, log)
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
		log:        log,
	}

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
	defer s.hub.BroadcastSessionEnd(s.ID)
	s.log.Info("session started", "session_id", s.ID)

	audioCh, err := s.transport.ReadStream(ctx)
	if err != nil {
		s.log.Error("session: ReadStream failed", "err", err, "session_id", s.ID)
		return
	}

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

	history := llm.NewHistory(systemPrompt)

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
			s.hub.BroadcastFinal(s.hub.NextChunkID(), result.Text)

			// sentenceCh carries text from the LLM to the TTS pipeline.
			sentenceCh := make(chan string, 4)
			chunkID := s.hub.NextChunkID()

			// TTS goroutine: consume sentenceCh → Cartesia → dispatcher.
			go s.runTTSTurn(ctx, sentenceCh, chunkID, tSTTFinal)

			// Filler audio (priority 2) dispatched immediately while LLM thinks.
			go s.playFiller(ctx, chunkID+"-filler")

			// LLM stream — sends sentences to sentenceCh; blocks until done.
			tLLMStart := time.Now()
			if err := s.llm.StreamLoop(ctx, history, sentenceCh); err != nil && ctx.Err() == nil {
				s.log.Error("session: LLM error", "err", err, "session_id", s.ID)
			}
			s.log.Info("session: LLM turn complete",
				"llm_duration_ms", time.Since(tLLMStart).Milliseconds(),
				"session_id", s.ID,
			)
			close(sentenceCh)

		case <-ctx.Done():
			return
		}
	}
}

// runTTSTurn connects to Cartesia, streams the sentence channel, and forwards
// the resulting audio to the dispatcher at tool-result priority (priority 1).
// tSTTFinal is the timestamp when the STT final result was received, used to
// compute end-to-end latency (STT → LLM TTFT → TTS first audio chunk).
func (s *Session) runTTSTurn(ctx context.Context, sentenceCh <-chan string, chunkID string, tSTTFinal time.Time) {
	// Use session ctx directly — a turn-scoped child context would cancel
	// Cartesia the moment this function returns (before any audio arrives).
	tTTSDial := time.Now()
	audioCh, err := s.tts.Stream(ctx, sentenceCh)
	if err != nil {
		s.log.Error("session: TTS dial error", "err", err)
		return
	}
	tTTSConnected := time.Now()
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

// fillerAudio returns a channel emitting pre-chunked mulaw 8kHz frames for
// "Laissez-moi regarder..." (currently: 200ms of mulaw silence as placeholder).
// Embed a real recording with //go:embed filler.raw when available.
func fillerAudio() <-chan []byte {
	ch := make(chan []byte, 16)
	go func() {
		defer close(ch)
		// 0x7f is the mulaw silence value; 160 bytes = 20ms at 8kHz.
		frame := make([]byte, 160)
		for i := range frame {
			frame[i] = 0x7f
		}
		for i := 0; i < 10; i++ { // 10 × 20ms = 200ms
			chunk := make([]byte, len(frame))
			copy(chunk, frame)
			ch <- chunk
		}
	}()
	return ch
}
