package events

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// EventType enumerates all UI event kinds.
type EventType string

const (
	EventTranscript   EventType = "transcript"
	EventSessionStart EventType = "session_start"
	EventSessionEnd   EventType = "session_end"
	EventToolCall     EventType = "tool_call"
	EventToolResult   EventType = "tool_result"
	EventMetrics      EventType = "metrics"
	EventError        EventType = "error"
)

// Status values for transcript events.
type Status string

const (
	StatusPlaying   Status = "playing"
	StatusCancelled Status = "cancelled"
	StatusFinal     Status = "final"
)

// MetricsPayload carries per-turn latency numbers.
type MetricsPayload struct {
	TTFTMs int64 `json:"ttft_ms"` // STT final → first LLM sentence sent to TTS
	TTFAMs int64 `json:"ttfa_ms"` // STT final → TTS WebSocket connected
	E2EMs  int64 `json:"e2e_ms"`  // STT final → audio queued to dispatcher
}

// ToolCallPayload carries the name and JSON arguments of a tool invocation.
type ToolCallPayload struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result,omitempty"`
}

// Event is the JSON payload sent to browser clients.
type Event struct {
	Type     EventType        `json:"type"`
	ChunkID  string           `json:"chunk_id,omitempty"`
	Text     string           `json:"text,omitempty"`
	Status   Status           `json:"status,omitempty"`
	Role     string           `json:"role,omitempty"` // "user" | "assistant"
	Metrics  *MetricsPayload  `json:"metrics,omitempty"`
	ToolCall *ToolCallPayload `json:"tool_call,omitempty"`
	Time     int64            `json:"ts"` // unix millis
}

type client struct {
	conn *websocket.Conn
	send chan []byte // buffered; full → drop-slow-reader
}

const clientSendBuf = 32

// Hub is a fan-out WebSocket broadcaster. One Run goroutine serialises all
// map access; per-client write goroutines own their WebSocket sends.
type Hub struct {
	register   chan *client
	unregister chan *client
	broadcast  chan []byte
	clients    map[*client]bool
	log        *slog.Logger
	chunkSeq   atomic.Uint64
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{
		register:   make(chan *client, 8),
		unregister: make(chan *client, 8),
		broadcast:  make(chan []byte, 64),
		clients:    make(map[*client]bool),
		log:        log,
	}
}

// Run processes registrations and broadcasts. Call in a dedicated goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
			h.log.Info("events hub: client connected", "total", len(h.clients))

		case c := <-h.unregister:
			if h.clients[c] {
				delete(h.clients, c)
				close(c.send)
				h.log.Info("events hub: client disconnected", "total", len(h.clients))
			}

		case msg := <-h.broadcast:
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// Drop-slow-reader: remove client to avoid head-of-line blocking.
					delete(h.clients, c)
					close(c.send)
					h.log.Warn("events hub: slow client dropped")
				}
			}
		}
	}
}

// Register adds a WebSocket connection to the hub and starts its write pump.
// Returns a cleanup function to call when the connection closes.
func (h *Hub) Register(conn *websocket.Conn) func() {
	c := &client{conn: conn, send: make(chan []byte, clientSendBuf)}
	h.register <- c

	go func() {
		defer func() { h.unregister <- c }()
		for msg := range c.send {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				h.log.Warn("events hub: write error", "err", err)
				return
			}
		}
	}()

	return func() { h.unregister <- c }
}

func (h *Hub) broadcast_(ev Event) {
	ev.Time = time.Now().UnixMilli()
	data, err := json.Marshal(ev)
	if err != nil {
		h.log.Warn("events hub: marshal error", "err", err)
		return
	}
	select {
	case h.broadcast <- data:
	default:
		h.log.Warn("events hub: broadcast channel full, dropping event")
	}
}

// BroadcastPlaying emits a playing transcript event with actual text.
func (h *Hub) BroadcastPlaying(chunkID, text, role string) {
	h.broadcast_(Event{Type: EventTranscript, ChunkID: chunkID, Text: text, Status: StatusPlaying, Role: role})
}

// BroadcastCancelled satisfies dispatcher.EventBroadcaster.
func (h *Hub) BroadcastCancelled(chunkID string) {
	h.broadcast_(Event{Type: EventTranscript, ChunkID: chunkID, Status: StatusCancelled})
}

// BroadcastFinal emits a finalised transcript segment.
func (h *Hub) BroadcastFinal(chunkID, text, role string) {
	h.broadcast_(Event{Type: EventTranscript, ChunkID: chunkID, Text: text, Status: StatusFinal, Role: role})
}

// BroadcastMetrics emits per-turn latency numbers.
func (h *Hub) BroadcastMetrics(m MetricsPayload) {
	h.broadcast_(Event{Type: EventMetrics, Metrics: &m})
}

// BroadcastSessionStart emits a session lifecycle event.
func (h *Hub) BroadcastSessionStart(sessionID string) {
	h.broadcast_(Event{Type: EventSessionStart, ChunkID: sessionID})
}

// BroadcastSessionEnd emits a session lifecycle event.
func (h *Hub) BroadcastSessionEnd(sessionID string) {
	h.broadcast_(Event{Type: EventSessionEnd, ChunkID: sessionID})
}

// BroadcastToolCall emits a real-time tool_call event so the UI can show
// which Calendly API is being invoked.
func (h *Hub) BroadcastToolCall(name, args string) {
	h.broadcast_(Event{
		Type:     EventToolCall,
		ToolCall: &ToolCallPayload{Name: name, Arguments: args},
	})
}

// BroadcastToolResult emits the result of a tool call to the UI.
func (h *Hub) BroadcastToolResult(name, result string) {
	h.broadcast_(Event{
		Type:     EventToolResult,
		ToolCall: &ToolCallPayload{Name: name, Result: result},
	})
}

// NextChunkID returns a monotonically increasing chunk identifier.
func (h *Hub) NextChunkID() string {
	id := h.chunkSeq.Add(1)
	return fmt.Sprintf("chunk-%d", id)
}
