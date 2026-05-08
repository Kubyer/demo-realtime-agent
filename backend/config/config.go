package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	SonioxAPIKey string
	SonioxWSURL  string // wss://stt-rt.eu.soniox.com/transcribe-websocket (EU region)

	GroqAPIKey string
	GroqModel  string

	ElevenLabsAPIKey  string
	ElevenLabsVoiceID string
	ElevenLabsModel   string

	CartesiaAPIKey string
	CartesiaWSURL  string

	GradiumAPIKey string
	GeminiAPIKey  string // optional; needed only when LLM provider = "gemini"

	DatabaseURL string // optional; falls back to in-memory store when empty

	// Twilio REST credentials — optional, only required for outbound calls.
	TwilioAccountSID string
	TwilioAuthToken  string
	TwilioFromNumber string // E.164 format, e.g. +33600000000

	HTTPPort string
	LogLevel string

	CalendlyAPIKey string
}

// Load charge d'abord le fichier .env à la racine du projet (chemin relatif ../
// ou répertoire courant), puis lit les variables d'environnement.
func Load() (*Config, error) {
	loadDotEnv()

	c := &Config{
		// SonioxWSURL : Soniox a migré de gRPC à WebSocket (plus de protoc nécessaire)
		SonioxWSURL: getEnvOrDefault("SONIOX_WS_URL", "wss://stt-rt.eu.soniox.com/transcribe-websocket"),
		// openai/gpt-oss-20b : ~1000 t/s on Groq, exceptionally fast,
		// handles complex tool calling reliably compared to smaller models.
		GroqModel:       getEnvOrDefault("GROQ_MODEL", "openai/gpt-oss-20b"),
		ElevenLabsModel: getEnvOrDefault("ELEVENLABS_MODEL", "eleven_flash_v2_5"),
		CartesiaWSURL:   getEnvOrDefault("CARTESIA_WS_URL", "wss://api.cartesia.ai/tts/websocket"),
		HTTPPort:        getEnvOrDefault("HTTP_PORT", "8080"),
		LogLevel:        getEnvOrDefault("LOG_LEVEL", "info"),
	}

	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.TwilioAccountSID = os.Getenv("TWILIO_ACCOUNT_SID")
	c.TwilioAuthToken = os.Getenv("TWILIO_AUTH_TOKEN")
	c.TwilioFromNumber = os.Getenv("TWILIO_FROM_NUMBER")
	c.CartesiaAPIKey = os.Getenv("CARTESIA_API_KEY")
	c.GradiumAPIKey = os.Getenv("GRADIUM_API_KEY")
	c.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")

	c.ElevenLabsVoiceID = getEnvOrDefault("ELEVENLABS_VOICE_ID", "3C1zYzXNXNzrB66ON8rj")

	required := map[string]*string{
		"SONIOX_API_KEY":     &c.SonioxAPIKey,
		"GROQ_API_KEY":       &c.GroqAPIKey,
		"ELEVENLABS_API_KEY": &c.ElevenLabsAPIKey,
		"CALENDLY_API_KEY":   &c.CalendlyAPIKey,
	}
	for key, ptr := range required {
		val := os.Getenv(key)
		if val == "" {
			return nil, fmt.Errorf("required env var %s is not set", key)
		}
		*ptr = val
	}

	return c, nil
}

// loadDotEnv tente de charger un fichier .env depuis plusieurs emplacements
// possibles (répertoire courant, parent, grand-parent) sans dépendance externe.
func loadDotEnv() {
	candidates := []string{
		".env",
		filepath.Join("..", ".env"),
		filepath.Join("..", "..", ".env"),
	}
	for _, path := range candidates {
		if err := parseDotEnvFile(path); err == nil {
			return
		}
	}
}

func parseDotEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Ignorer commentaires et lignes vides
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Supprimer le préfixe "export " si présent
		line = strings.TrimPrefix(line, "export ")

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Retirer les guillemets éventuels
		val = strings.Trim(val, `"'`)

		// Ne pas écraser une variable déjà définie dans l'environnement shell
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
