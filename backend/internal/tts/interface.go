package tts

import "context"

// Client is the interface for TTS streaming clients
type Client interface {
	Stream(ctx context.Context, sentenceCh <-chan string) (<-chan []byte, error)
}
