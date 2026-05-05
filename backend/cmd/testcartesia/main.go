package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/demo-realtime-agent/voiceagent/config"
	"github.com/demo-realtime-agent/voiceagent/internal/tts"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("config error:", err)
		os.Exit(1)
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	voiceID := os.Getenv("CARTESIA_VOICE_ID")
	if voiceID == "" {
		voiceID = "a0e99841-438c-4a64-b679-ae501e7d6091" // example voice
	}
	client := tts.NewCartesiaClient(cfg.CartesiaWSURL, cfg.CartesiaAPIKey, voiceID, log)

	sentenceCh := make(chan string, 4)
	sentenceCh <- "Bonjour !"
	sentenceCh <- "Comment puis-je vous aider aujourd'hui ?"
	sentenceCh <- "Je suis là pour répondre à vos questions."
	close(sentenceCh)

	audioCh, err := client.Stream(context.Background(), sentenceCh)
	if err != nil {
		fmt.Println("stream error:", err)
		os.Exit(1)
	}

	total := 0
	for chunk := range audioCh {
		total += len(chunk)
	}
	fmt.Printf("Total audio bytes received: %d\n", total)
	if total == 0 {
		fmt.Println("ERROR: Cartesia returned 0 bytes — check logs above for the error message")
		os.Exit(1)
	}
}
