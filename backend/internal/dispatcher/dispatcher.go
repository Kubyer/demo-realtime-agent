package dispatcher

import (
	"context"
	"log/slog"
	"sync"

	"github.com/demo-realtime-agent/voiceagent/internal/transport"
)

// AudioSource represents a named, cancellable audio stream.
type AudioSource struct {
	Audio   <-chan []byte
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
	BargeinCh    chan struct{}    // buffered 1; signal only
	ToolResultCh chan AudioSource // buffered 4
	FillerCh     chan AudioSource // buffered 4

	cancelMu      sync.Mutex
	currentCancel context.CancelFunc
	currentChunk  string

	log *slog.Logger
}

// EventBroadcaster allows the dispatcher to emit UI events without importing
// the events package (breaks circular imports). Only cancellation is needed
// here; playing events are broadcast by the session with actual text.
type EventBroadcaster interface {
	BroadcastCancelled(chunkID string)
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
			d.flushQueues()
			d.log.Info("dispatcher: barge-in detected")
			continue
		default:
		}

		// Tier 1 + 2: block until something arrives.
		select {
		case <-ctx.Done():
			d.stopCurrent()
			return

		// Barge-in re-checked here so we don't miss a signal that arrived
		// while we were between the two selects.
		case <-d.BargeinCh:
			d.cancelCurrent()
			d.flushQueues()
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

// play preempts the current stream and starts a new one for src.
// Uses a silent clear (bargein=false) so the UI does not flash the
// barge-in indicator for routine filler→TTS or TTS→TTS transitions.
func (d *AudioDispatcher) play(ctx context.Context, src AudioSource) {
	if chunkID := d.stopCurrent(); chunkID != "" {
		d.transport.ClearBuffer(false)
		if d.hub != nil {
			d.hub.BroadcastCancelled(chunkID)
		}
	}

	child, cancel := context.WithCancel(ctx)
	d.cancelMu.Lock()
	d.currentCancel = cancel
	d.currentChunk = src.ChunkID
	d.cancelMu.Unlock()

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

// stopCurrent cancels the active audio stream goroutine and returns its
// chunkID. Returns "" when nothing is playing. Does NOT send a transport
// clear or broadcast — callers decide whether this is a barge-in.
func (d *AudioDispatcher) stopCurrent() string {
	d.cancelMu.Lock()
	cancel := d.currentCancel
	chunkID := d.currentChunk
	d.currentCancel = nil
	d.currentChunk = ""
	d.cancelMu.Unlock()
	if cancel == nil {
		return ""
	}
	cancel()
	return chunkID
}

// cancelCurrent is the full barge-in path: stops the stream, sends a
// bargein-flagged clear to the transport, and broadcasts the cancellation.
// Idempotent: safe to call when nothing is playing.
func (d *AudioDispatcher) cancelCurrent() {
	chunkID := d.stopCurrent()
	if chunkID != "" {
		d.transport.ClearBuffer(true)
		if d.hub != nil {
			d.hub.BroadcastCancelled(chunkID)
		}
	}
}
// flushQueues drains ToolResultCh and FillerCh so stale audio queued before
// barge-in does not play after the interruption completes.
func (d *AudioDispatcher) flushQueues() {
	for {
		select {
		case src, ok := <-d.ToolResultCh:
			if !ok {
				return
			}
			d.log.Info("dispatcher: flushing stale tool result", "chunk_id", src.ChunkID)
			if d.hub != nil {
				d.hub.BroadcastCancelled(src.ChunkID)
			}
		case src, ok := <-d.FillerCh:
			if !ok {
				return
			}
			d.log.Info("dispatcher: flushing stale filler", "chunk_id", src.ChunkID)
		default:
			return
		}
	}
}
