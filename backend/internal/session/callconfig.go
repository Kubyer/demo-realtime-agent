package session

import (
	"crypto/sha256"
	"fmt"
)

// CallConfig is an immutable snapshot of every pipeline parameter active
// during a session. It is captured once at session start and stored alongside
// the transcript so every call record is fully reproducible.
//
// This is the primary input for voice A/B comparison: two calls with different
// TTSVoiceID or TTSStability values can be compared objectively via their
// QA scores.
type CallConfig struct {
	// Session transport
	Source string `json:"source"` // "browser" | "twilio"

	// STT — Soniox
	STTProvider    string `json:"stt_provider"`     // "soniox"
	STTModel       string `json:"stt_model"`        // "stt-rt-v4"
	STTAudioFormat string `json:"stt_audio_format"` // "pcm_s16le" | "mulaw"
	STTSampleRate  int    `json:"stt_sample_rate"`  // 16000 | 8000
	STTLanguage    string `json:"stt_language"`     // "fr"
	STTEndpoint    bool   `json:"stt_endpoint_detection"`

	// LLM — Groq
	LLMProvider string `json:"llm_provider"` // "groq"
	LLMModel    string `json:"llm_model"`    // e.g. "openai/gpt-oss-20b"

	// TTS — ElevenLabs / Cartesia / Gradium
	TTSProvider     string   `json:"tts_provider"`               // "elevenlabs" | "cartesia" | "gradium"
	TTSVoiceID      string   `json:"tts_voice_id"`
	TTSModel        string   `json:"tts_model,omitempty"`
	TTSStability    *float64 `json:"tts_stability,omitempty"`
	TTSSimilarity   *float64 `json:"tts_similarity_boost,omitempty"`
	TTSStyle        *float64 `json:"tts_style,omitempty"`
	TTSSpeed        *float64 `json:"tts_speed,omitempty"`

	// Prompt & conversation
	PromptHash      string `json:"prompt_hash"`               // first 16 hex chars of SHA-256
	OpeningSentence string `json:"opening_sentence,omitempty"`

	// Barge-in threshold (words required before interruption fires)
	BargeinMinWords int `json:"bargein_min_words"`
}

// buildCallConfig captures the full parameter set at session start from the
// active Settings and the per-session Config (API credentials + LLM model).
func buildCallConfig(source string, settings Settings, cfg Config) CallConfig {
	cc := CallConfig{
		Source:          source,
		STTProvider:     "soniox",
		STTModel:        "stt-rt-v4",
		STTLanguage:     "fr",
		STTEndpoint:     true,
		LLMProvider:     settings.LLMProvider,
		LLMModel:        settings.LLMModel,
		TTSProvider:     settings.VoiceProvider,
		TTSVoiceID:      settings.VoiceID,
		TTSModel:        settings.VoiceModel,
		PromptHash:      promptHash(settings.Prompt),
		OpeningSentence: settings.OpeningSentence,
		BargeinMinWords: 2,
	}

	if source == "browser" {
		cc.STTAudioFormat = "pcm_s16le"
		cc.STTSampleRate = 16000
	} else {
		cc.STTAudioFormat = "mulaw"
		cc.STTSampleRate = 8000
	}

	// Capture ElevenLabs voice parameters when ElevenLabs is active.
	if settings.VoiceProvider == "elevenlabs" {
		cc.TTSStability = settings.VoiceStability
		cc.TTSSimilarity = settings.VoiceSimilarity
		cc.TTSStyle = settings.VoiceStyle
		cc.TTSSpeed = settings.VoiceSpeed
	}

	return cc
}

func promptHash(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars — enough to detect changes
}
