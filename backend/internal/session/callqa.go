package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// QA result types
// ---------------------------------------------------------------------------

// CallQAResult holds the post-call quality audit for a single session.
// Fields are nil when the analysis could not compute them (e.g. DNSMOS model
// not downloaded yet, or one of the WAV files was empty).
type CallQAResult struct {
	SessionID string `json:"session_id"`
	// unix millis when analysis completed (or failed)
	AnalyzedAt int64  `json:"analyzed_at"`
	Status     string `json:"status"` // "pending" | "done" | "failed"
	Error      string `json:"error,omitempty"`

	// Audio quality (DNSMOS) — nil if model not present
	MOSSig  *float64 `json:"mos_sig,omitempty"`
	MOSBak  *float64 `json:"mos_bak,omitempty"`
	MOSOvrl *float64 `json:"mos_ovrl,omitempty"`

	// Overlap — fraction of call time where both speakers are active
	TalkOverRate *float64 `json:"talk_over_rate,omitempty"`

	// Conversation health derived from transcript (no audio needed)
	AvgTTFAMs    *int64 `json:"avg_ttfa_ms,omitempty"`
	TurnCount    int    `json:"turn_count"`
	BargeinCount int    `json:"bargein_count"`
	Completed    bool   `json:"completed"` // book_meeting tool was called

	// Full pipeline parameter snapshot
	Config CallConfig `json:"config"`

	// Derived alerts
	Alerts []QAAlert `json:"alerts,omitempty"`
}

