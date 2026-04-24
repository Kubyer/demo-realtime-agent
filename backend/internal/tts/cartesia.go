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
	Type string `json:"type"`
	Data string `json:"data"` // base64-encoded PCM/mulaw chunk
	Done bool   `json:"done"`
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
	// Does NOT send a CloseMessage — that is the receiver's job after done=true,
	// so Cartesia is not interrupted mid-generation.
	go func() {
		contextID := "turn-1"
		first := true
		for {
			select {
			case sentence, ok := <-sentenceCh:
				if !ok {
					return
				}
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
					Continue:  !first,
				}
				first = false
				data, _ := json.Marshal(req)
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					c.log.Warn("cartesia send", "err", err)
					return
				}
				c.log.Debug("cartesia: sent sentence", "text", sentence)
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
				c.log.Warn("cartesia unmarshal", "err", err)
				continue
			}
			if resp.Done {
				c.log.Debug("cartesia: done")
				// Graceful close after all audio is received.
				conn.WriteMessage(websocket.CloseMessage, //nolint:errcheck
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if resp.Type != "chunk" || resp.Data == "" {
				c.log.Debug("cartesia: non-chunk msg", "type", resp.Type)
				continue
			}
			audio, err := base64.StdEncoding.DecodeString(resp.Data)
			if err != nil {
				audio = []byte(resp.Data)
			}
			c.log.Debug("cartesia: audio chunk", "bytes", len(audio))
			select {
			case audioCh <- audio:
			case <-ctx.Done():
				return
			}
		}
	}()

	return audioCh, nil
}
