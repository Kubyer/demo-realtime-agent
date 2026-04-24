package session

import (
	"log/slog"
	"sync"

	"github.com/demo-realtime-agent/voiceagent/internal/events"
	"github.com/demo-realtime-agent/voiceagent/internal/transport"
)

// Manager maintains the active session map and enforces one session per
// stream SID.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	cfg      Config
	hub      *events.Hub
	log      *slog.Logger
}

func NewManager(cfg Config, hub *events.Hub, log *slog.Logger) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		cfg:      cfg,
		hub:      hub,
		log:      log,
	}
}

// Create registers a new session for the given stream SID.
// If a session already exists for that SID (duplicate connection), it is
// stopped first.
func (m *Manager) Create(id string, tr transport.AudioTransport) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.sessions[id]; ok {
		m.log.Warn("session manager: duplicate SID, stopping existing", "session_id", id)
		existing.Stop()
	}

	s := NewSession(id, tr, m.hub, m.cfg, m.log)
	m.sessions[id] = s
	m.log.Info("session manager: created", "session_id", id, "active", len(m.sessions))
	return s
}

// Stop cancels and removes a session by ID.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return
	}
	s.Stop()
	delete(m.sessions, id)
	m.log.Info("session manager: stopped", "session_id", id, "active", len(m.sessions))
}

// ActiveCount returns the number of live sessions.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
