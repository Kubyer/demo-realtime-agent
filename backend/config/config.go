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

	CartesiaAPIKey  string
	CartesiaWSURL   string
	CartesiaVoiceID string

	DatabaseURL string // optional; falls back to in-memory store when empty

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
		// openai/gpt-oss-20b : ~950 t/s sur LPU Groq — MoE 20B actifs,
		// meilleur raisonnement juridique + tool calling vs llama-3.1-8b-instant (700 t/s)
		GroqModel:     getEnvOrDefault("GROQ_MODEL", "openai/gpt-oss-20b"),
		CartesiaWSURL: getEnvOrDefault("CARTESIA_WS_URL", "wss://api.cartesia.ai/tts/websocket"),
		HTTPPort:      getEnvOrDefault("HTTP_PORT", "8080"),
		LogLevel:      getEnvOrDefault("LOG_LEVEL", "info"),
	}

	c.DatabaseURL = os.Getenv("DATABASE_URL")

	// CARTESIA_VOICE_ID peut être fourni directement ou via CARTESIA_FEMALE (alias)
	if v := os.Getenv("CARTESIA_VOICE_ID"); v != "" {
		c.CartesiaVoiceID = v
	} else if v := os.Getenv("CARTESIA_FEMALE"); v != "" {
		c.CartesiaVoiceID = v
	}

	required := map[string]*string{
		"SONIOX_API_KEY":   &c.SonioxAPIKey,
		"GROQ_API_KEY":     &c.GroqAPIKey,
		"CARTESIA_API_KEY": &c.CartesiaAPIKey,
		"CALENDLY_API_KEY": &c.CalendlyAPIKey,
	}
	for key, ptr := range required {
		val := os.Getenv(key)
		if val == "" {
			return nil, fmt.Errorf("required env var %s is not set", key)
		}
		*ptr = val
	}

	if c.CartesiaVoiceID == "" {
		return nil, fmt.Errorf("required env var CARTESIA_VOICE_ID (or CARTESIA_FEMALE) is not set")
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
