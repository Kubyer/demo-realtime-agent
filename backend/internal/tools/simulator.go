package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const simulatedToolDelay = 400 * time.Millisecond

// Simulator implements llm.ToolSimulator with hardcoded stub responses.
// Replace individual handlers with real API calls when ready.
type Simulator struct{}

func NewSimulator() *Simulator { return &Simulator{} }

// Execute dispatches the tool call and returns a result string for the LLM.
func (s *Simulator) Execute(ctx context.Context, name, arguments string) (string, error) {
	// Simulate network latency for all tools.
	select {
	case <-time.After(simulatedToolDelay):
	case <-ctx.Done():
		return "", ctx.Err()
	}

	switch name {
	case "get_calendar_availability":
		return s.getCalendarAvailability(arguments)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Simulator) getCalendarAvailability(arguments string) (string, error) {
	var args struct {
		DateRange string `json:"date_range"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	// Hardcoded stub — replace with real calendar API.
	slots := []string{
		"2026-04-24T09:00:00",
		"2026-04-24T14:30:00",
		"2026-04-25T11:00:00",
	}
	result, _ := json.Marshal(map[string]any{
		"available_slots": slots,
		"date_range":      args.DateRange,
	})
	return string(result), nil
}
