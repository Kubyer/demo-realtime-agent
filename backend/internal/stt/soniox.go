// Package stt provides a Soniox real-time speech-to-text client using the
// WebSocket API (wss://stt-rt.soniox.com/transcribe-websocket).
package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Wire types (WebSocket JSON protocol)
// ---------------------------------------------------------------------------

// startConfig is the first message sent to Soniox to open a transcription session.
type startConfig struct {
	APIKey                  string   `json:"api_key"`
	Model                   string   `json:"model"`
	AudioFormat             string   `json:"audio_format"`
	SampleRate              int      `json:"sample_rate"`
	NumChannels             int      `json:"num_channels"`
	EnableEndpointDetection bool     `json:"enable_endpoint_detection"`
	LanguageHints           []string `json:"language_hints,omitempty"`
}

// token is a single word/sub-word unit in a Soniox response.
type token struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
}

// sonioxResponse is the message received from Soniox.
// error_code arrives as a number (int) — using RawMessage avoids unmarshal failures.
type sonioxResponse struct {
	Tokens       []token         `json:"tokens"`
	Finished     bool            `json:"finished"`
	ErrorCode    json.RawMessage `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
}

// hasError returns true if the response carries a non-null, non-zero error_code.
func (r *sonioxResponse) hasError() bool {
	if len(r.ErrorCode) == 0 {
		return false
	}
	s := string(r.ErrorCode)
	return s != "null" && s != "0" && s != `""`
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Result carries a transcription result from Soniox.
type Result struct {
	Text      string
	IsFinal   bool
	StartTime float64 // seconds; kept for compat
}

// AudioConfig describes the raw audio format sent to Soniox.
type AudioConfig struct {
	Format      string // "mulaw" | "pcm_s16le"
	SampleRate  int    // e.g. 8000 | 16000
	NumChannels int    // typically 1
}

// TwilioAudio is the default config for Twilio media streams (G.711 µ-law 8 kHz).
var TwilioAudio = AudioConfig{Format: "mulaw", SampleRate: 8000, NumChannels: 1}

// BrowserAudio is the config for browser AudioWorklet streams (PCM s16le 16 kHz).
var BrowserAudio = AudioConfig{Format: "pcm_s16le", SampleRate: 16000, NumChannels: 1}

// Client is a Soniox WebSocket STT client for a single call session.
type Client struct {
	apiKey   string
	wsURL    string
	audioCfg AudioConfig
	log      *slog.Logger
}

func NewClient(apiKey, wsURL string, audioCfg AudioConfig, log *slog.Logger) *Client {
	return &Client{apiKey: apiKey, wsURL: wsURL, audioCfg: audioCfg, log: log}
}

// Stream starts a WebSocket stream for the lifetime of ctx.
// Reads audio from audioCh; sends results to interimCh and finalCh.
// Both result channels are closed when the stream ends.
func (c *Client) Stream(
	ctx context.Context,
	audioCh <-chan []byte,
	interimCh chan<- Result,
	finalCh chan<- Result,
) error {
	defer close(interimCh)
	defer close(finalCh)

	// --- Connect ---
	t0 := time.Now()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, nil)
	if err != nil {
		return fmt.Errorf("soniox dial: %w", err)
	}
	defer conn.Close()
	c.log.Info("stt: connected", "latency_ms", time.Since(t0).Milliseconds())

	// --- Send initial config with api_key in JSON body (Soniox protocol) ---
	cfg := startConfig{
		APIKey:                  c.apiKey,
		Model:                   "stt-rt-v4",
		AudioFormat:             c.audioCfg.Format,
		SampleRate:              c.audioCfg.SampleRate,
		NumChannels:             c.audioCfg.NumChannels,
		EnableEndpointDetection: true,
		LanguageHints:           []string{"fr"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("soniox marshal config: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("soniox send config: %w", err)
	}
	c.log.Debug("stt: config sent", "config", string(data))

	// --- Sender goroutine: audio chunks → Soniox ---
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		for {
			select {
			case chunk, ok := <-audioCh:
				if !ok {
					// Empty binary frame signals end-of-audio to the server.
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
					c.log.Warn("stt: send error", "err", err)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// --- Receiver: Soniox → result channels ---
	var (
		finalTextBuf string
		firstFinal   = true
	)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			if ctx.Err() != nil {
				break
			}
			c.log.Warn("stt: recv error", "err", err)
			break
		}

		var resp sonioxResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			// Log raw message to help debug unexpected formats.
			c.log.Warn("stt: unmarshal error", "err", err, "raw", string(raw))
			continue
		}

		if resp.hasError() {
			c.log.Error("stt: server error",
				"code", string(resp.ErrorCode),
				"msg", resp.ErrorMessage,
				"raw", string(raw),
			)
			break
		}

		if resp.Finished {
			break
		}

		var interimText string
		allFinal := true

		for _, tok := range resp.Tokens {
			if tok.Text == "" {
				continue
			}
			if tok.IsFinal {
				finalTextBuf += tok.Text
				if firstFinal {
					firstFinal = false
					c.log.Info("stt: first_final_token",
						"latency_ms", time.Since(t0).Milliseconds(),
					)
				}
			} else {
				allFinal = false
				interimText += tok.Text
			}
		}

		// Emit interim.
		if interimText != "" {
			text := strings.TrimSpace(strings.ReplaceAll(finalTextBuf+interimText, "<end>", ""))
			if text != "" {
				r := Result{Text: text, IsFinal: false}
				select {
				case interimCh <- r:
				case <-ctx.Done():
					<-senderDone
					return ctx.Err()
				}
			}
		}

		// Emit final when all tokens in this response are final.
		if allFinal && finalTextBuf != "" {
			text := strings.TrimSpace(strings.ReplaceAll(finalTextBuf, "<end>", ""))
			if text != "" {
				r := Result{Text: text, IsFinal: true}
				select {
				case finalCh <- r:
				case <-ctx.Done():
					<-senderDone
					return ctx.Err()
				}
			}
			// Reset for next utterance after endpoint detection.
			finalTextBuf = ""
			firstFinal = true
		}
	}

	<-senderDone
	return nil
}
