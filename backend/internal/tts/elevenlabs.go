package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/gorilla/websocket"
)

// ElevenLabsConfig groups all configurable parameters for the ElevenLabs TTS client.
type ElevenLabsConfig struct {
	APIKey          string
	VoiceID         string
	Model           string
	Stability       *float64 // 0–1, nil = provider default (0.5)
	SimilarityBoost *float64 // 0–1, nil = provider default (0.75)
	Style           *float64 // 0–1, nil = provider default (0.0)
	Speed           *float64 // 0.7–1.2, nil = provider default (1.0)
}

type elevenLabsRequest struct {
	Text                 string               `json:"text"`
	VoiceSettings        *elevenLabsVoiceConf `json:"voice_settings,omitempty"`
	TryTriggerGeneration bool                 `json:"try_trigger_generation,omitempty"`
}

type elevenLabsVoiceConf struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Style           float64 `json:"style"`
	Speed           float64 `json:"speed"`
}

type elevenLabsResponse struct {
	Audio   string `json:"audio"` // base64-encoded PCM/ulaw chunk
	IsFinal bool   `json:"isFinal"`
}

type ElevenLabsClient struct {
	cfg ElevenLabsConfig
	log *slog.Logger
}

// NewElevenLabsClient creates a client with the minimal required parameters.
func NewElevenLabsClient(apiKey, voiceID, model string, log *slog.Logger) *ElevenLabsClient {
	return &ElevenLabsClient{cfg: ElevenLabsConfig{APIKey: apiKey, VoiceID: voiceID, Model: model}, log: log}
}

// NewElevenLabsClientFromConfig creates a client from a full config struct.
func NewElevenLabsClientFromConfig(cfg ElevenLabsConfig, log *slog.Logger) *ElevenLabsClient {
	return &ElevenLabsClient{cfg: cfg, log: log}
}

func (c *ElevenLabsClient) voiceConf() *elevenLabsVoiceConf {
	if c.cfg.Stability == nil && c.cfg.SimilarityBoost == nil && c.cfg.Style == nil && c.cfg.Speed == nil {
		return nil
	}
	vc := &elevenLabsVoiceConf{
		Stability:       0.5,
		SimilarityBoost: 0.75,
		Style:           0.0,
		Speed:           1.0,
	}
	if c.cfg.Stability != nil {
		vc.Stability = *c.cfg.Stability
	}
	if c.cfg.SimilarityBoost != nil {
		vc.SimilarityBoost = *c.cfg.SimilarityBoost
	}
	if c.cfg.Style != nil {
		vc.Style = *c.cfg.Style
	}
	if c.cfg.Speed != nil {
		vc.Speed = *c.cfg.Speed
	}
	return vc
}

func (c *ElevenLabsClient) Stream(ctx context.Context, sentenceCh <-chan string) (<-chan []byte, error) {
	wsURL := fmt.Sprintf(
		"wss://api.elevenlabs.io/v1/text-to-speech/%s/stream-input?model_id=%s&output_format=ulaw_8000",
		c.cfg.VoiceID, c.cfg.Model,
	)

	header := map[string][]string{
		"xi-api-key": {c.cfg.APIKey},
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
			VoiceSettings:        c.voiceConf(),
			TryTriggerGeneration: true,
		}
		data, _ := json.Marshal(req)
		conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck

		for {
			select {
			case sentence, ok := <-sentenceCh:
				if !ok {
					endReq := elevenLabsRequest{Text: ""}
					data, _ := json.Marshal(endReq)
					conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck
					return
				}
				r := elevenLabsRequest{Text: sentence + " "}
				data, _ := json.Marshal(r)
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