// QAAlert is a machine-readable quality warning attached to a CallQAResult.
type QAAlert struct {
	Severity string `json:"severity"` // "warning" | "critical"
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// ---------------------------------------------------------------------------
// In-memory QA store
// ---------------------------------------------------------------------------

// CallQAStore is a thread-safe in-memory store for QA results, keyed by session ID.
// Results are kept for the lifetime of the process.
type CallQAStore struct {
	mu      sync.RWMutex
	results map[string]*CallQAResult
	log     *slog.Logger
}

func NewCallQAStore(log *slog.Logger) *CallQAStore {
	return &CallQAStore{
		results: make(map[string]*CallQAResult),
		log:     log,
	}
}

func (s *CallQAStore) setPending(sessionID string, cfg CallConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[sessionID] = &CallQAResult{
		SessionID: sessionID,
		Status:    "pending",
		Config:    cfg,
	}
}

func (s *CallQAStore) setResult(r *CallQAResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[r.SessionID] = r
}

// Get returns the QA result for a session, and whether it exists.
func (s *CallQAStore) Get(sessionID string) (*CallQAResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.results[sessionID]
	return r, ok
}

// ---------------------------------------------------------------------------
// QA pipeline trigger
// ---------------------------------------------------------------------------

// scoreInput is the JSON payload passed to qa/score.py on stdin.
type scoreInput struct {
	SessionID string `json:"session_id"`
	UserWAV   string `json:"user_wav"`
	AgentWAV  string `json:"agent_wav"`
}

// scriptOutput is the JSON printed to stdout by qa/score.py.
type scriptOutput struct {
	TalkOverRate *float64 `json:"talk_over_rate"`
	MOSSig       *float64 `json:"mos_sig"`
	MOSBak       *float64 `json:"mos_bak"`
	MOSOvrl      *float64 `json:"mos_ovrl"`
	Error        string   `json:"error,omitempty"`
}

// findPython returns the path to python3/python, checking PATH and common
// macOS/Linux install locations (Homebrew, system) so it works even when the
// server is started from an IDE that doesn't inherit the full shell PATH.
func findPython() (string, error) {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	// Fallback: probe well-known prefixes not always on Go's PATH.
	for _, candidate := range []string{
		"/opt/homebrew/bin/python3",
		"/usr/local/bin/python3",
		"/usr/bin/python3",
		"/opt/homebrew/bin/python",
		"/usr/local/bin/python",
		"/usr/bin/python",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("python3 not found in PATH or common install locations")
}

// findScript locates qa/score.py relative to CWD or common parent directories.
func findScript() string {
	for _, rel := range []string{"qa/score.py", "backend/qa/score.py", "../qa/score.py"} {
		if _, err := os.Stat(rel); err == nil {
			abs, _ := filepath.Abs(rel)
			return abs
		}
	}
	return ""
}

// TriggerQA marks the session as pending and launches the analysis in a
// background goroutine. It returns immediately. Results can be retrieved
// via Get once the goroutine completes.
func (s *CallQAStore) TriggerQA(
	sessionID string,
	paths RecorderPaths,
	record CallRecord,
	cfg CallConfig,
) {
	s.setPending(sessionID, cfg)
	go s.runAnalysis(sessionID, paths, record, cfg)
}

func (s *CallQAStore) runAnalysis(
	sessionID string,
	paths RecorderPaths,
	record CallRecord,
	cfg CallConfig,
) {
	result := &CallQAResult{
		SessionID: sessionID,
		Config:    cfg,
	}

	// --- Transcript-derived metrics (always available) ---
	result.TurnCount = len(record.Transcript)
	var ttfas []int64
	for _, turn := range record.Transcript {
		if turn.Role == "assistant" && turn.E2EMs != nil {
			ttfas = append(ttfas, *turn.E2EMs)
		}
		if strings.Contains(strings.ToLower(turn.Text), "book_meeting") ||
			strings.Contains(strings.ToLower(turn.Text), "réservation confirmée") ||
			strings.Contains(strings.ToLower(turn.Text), "créneau") {
			result.Completed = true
		}
	}
	if len(ttfas) > 0 {
		var sum int64
		for _, v := range ttfas {
			sum += v
		}
		avg := sum / int64(len(ttfas))
		result.AvgTTFAMs = &avg
	}

	// --- Audio analysis via Python script ---
	if paths.User != "" && paths.Agent != "" {
		python, pyErr := findPython()
		script := findScript()

		if pyErr != nil || script == "" {
			msg := "audio analysis unavailable"
			if pyErr != nil {
				msg += ": " + pyErr.Error()
			} else {
				msg += ": qa/score.py not found"
			}
			s.log.Warn("qa: cannot run audio analysis", "session_id", sessionID, "reason", msg)
			result.Error = msg
		} else {
			payload, _ := json.Marshal(scoreInput{
				SessionID: sessionID,
				UserWAV:   paths.User,
				AgentWAV:  paths.Agent,
			})

			cmd := exec.Command(python, script)
			cmd.Stdin = bytes.NewReader(payload)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				s.log.Warn("qa: script failed",
					"session_id", sessionID,
					"err", err,
					"stderr", stderr.String(),
				)
				result.Error = "audio analysis unavailable: " + stderr.String()
			} else {
				var out scriptOutput
				if jsonErr := json.Unmarshal(stdout.Bytes(), &out); jsonErr != nil {
					s.log.Warn("qa: parse script output", "err", jsonErr, "raw", stdout.String())
					result.Error = "output parse error"
				} else {
					result.TalkOverRate = out.TalkOverRate
					result.MOSSig = out.MOSSig
					result.MOSBak = out.MOSBak
					result.MOSOvrl = out.MOSOvrl
					if out.Error != "" {
						result.Error = out.Error
					}
				}
			}
		}
	} else {
		s.log.Info("qa: skipping audio analysis (one or both WAV files missing)", "session_id", sessionID)
	}

	result.Status = "done"
	result.AnalyzedAt = time.Now().UnixMilli()
	result.Alerts = generateAlerts(result)

	s.setResult(result)
	s.log.Info("qa: analysis complete",
		"session_id", sessionID,
		"mos_ovrl", result.MOSOvrl,
		"talk_over_rate", result.TalkOverRate,
		"alerts", len(result.Alerts),
	)
}

// generateAlerts produces machine-readable quality warnings from a finished result.
func generateAlerts(r *CallQAResult) []QAAlert {
	var alerts []QAAlert

	if r.MOSOvrl != nil && *r.MOSOvrl < 3.0 {
		alerts = append(alerts, QAAlert{
			Severity: "critical",
			Code:     "LOW_MOS",
			Message:  "Agent voice quality below threshold — MOS < 3.0 (sounds robotic or noisy)",
		})
	}
	if r.TalkOverRate != nil && *r.TalkOverRate > 0.15 {
		alerts = append(alerts, QAAlert{
			Severity: "warning",
			Code:     "HIGH_OVERLAP",
			Message:  "Talk-over rate > 15% — agent kept speaking while user was talking",
		})
	}
	if r.AvgTTFAMs != nil && *r.AvgTTFAMs > 2000 {
		alerts = append(alerts, QAAlert{
			Severity: "warning",
			Code:     "HIGH_LATENCY",
			Message:  "Average response latency > 2s — check LLM or TTS pipeline",
		})
	}
	if r.TurnCount < 3 {
		alerts = append(alerts, QAAlert{
			Severity: "warning",
			Code:     "SHORT_CALL",
			Message:  "Fewer than 3 turns — call ended too quickly",
		})
	}

	return alerts
}
