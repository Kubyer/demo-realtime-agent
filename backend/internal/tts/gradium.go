package tts

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
)

type GradiumConfig struct {
	APIKey       string
	VoiceID      string
	Temp         *float64
	CfgCoef      *float64
	PaddingBonus *float64
}

type GradiumClient struct {
	cfg GradiumConfig
	log *slog.Logger
}

func NewGradiumClientFromConfig(cfg GradiumConfig, log *slog.Logger) *GradiumClient {
	return &GradiumClient{
		cfg: cfg,
		log: log.With("component", "tts_gradium"),
	}
}

type gradiumTextMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type gradiumEndOfStreamMsg struct {
	Type string `json:"type"`
}

func (c *GradiumClient) Stream(ctx context.Context, sentenceCh <-chan string) (<-chan []byte, error) {
	url := "wss://api.gradium.ai/api/speech/tts"
	headers := http.Header{}
	headers.Set("x-api-key", c.cfg.APIKey)

	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		return nil, fmt.Errorf("gradium ws dial: %w", err)
	}

	setupMap := map[string]interface{}{
		"type":          "setup",
		"model_name":    "default",
		"voice_id":      c.cfg.VoiceID,
		"output_format": "mulaw_8000",
	}

	if c.cfg.Temp != nil || c.cfg.CfgCoef != nil || c.cfg.PaddingBonus != nil {
		jc := map[string]interface{}{}
		if c.cfg.Temp != nil {
			jc["temp"] = *c.cfg.Temp
		}
		if c.cfg.CfgCoef != nil {
			jc["cfg_coef"] = *c.cfg.CfgCoef
		}
		if c.cfg.PaddingBonus != nil {
			jc["padding_bonus"] = *c.cfg.PaddingBonus
		}
		setupMap["json_config"] = jc
	}

	audioOut := make(chan []byte, 32)

	// Sender
	go func() {
		defer conn.Close()

		if err := conn.WriteJSON(setupMap); err != nil {
			c.log.Error("gradium setup failed", "err", err)
			return
		}

		for sentence := range sentenceCh {
			if sentence == "" {
				continue
			}
			msg := gradiumTextMsg{Type: "text", Text: sentence + " "}
			if err := conn.WriteJSON(msg); err != nil {
				c.log.Error("gradium write text failed", "err", err)
				return
			}
		}

		_ = conn.WriteJSON(gradiumEndOfStreamMsg{Type: "end_of_stream"})
	}()

	// Receiver
	go func() {
		defer close(audioOut)
		for {
			var resp struct {
				Type  string `json:"type"`
				Audio string `json:"audio"`
				Error string `json:"message"`
			}
			if err := conn.ReadJSON(&resp); err != nil {
				return
			}

			if resp.Type == "error" {
				c.log.Error("gradium returned error", "msg", resp.Error)
				return
			}

			if resp.Type == "audio" && resp.Audio != "" {
				data, err := base64.StdEncoding.DecodeString(resp.Audio)
				if err == nil && len(data) > 0 {
					audioOut <- data
				}
			}

			if resp.Type == "end_of_stream" {
				return
			}
		}
	}()

	return audioOut, nil
}
