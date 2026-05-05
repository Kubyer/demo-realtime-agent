package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const simulatedToolDelay = 400 * time.Millisecond

type HubBroadcaster interface {
	BroadcastToolCall(name, args string)
	BroadcastToolResult(name, result string)
}

// CalendlyEvent is the public representation sent to the frontend.
type CalendlyEvent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Status    string `json:"status"`
}

var (
	MockBookings   []CalendlyEvent
	MockBookingsMu sync.RWMutex
)

// Simulator implements llm.ToolSimulator with stub responses for demo purposes.
// Replace individual handlers with real API calls when ready.
type Simulator struct {
	CalendlyAPIKey string
	Hub            HubBroadcaster // optional; nil → no event broadcast
}

func NewSimulator(calendlyAPIKey string, hub HubBroadcaster) *Simulator {
	return &Simulator{
		CalendlyAPIKey: calendlyAPIKey,
		Hub:            hub,
	}
}

// Execute dispatches the tool call and returns a result string for the LLM.
func (s *Simulator) Execute(ctx context.Context, name, arguments string) (string, error) {
	// Broadcast that the tool was called.
	if s.Hub != nil {
		s.Hub.BroadcastToolCall(name, arguments)
	}

	var result string
	var err error

	switch name {
	case "check_availability":
		result, err = s.checkAvailability(ctx, arguments)
	case "book_meeting":
		result, err = s.bookMeeting(ctx, arguments)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	if err != nil {
		return result, err
	}

	// Broadcast the tool result.
	if s.Hub != nil {
		s.Hub.BroadcastToolResult(name, result)
	}

	return result, nil
}

// checkAvailability returns available slots for the requested date.
// It first tries the real Calendly API (to show real busy times), then
// falls back to a deterministic mock that always has sensible afternoon slots.
func (s *Simulator) checkAvailability(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	// Validate and parse the date.
	ref, err := time.Parse("2006-01-02", args.Date)
	if err != nil {
		return `{"error": "Format de date invalide, utilisez YYYY-MM-DD"}`, nil
	}

	// Don't offer slots in the past.
	today := time.Now().Truncate(24 * time.Hour)
	if ref.Before(today) {
		return `{"error": "Cette date est déjà passée. Proposez un jour futur."}`, nil
	}

	// Weekends: no availability.
	wd := ref.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return `{"error": "Pas de disponibilité le week-end. Proposez un jour de semaine."}`, nil
	}

	// Try to fetch real booked events from Calendly to avoid conflicts.
	busySlots := s.fetchBusySlots(ctx, args.Date)

	MockBookingsMu.RLock()
	for _, mb := range MockBookings {
		if len(mb.StartTime) >= 16 && mb.StartTime[:10] == args.Date {
			busySlots = append(busySlots, mb.StartTime[:16])
		}
	}
	MockBookingsMu.RUnlock()

	// Generate mock available slots (09:00 and 14:30) avoiding busy times.
	candidates := []string{
		args.Date + "T09:00:00",
		args.Date + "T10:00:00",
		args.Date + "T14:30:00",
		args.Date + "T16:00:00",
	}

	var available []string
	for _, slot := range candidates {
		if !isBusy(slot, busySlots) {
			available = append(available, slot)
		}
	}

	if len(available) == 0 {
		return fmt.Sprintf(`{"date": "%s", "available_slots": [], "message": "Aucun créneau disponible ce jour-là."}`, args.Date), nil
	}

	slotsJSON, _ := json.Marshal(available)
	return fmt.Sprintf(`{"date": "%s", "available_slots": %s}`, args.Date, slotsJSON), nil
}

// fetchBusySlots calls the Calendly API to get booked event start times for a day.
// Returns an empty slice on any error (graceful degradation).
func (s *Simulator) fetchBusySlots(ctx context.Context, dateStr string) []string {
	if s.CalendlyAPIKey == "" {
		return nil
	}

	tCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Get user URI.
	req, _ := http.NewRequestWithContext(tCtx, "GET", "https://api.calendly.com/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+s.CalendlyAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	var me struct {
		Resource struct {
			URI string `json:"uri"`
		} `json:"resource"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil || me.Resource.URI == "" {
		return nil
	}

	// Fetch scheduled events for that day.
	minTime := dateStr + "T00:00:00.000000Z"
	maxTime := dateStr + "T23:59:59.000000Z"
	url := fmt.Sprintf(
		"https://api.calendly.com/scheduled_events?user=%s&min_start_time=%s&max_start_time=%s&status=active&count=50",
		me.Resource.URI, minTime, maxTime,
	)

	tCtx2, cancel2 := context.WithTimeout(ctx, 3*time.Second)
	defer cancel2()

	req2, _ := http.NewRequestWithContext(tCtx2, "GET", url, nil)
	req2.Header.Set("Authorization", "Bearer "+s.CalendlyAPIKey)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil || resp2.StatusCode != 200 {
		return nil
	}
	defer resp2.Body.Close()

	var result struct {
		Collection []struct {
			StartTime string `json:"start_time"`
		} `json:"collection"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&result); err != nil {
		return nil
	}

	busy := make([]string, 0, len(result.Collection))
	for _, e := range result.Collection {
		busy = append(busy, e.StartTime[:10]+"T"+e.StartTime[11:16]) // keep HH:MM
	}
	return busy
}

// isBusy returns true if slot (YYYY-MM-DDTHH:MM:SS) overlaps any busy entry.
func isBusy(slot string, busy []string) bool {
	// Compare by HH:MM prefix only (ignore seconds).
	slotPrefix := slot[:16]
	for _, b := range busy {
		if len(b) >= 16 && b[:16] == slotPrefix {
			return true
		}
	}
	return false
}

// bookMeeting creates a mock booking. In production replace with Calendly scheduling link API.
func (s *Simulator) bookMeeting(_ context.Context, arguments string) (string, error) {
	var args struct {
		Datetime string `json:"datetime"` // ISO e.g. "2026-05-07T14:30:00"
		Name     string `json:"name"`
		Email    string `json:"email"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	if args.Datetime == "" {
		return `{"status": "error", "message": "Créneau manquant pour la réservation."}`, nil
	}

	// Parse the datetime for a friendly confirmation message.
	t, err := time.Parse("2006-01-02T15:04:05", args.Datetime)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, args.Datetime)
	}

	var dayFr string
	if !t.IsZero() {
		days := []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
		months := []string{"", "janvier", "février", "mars", "avril", "mai", "juin",
			"juillet", "août", "septembre", "octobre", "novembre", "décembre"}
		dayFr = fmt.Sprintf("%s %d %s à %02dh%02d",
			days[t.Weekday()], t.Day(), months[t.Month()], t.Hour(), t.Minute())
	} else {
		dayFr = args.Datetime
	}

	if !t.IsZero() {
		MockBookingsMu.Lock()
		MockBookings = append(MockBookings, CalendlyEvent{
			ID:        fmt.Sprintf("booked-%d", time.Now().UnixNano()),
			Name:      args.Name,
			StartTime: t.Format(time.RFC3339),
			EndTime:   t.Add(30 * time.Minute).Format(time.RFC3339),
			Status:    "active",
		})
		MockBookingsMu.Unlock()
	}

	result := map[string]string{
		"status":   "success",
		"datetime": args.Datetime,
		"name":     args.Name,
		"email":    args.Email,
		"message":  fmt.Sprintf("Rendez-vous confirmé le %s pour %s (%s).", dayFr, args.Name, args.Email),
	}
	out, _ := json.Marshal(result)
	return string(out), nil
}
