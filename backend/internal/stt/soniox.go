// Package stt provides a Soniox real-time speech-to-text client using the
// WebSocket API (wss://stt-rt.soniox.com/transcribe-websocket).
package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Wire types (WebSocket JSON protocol)
// ---------------------------------------------------------------------------

// startConfig is the first message sent to Soniox to open a transcription session.
// Authentication is done via the Authorization header, not here.
type startConfig struct {
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

// Client is a Soniox WebSocket STT client for a single call session.
type Client struct {
	apiKey string
	wsURL  string
	log    *slog.Logger
}

func NewClient(apiKey, wsURL string, log *slog.Logger) *Client {
	return &Client{apiKey: apiKey, wsURL: wsURL, log: log}
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

	// --- Connect with Authorization header ---
	t0 := time.Now()
	headers := http.Header{
		"Authorization": {"Bearer " + c.apiKey},
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, headers)
	if err != nil {
		return fmt.Errorf("soniox dial: %w", err)
	}
	defer conn.Close()
	c.log.Info("stt: connected", "latency_ms", time.Since(t0).Milliseconds())

	// --- Send initial config (no api_key in body) ---
	cfg := startConfig{
		Model:                   "stt-rt-v4",
		AudioFormat:             "mulaw",
		SampleRate:              8000,
		NumChannels:             1,
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
			r := Result{Text: finalTextBuf + interimText, IsFinal: false}
			select {
			case interimCh <- r:
			case <-ctx.Done():
				<-senderDone
				return ctx.Err()
			}
		}

		// Emit final when all tokens in this response are final.
		if allFinal && finalTextBuf != "" {
			r := Result{Text: finalTextBuf, IsFinal: true}
			select {
			case finalCh <- r:
			case <-ctx.Done():
				<-senderDone
				return ctx.Err()
			}
			// Reset for next utterance after endpoint detection.
			finalTextBuf = ""
			firstFinal = true
		}
	}

	<-senderDone
	return nil
}
