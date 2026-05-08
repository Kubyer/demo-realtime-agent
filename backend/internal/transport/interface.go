package transport

import "context"

// AudioTransport abstracts the bidirectional audio channel between the server
// and the call medium (Twilio, browser, etc.).
type AudioTransport interface {
	// ReadStream returns a channel of raw audio frames (mulaw 8kHz).
	// The implementation owns the goroutine writing to the channel.
	// Cancel ctx to stop streaming and release resources.
	ReadStream(ctx context.Context) (<-chan []byte, error)

	// WriteStream consumes audio frames from the provided channel and forwards
	// them to the underlying transport. Blocks until ctx is cancelled or audio
	// channel is closed.
	WriteStream(ctx context.Context, audio <-chan []byte) error

	// ClearBuffer sends an out-of-band signal to the transport to discard any
	// audio it has buffered but not yet played. bargein=true allows transports
	// to signal a user-initiated interruption vs. a routine audio swap.
	// Safe to call from any goroutine.
	ClearBuffer(bargein bool)
}
