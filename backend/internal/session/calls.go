package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TurnEntry is one user or assistant turn in a call transcript.
type TurnEntry struct {
	Role         string `json:"role"` // "user" | "assistant"
	Text         string `json:"text"`
	Ts           int64  `json:"ts"` // unix millis
	AudioStartMs int64  `json:"audio_start_ms"`
	TTSLatency   *int64 `json:"tts_latency,omitempty"`
	TTFTMs       *int64 `json:"ttft_ms,omitempty"`
	E2EMs        *int64 `json:"e2e_ms,omitempty"`
}

// CallRecord holds metadata and transcript for a single call session.
type CallRecord struct {
	ID         string      `json:"id"`
	Source     string      `json:"source"`             // "twilio" | "browser"
	Status     string      `json:"status"`             // "ongoing" | "done"
	StartedAt  int64       `json:"started_at"`         // unix millis
	EndedAt    *int64      `json:"ended_at,omitempty"` // unix millis
	Transcript []TurnEntry `json:"transcript"`
}

// CallStorer is implemented by both the in-memory store and the Postgres store.
type CallStorer interface {
	Start(id, source string)
	AppendTurn(id string, entry TurnEntry)
	End(id string)
	List() []CallRecord
	Get(id string) (CallRecord, bool)
}

// ---------------------------------------------------------------------------
// In-memory store (default when DATABASE_URL is not set)
// ---------------------------------------------------------------------------

type CallStore struct {
	mu      sync.RWMutex
	records []*CallRecord
	index   map[string]*CallRecord
}

func NewCallStore() *CallStore {
	return &CallStore{
		records: make([]*CallRecord, 0, 32),
		index:   make(map[string]*CallRecord),
	}
}

func (s *CallStore) Start(id, source string) {
	r := &CallRecord{
		ID:         id,
		Source:     source,
		Status:     "ongoing",
		StartedAt:  time.Now().UnixMilli(),
		Transcript: make([]TurnEntry, 0),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	s.index[id] = r
}

func (s *CallStore) AppendTurn(id string, entry TurnEntry) {
	if entry.Text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.index[id]
	if !ok {
		return
	}
	if entry.Ts == 0 {
		entry.Ts = time.Now().UnixMilli()
	}
	r.Transcript = append(r.Transcript, entry)
}

func (s *CallStore) End(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.index[id]
	if !ok {
		return
	}
	now := time.Now().UnixMilli()
	r.EndedAt = &now
	r.Status = "done"
}

func (s *CallStore) List() []CallRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CallRecord, len(s.records))
	for i, r := range s.records {
		out[len(s.records)-1-i] = *r
	}
	return out
}

func (s *CallStore) Get(id string) (CallRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.index[id]
	if !ok {
		return CallRecord{}, false
	}
	return *r, true
}

// ---------------------------------------------------------------------------
// Postgres store
// ---------------------------------------------------------------------------

// pgSchema is run once on startup to create the table if it doesn't exist.
const pgSchema = `
CREATE TABLE IF NOT EXISTS calls (
    id          TEXT PRIMARY KEY,
    source      TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'ongoing',
    started_at  BIGINT      NOT NULL,
    ended_at    BIGINT,
    transcript  JSONB       NOT NULL DEFAULT '[]'
);`

// PGCallStore persists call records in Postgres using pgx/v5.
type PGCallStore struct {
	pool *pgxpool.Pool
}

// NewPGCallStore connects to Postgres, runs the schema migration, and returns
// a ready store. The caller is responsible for closing pool on shutdown.
func NewPGCallStore(ctx context.Context, databaseURL string) (*PGCallStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	if _, err := pool.Exec(ctx, pgSchema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}
	return &PGCallStore{pool: pool}, nil
}

func (s *PGCallStore) Close() { s.pool.Close() }

func (s *PGCallStore) Start(id, source string) {
	ctx := context.Background()
	_, _ = s.pool.Exec(ctx,
		`INSERT INTO calls (id, source, status, started_at, transcript)
		 VALUES ($1, $2, 'ongoing', $3, '[]')
		 ON CONFLICT (id) DO NOTHING`,
		id, source, time.Now().UnixMilli(),
	)
}

func (s *PGCallStore) AppendTurn(id string, entry TurnEntry) {
	if entry.Text == "" {
		return
	}
	if entry.Ts == 0 {
		entry.Ts = time.Now().UnixMilli()
	}
	arr := []TurnEntry{entry}
	raw, _ := json.Marshal(arr)
	ctx := context.Background()
	_, _ = s.pool.Exec(ctx,
		`UPDATE calls SET transcript = transcript || $2::jsonb WHERE id = $1`,
		id, raw,
	)
}

func (s *PGCallStore) End(id string) {
	ctx := context.Background()
	_, _ = s.pool.Exec(ctx,
		`UPDATE calls SET status = 'done', ended_at = $2 WHERE id = $1`,
		id, time.Now().UnixMilli(),
	)
}

func (s *PGCallStore) List() []CallRecord {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, source, status, started_at, ended_at, transcript
		 FROM calls ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanRows(rows)
}

func (s *PGCallStore) Get(id string) (CallRecord, bool) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, source, status, started_at, ended_at, transcript
		 FROM calls WHERE id = $1`,
		id,
	)
	if err != nil {
		return CallRecord{}, false
	}
	defer rows.Close()
	records := scanRows(rows)
	if len(records) == 0 {
		return CallRecord{}, false
	}
	return records[0], true
}

func scanRows(rows interface {
	Next() bool
	Scan(dest ...any) error
}) []CallRecord {
	var out []CallRecord
	for rows.Next() {
		var (
			r       CallRecord
			rawJSON []byte
		)
		if err := rows.Scan(&r.ID, &r.Source, &r.Status, &r.StartedAt, &r.EndedAt, &rawJSON); err != nil {
			continue
		}
		_ = json.Unmarshal(rawJSON, &r.Transcript)
		if r.Transcript == nil {
			r.Transcript = []TurnEntry{}
		}
		out = append(out, r)
	}
	return out
}
