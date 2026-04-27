package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/gorilla/websocket"
)

type elevenLabsRequest struct {
	Text                 string `json:"text"`
	TryTriggerGeneration bool   `json:"try_trigger_generation,omitempty"`
}

type elevenLabsResponse struct {
	Audio   string `json:"audio"` // base64-encoded PCM/ulaw chunk
	IsFinal bool   `json:"isFinal"`
}

type Client struct {
	apiKey  string
	voiceID string
	model   string
	log     *slog.Logger
}

func NewClient(apiKey, voiceID, model string, log *slog.Logger) *Client {
	return &Client{apiKey: apiKey, voiceID: voiceID, model: model, log: log}
}

func (c *Client) Stream(ctx context.Context, sentenceCh <-chan string) (<-chan []byte, error) {
	wsURL := fmt.Sprintf("wss://api.elevenlabs.io/v1/text-to-speech/%s/stream-input?model_id=%s&output_format=ulaw_8000", c.voiceID, c.model)
	
	header := map[string][]string{
		"xi-api-key": {c.apiKey},
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs dial: %w", err)
	}

	audioCh := make(chan []byte, 64)

	// Sender: sentences → ElevenLabs WS
	go func() {
		// ElevenLabs needs an initial text block to start generation.
		req := elevenLabsRequest{
			Text:                 " ",
			TryTriggerGeneration: true,
		}
		data, _ := json.Marshal(req)
		conn.WriteMessage(websocket.TextMessage, data)

		for {
			select {
			case sentence, ok := <-sentenceCh:
				if !ok {
					// Empty text signals the end of input
					endReq := elevenLabsRequest{Text: ""}
					data, _ := json.Marshal(endReq)
					conn.WriteMessage(websocket.TextMessage, data)
					return
				}
				
				req := elevenLabsRequest{Text: sentence + " "}
				data, _ := json.Marshal(req)
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					c.log.Warn("elevenlabs send", "err", err)
					return
				}
				c.log.Debug("elevenlabs: sent sentence", "text", sentence)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Receiver: ElevenLabs WS → audioCh
	go func() {
		defer close(audioCh)
		defer conn.Close() //nolint:errcheck
		chunks := 0
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					c.log.Warn("elevenlabs recv", "err", err)
				}
				return
			}
			
			var resp elevenLabsResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				// Handle potential error format from ElevenLabs
				var errResp map[string]interface{}
				if json.Unmarshal(raw, &errResp) == nil {
					if _, hasError := errResp["error"]; hasError {
						c.log.Error("elevenlabs: error from API", "raw", string(raw))
						return
					}
				}
				c.log.Warn("elevenlabs unmarshal", "err", err, "raw", string(raw))
				continue
			}

			if resp.Audio != "" {
				audio, err := base64.StdEncoding.DecodeString(resp.Audio)
				if err == nil {
					chunks++
					c.log.Debug("elevenlabs: audio chunk", "bytes", len(audio), "n", chunks)
					select {
					case audioCh <- audio:
					case <-ctx.Done():
						return
					}
				}
			}

			if resp.IsFinal {
				c.log.Debug("elevenlabs: done", "chunks", chunks)
				conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")) //nolint:errcheck
				return
			}
		}
	}()

	return audioCh, nil
}
