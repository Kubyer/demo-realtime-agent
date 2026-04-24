package dispatcher

import (
	"context"
	"log/slog"
	"sync"

	"github.com/demo-realtime-agent/voiceagent/internal/transport"
)

// AudioSource represents a named, cancellable audio stream.
type AudioSource struct {
	Audio  <-chan []byte
	ChunkID string
}

// AudioDispatcher is the speculative execution core. It arbitrates between
// competing audio sources at three priority levels and owns the single
// WriteStream call on the transport.
//
// Priority ladder (highest first):
//
//	0 — Barge-in: human started speaking → cancel everything, ClearBuffer.
//	1 — Tool result audio → override any filler playing.
//	2 — Filler audio ("Laissez-moi regarder...") → plays while LLM thinks.
type AudioDispatcher struct {
	transport transport.AudioTransport
	hub       EventBroadcaster

	// Input signals — written by external goroutines.
	BargeinCh     chan struct{}   // buffered 1; signal only
	ToolResultCh  chan AudioSource // buffered 4
	FillerCh      chan AudioSource // buffered 4

	cancelMu      sync.Mutex
	currentCancel context.CancelFunc
	currentChunk  string

	log *slog.Logger
}

// EventBroadcaster allows the dispatcher to emit UI events without importing
// the events package (breaks circular imports).
type EventBroadcaster interface {
	BroadcastCancelled(chunkID string)
	BroadcastPlaying(chunkID, text string)
}

func New(t transport.AudioTransport, hub EventBroadcaster, log *slog.Logger) *AudioDispatcher {
	return &AudioDispatcher{
		transport:    t,
		hub:          hub,
		BargeinCh:    make(chan struct{}, 1),
		ToolResultCh: make(chan AudioSource, 4),
		FillerCh:     make(chan AudioSource, 4),
		log:          log,
	}
}

// Run starts the dispatch loop. It blocks until ctx is cancelled.
// Call this in a dedicated goroutine.
func (d *AudioDispatcher) Run(ctx context.Context) {
	for {
		// Tier 0: non-blocking barge-in check (highest priority).
		select {
		case <-d.BargeinCh:
			d.cancelCurrent()
			d.log.Info("dispatcher: barge-in detected")
			continue
		default:
		}

		// Tier 1 + 2: block until something arrives.
		select {
		case <-ctx.Done():
			d.cancelCurrent()
			return

		// Barge-in re-checked here so we don't miss a signal that arrived
		// while we were between the two selects.
		case <-d.BargeinCh:
			d.cancelCurrent()
			d.log.Info("dispatcher: barge-in detected (tier1)")

		case src, ok := <-d.ToolResultCh:
			if !ok {
				return
			}
			d.log.Info("dispatcher: tool result audio", "chunk_id", src.ChunkID)
			d.play(ctx, src)

		case src, ok := <-d.FillerCh:
			if !ok {
				return
			}
			// Only start filler if nothing is currently playing.
			d.cancelMu.Lock()
			isIdle := d.currentCancel == nil
			d.cancelMu.Unlock()
			if isIdle {
				d.log.Info("dispatcher: filler audio", "chunk_id", src.ChunkID)
				d.play(ctx, src)
			}
		}
	}
}

// SignalBargein is the thread-safe entry point for barge-in detection.
// Non-blocking: if the channel is full the signal is already pending.
func (d *AudioDispatcher) SignalBargein() {
	select {
	case d.BargeinCh <- struct{}{}:
	default:
	}
}

// play cancels the current stream, then starts a new one for the given source.
func (d *AudioDispatcher) play(ctx context.Context, src AudioSource) {
	d.cancelCurrent()

	child, cancel := context.WithCancel(ctx)
	d.cancelMu.Lock()
	d.currentCancel = cancel
	d.currentChunk = src.ChunkID
	d.cancelMu.Unlock()

	if d.hub != nil {
		d.hub.BroadcastPlaying(src.ChunkID, "")
	}

	go func() {
		defer func() {
			d.cancelMu.Lock()
			// Only nil out if we are still the current stream.
			if d.currentChunk == src.ChunkID {
				d.currentCancel = nil
				d.currentChunk = ""
			}
			d.cancelMu.Unlock()
		}()
		if err := d.transport.WriteStream(child, src.Audio); err != nil {
			if err != context.Canceled {
				d.log.Warn("dispatcher: write stream error", "chunk_id", src.ChunkID, "err", err)
			}
		}
	}()
}

// cancelCurrent cancels the active audio stream and clears the transport
// buffer. Idempotent: safe to call when nothing is playing.
func (d *AudioDispatcher) cancelCurrent() {
	d.cancelMu.Lock()
	cancel := d.currentCancel
	chunkID := d.currentChunk
	d.currentCancel = nil
	d.currentChunk = ""
	d.cancelMu.Unlock()

	if cancel != nil {
		cancel()
		d.transport.ClearBuffer()
		if d.hub != nil && chunkID != "" {
			d.hub.BroadcastCancelled(chunkID)
		}
	}
}
