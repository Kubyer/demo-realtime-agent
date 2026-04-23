package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

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
	Type    string `json:"type"`
	Data    string `json:"data"` // base64-encoded PCM/mulaw chunk
	Done    bool   `json:"done"`
	ChunkID string `json:"context_id"`
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
func (c *Client) Stream(ctx context.Context, sentenceCh <-chan string) (<-chan []byte, error) {
	header := map[string][]string{
		"X-API-Key":        {c.apiKey},
		"Cartesia-Version": {"2024-06-10"},
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("cartesia dial: %w", err)
	}

	audioCh := make(chan []byte, 32)
	var wg sync.WaitGroup

	// Sender: sentence → Cartesia WS.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer conn.WriteMessage(websocket.CloseMessage, //nolint:errcheck
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

		contextID := "turn-1"
		first := true
		for {
			select {
			case sentence, ok := <-sentenceCh:
				if !ok {
					return
				}
				req := cartesiaRequest{
					ModelID:    "sonic-english",
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
			case <-ctx.Done():
				return
			}
		}
	}()

	// Receiver: Cartesia WS → audioCh.
	wg.Add(1)
	go func() {
		defer wg.Done()
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
			if resp.Type != "chunk" || resp.Data == "" {
				continue
			}
			audio, err := base64.StdEncoding.DecodeString(resp.Data)
			if err != nil {
				// Cartesia may send raw binary for some encodings.
				audio = []byte(resp.Data)
			}
			select {
			case audioCh <- audio:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(audioCh)
		conn.Close() //nolint:errcheck
	}()

	return audioCh, nil
}
