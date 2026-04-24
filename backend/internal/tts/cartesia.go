package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/gorilla/websocket"
)

// cartesiaRequest is the JSON payload sent to Cartesia's TTS WebSocket.
type cartesiaRequest struct {
	ModelID      string         `json:"model_id"`
	Transcript   string         `json:"transcript"`
	Voice        cartesiaVoice  `json:"voice"`
	OutputFormat cartesiaFormat `json:"output_format"`
	ContextID    string         `json:"context_id"`
	Continue     bool           `json:"continue"`
}

type cartesiaVoice struct {
	Mode string `json:"mode"`
	ID   string `json:"id"`
}

type cartesiaFormat struct {
	Container  string `json:"container"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

// cartesiaResponse is a chunk received from Cartesia.
type cartesiaResponse struct {
	Type       string `json:"type"`
	Data       string `json:"data"` // base64-encoded PCM/mulaw chunk
	Done       bool   `json:"done"`
	Error      string `json:"error,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
}

// Client streams text to Cartesia and returns audio chunks.
type Client struct {
	wsURL   string
	apiKey  string
	voiceID string
	log     *slog.Logger
}

func NewClient(wsURL, apiKey, voiceID string, log *slog.Logger) *Client {
	return &Client{wsURL: wsURL, apiKey: apiKey, voiceID: voiceID, log: log}
}

// Stream connects to Cartesia, sends sentences from sentenceCh, and writes
// received audio chunks to the returned channel. The channel is closed when
// all sentences are synthesised or ctx is cancelled.
//
// Design: the sender goroutine sends sentences without closing the WS (a
// premature CloseMessage would cause Cartesia to abort audio generation before
// streaming it back). The receiver exits when Cartesia signals done=true, then
// sends the CloseMessage itself.
func (c *Client) Stream(ctx context.Context, sentenceCh <-chan string) (<-chan []byte, error) {
	header := map[string][]string{
		"X-API-Key":        {c.apiKey},
		"Cartesia-Version": {"2024-06-10"},
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("cartesia dial: %w", err)
	}

	audioCh := make(chan []byte, 64)

	// Sender: sentences → Cartesia WS.
	// Cartesia protocol: all sentences except the last use continue:true (context stays
	// open for more text); the last sentence uses continue:false (signals end of input).
	// We use a one-item look-ahead buffer: hold the current sentence, send the previous
	// one when the next arrives, send the held sentence with continue:false on close.
	go func() {
		contextID := "turn-1"
		send := func(sentence string, cont bool) bool {
			req := cartesiaRequest{
				ModelID:    "sonic-2",
				Transcript: sentence,
				Voice:      cartesiaVoice{Mode: "id", ID: c.voiceID},
				OutputFormat: cartesiaFormat{
					Container:  "raw",
					Encoding:   "pcm_mulaw",
					SampleRate: 8000,
				},
				ContextID: contextID,
				Continue:  cont,
			}
			data, _ := json.Marshal(req)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				c.log.Warn("cartesia send", "err", err)
				return false
			}
			c.log.Debug("cartesia: sent sentence", "text", sentence, "continue", cont)
			return true
		}

		var pending string
		hasPending := false
		for {
			select {
			case sentence, ok := <-sentenceCh:
				if !ok {
					// Channel closed: flush pending sentence as the final one.
					if hasPending {
						send(pending, false)
					}
					return
				}
				// Send previously buffered sentence with continue:true (more is coming).
				if hasPending {
					if !send(pending, true) {
						return
					}
				}
				pending = sentence
				hasPending = true
			case <-ctx.Done():
				return
			}
		}
	}()

	// Receiver: Cartesia WS → audioCh.
	// Exits on done=true, then closes the connection cleanly.
	go func() {
		defer close(audioCh)
		defer conn.Close() //nolint:errcheck
		chunks := 0
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					c.log.Warn("cartesia recv", "err", err)
				}
				return
			}
			var resp cartesiaResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				c.log.Warn("cartesia unmarshal", "err", err, "raw", string(raw))
				continue
			}
			if resp.Done {
				if resp.Type == "error" || resp.Error != "" {
					c.log.Error("cartesia: error from API",
						"type", resp.Type,
						"status_code", resp.StatusCode,
						"error", resp.Error,
						"raw", string(raw),
					)
				} else if chunks == 0 {
					c.log.Warn("cartesia: done with 0 audio chunks", "raw", string(raw))
				} else {
					c.log.Debug("cartesia: done", "chunks", chunks)
				}
				conn.WriteMessage(websocket.CloseMessage, //nolint:errcheck
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if resp.Type != "chunk" || resp.Data == "" {
				c.log.Info("cartesia: non-chunk msg", "type", resp.Type, "raw", string(raw))
				continue
			}
			audio, err := base64.StdEncoding.DecodeString(resp.Data)
			if err != nil {
				audio = []byte(resp.Data)
			}
			chunks++
			c.log.Debug("cartesia: audio chunk", "bytes", len(audio), "n", chunks)
			select {
			case audioCh <- audio:
			case <-ctx.Done():
				return
			}
		}
	}()

	return audioCh, nil
}
