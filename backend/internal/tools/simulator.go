package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const simulatedToolDelay = 400 * time.Millisecond

// Simulator implements llm.ToolSimulator with hardcoded stub responses.
// Replace individual handlers with real API calls when ready.
type Simulator struct {
	DB             *sql.DB
	CalendlyAPIKey string
}

func NewSimulator(db *sql.DB, calendlyAPIKey string) *Simulator {
	return &Simulator{
		DB:             db,
		CalendlyAPIKey: calendlyAPIKey,
	}
}

// Execute dispatches the tool call and returns a result string for the LLM.
func (s *Simulator) Execute(ctx context.Context, name, arguments string) (string, error) {
	switch name {
	case "check_availability":
		return s.checkAvailability(ctx, arguments)
	case "fetch_prospect":
		return s.fetchProspect(ctx, arguments)
	case "book_meeting":
		return `{"status": "success", "message": "Invité ajouté"}`, nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Simulator) checkAvailability(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.calendly.com/user_availability_schedules", nil)
	if err != nil {
		return `{"error": "Impossible de construire la requête calendrier"}`, nil
	}
	req.Header.Set("Authorization", "Bearer "+s.CalendlyAPIKey)

	// Timeout de 3s
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return `{"error": "Erreur réseau lors de la requête Calendly"}`, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return `{"error": "L'API Calendly a renvoyé une erreur"}`, nil
	}

	// Mock logical des créneaux
	return fmt.Sprintf(`{"available_slots": ["%sT09:00:00", "%sT14:30:00"]}`, args.Date, args.Date), nil
}

func (s *Simulator) fetchProspect(ctx context.Context, arguments string) (string, error) {
	if s.DB == nil {
		return `{"error": "La base de données n'est pas connectée"}`, nil
	}

	var args struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	var nom, statut string
	err := s.DB.QueryRowContext(ctx, "SELECT nom, statut FROM prospects WHERE email = $1", args.Email).Scan(&nom, &statut)
	if err == sql.ErrNoRows {
		return `{"error": "Prospect introuvable"}`, nil
	} else if err != nil {
		return `{"error": "Erreur lors de l'accès à la base de données"}`, nil
	}

	result, _ := json.Marshal(map[string]string{
		"nom":    nom,
		"statut": statut,
	})
	return string(result), nil
}
