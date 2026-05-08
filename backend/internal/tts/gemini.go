package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// GeminiTTSConfig holds configuration for the Gemini TTS client.
type GeminiTTSConfig struct {
	APIKey    string
	Model     string // e.g. "gemini-2.5-flash-preview-tts"
	VoiceName string // e.g. "Aoede"
}

type GeminiTTSClient struct {
	cfg GeminiTTSConfig
	log *slog.Logger
}

func NewGeminiTTSClient(cfg GeminiTTSConfig, log *slog.Logger) *GeminiTTSClient {
	if cfg.VoiceName == "" {
		cfg.VoiceName = "Aoede"
	}
	return &GeminiTTSClient{cfg: cfg, log: log}
}

// gemini TTS request / response shapes
type geminiTTSReq struct {
	Contents         []geminiTTSContent `json:"contents"`
	GenerationConfig geminiTTSGenCfg    `json:"generationConfig"`
}

type geminiTTSContent struct {
	Parts []geminiTTSPart `json:"parts"`
	Role  string          `json:"role"`
}

type geminiTTSPart struct {
	Text string `json:"text"`
}

type geminiTTSGenCfg struct {
	ResponseModalities []string       `json:"responseModalities"`
	SpeechConfig       geminiSpeechCfg `json:"speechConfig"`
}

type geminiSpeechCfg struct {
	VoiceConfig geminiVoiceCfg `json:"voiceConfig"`
}

type geminiVoiceCfg struct {
	PrebuiltVoiceConfig geminiPrebuiltVoice `json:"prebuiltVoiceConfig"`
}

type geminiPrebuiltVoice struct {
	VoiceName string `json:"voiceName"`
}

type geminiTTSResp struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"` // base64 LINEAR16 PCM
				} `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Stream implements tts.Client: synthesises sentences one at a time and
// delivers µ-law 8 kHz audio chunks to the returned channel.
func (c *GeminiTTSClient) Stream(ctx context.Context, sentenceCh <-chan string) (<-chan []byte, error) {
	audioCh := make(chan []byte, 64)
	go func() {
		defer close(audioCh)
		for {
			select {
			case sentence, ok := <-sentenceCh:
				if !ok {
					return
				}
				if strings.TrimSpace(sentence) == "" {
					continue
				}
				pcm, err := c.synthesize(ctx, sentence)
				if err != nil {
					c.log.Warn("gemini tts: synthesis error", "err", err)
					continue
				}
				ulaw := pcm24kToUlaw8k(pcm)
				// Ship in ≤320-byte chunks (20 ms of µ-law at 8 kHz) so the
				// dispatcher can interleave barge-in cancellations.
				for len(ulaw) > 0 {
					n := min(320, len(ulaw))
					chunk := make([]byte, n)
					copy(chunk, ulaw[:n])
					ulaw = ulaw[n:]
					select {
					case audioCh <- chunk:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return audioCh, nil
}

func (c *GeminiTTSClient) synthesize(ctx context.Context, text string) ([]byte, error) {
	reqBody := geminiTTSReq{
		Contents: []geminiTTSContent{{
			Parts: []geminiTTSPart{{Text: text}},
			Role:  "user",
		}},
		GenerationConfig: geminiTTSGenCfg{
			ResponseModalities: []string{"AUDIO"},
			SpeechConfig: geminiSpeechCfg{
				VoiceConfig: geminiVoiceCfg{
					PrebuiltVoiceConfig: geminiPrebuiltVoice{VoiceName: c.cfg.VoiceName},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		c.cfg.Model, c.cfg.APIKey,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini tts: HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var gr geminiTTSResp
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, fmt.Errorf("gemini tts: decode response: %w", err)
	}
	for _, cand := range gr.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				return base64.StdEncoding.DecodeString(part.InlineData.Data)
			}
		}
	}
	return nil, fmt.Errorf("gemini tts: no audio data in response")
}

// pcm24kToUlaw8k converts signed 16-bit little-endian 24 kHz PCM to G.711
// µ-law 8 kHz by 3:1 decimation followed by µ-law compression.
func pcm24kToUlaw8k(pcm []byte) []byte {
	nSamples := len(pcm) / 2
	out := make([]byte, 0, nSamples/3+1)
	for i := 0; i+1 < len(pcm); i += 6 { // step 3 samples × 2 bytes
		s := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8)
		out = append(out, linearToUlaw(s))
	}
	return out
}

// segEnd holds the upper boundary of each G.711 µ-law segment.
var segEnd = [8]int{0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF, 0x1FFF, 0x3FFF, 0x7FFF}

// linearToUlaw encodes a signed 16-bit PCM sample as G.711 µ-law.
// Based on the Sun Microsystems reference implementation.
func linearToUlaw(s int16) byte {
	v := int(s)
	mask := 0xFF
	if v < 0 {
		mask = 0x7F
		v = ^v // bitwise NOT (not negation) for µ-law sign handling
	}
	if v > 32767 {
		v = 32767
	}
	v += 0x84 // add bias

	seg := 0
	for seg < 8 && v > segEnd[seg] {
		seg++
	}
	if seg == 8 {
		return byte(0x7F ^ mask)
	}
	return byte(((seg << 4) | ((v >> (seg + 3)) & 0x0F)) ^ mask)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
