package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/lib/pq"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"github.com/demo-realtime-agent/voiceagent/config"
	"github.com/demo-realtime-agent/voiceagent/internal/events"
	"github.com/demo-realtime-agent/voiceagent/internal/session"
	"github.com/demo-realtime-agent/voiceagent/internal/tools"
	"github.com/demo-realtime-agent/voiceagent/internal/transport"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// fetchCalendlyEvents returns the scheduled events for the ISO week that
// contains dateStr (YYYY-MM-DD). Merges real Calendly events with mock demo
// data so the calendar always looks populated during a demo.
func fetchCalendlyEvents(apiKey, dateStr string) []tools.CalendlyEvent {
	ref, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		ref = time.Now()
	}
	// Compute Monday of the same week.
	wd := int(ref.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := ref.AddDate(0, 0, -(wd - 1))
	sunday := monday.AddDate(0, 0, 6)

	minTime := monday.Format("2006-01-02") + "T00:00:00.000000Z"
	maxTime := sunday.Format("2006-01-02") + "T23:59:59.000000Z"

	mock := mockCalendlyEvents(monday)

	tools.MockBookingsMu.RLock()
	mock = append(mock, tools.MockBookings...)
	tools.MockBookingsMu.RUnlock()

	if apiKey != "" {
		real, err := fetchFromCalendly(apiKey, minTime, maxTime)
		if err == nil && len(real) > 0 {
			// Real events take precedence; still append mock ones for a full demo feel.
			return append(real, mock...)
		}
	}
	return mock
}

func fetchFromCalendly(apiKey, minTime, maxTime string) ([]tools.CalendlyEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First: get user URI.
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.calendly.com/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("calendly /users/me: %d", resp.StatusCode)
	}
	var me struct {
		Resource struct {
			URI string `json:"uri"`
		} `json:"resource"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return nil, err
	}
	userURI := me.Resource.URI
	if userURI == "" {
		return nil, fmt.Errorf("empty user URI")
	}

	// Then: fetch scheduled events.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	url := fmt.Sprintf(
		"https://api.calendly.com/scheduled_events?user=%s&min_start_time=%s&max_start_time=%s&status=active&count=100",
		userURI, minTime, maxTime,
	)
	req2, _ := http.NewRequestWithContext(ctx2, "GET", url, nil)
	req2.Header.Set("Authorization", "Bearer "+apiKey)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		return nil, fmt.Errorf("calendly /scheduled_events: %d", resp2.StatusCode)
	}
	var result struct {
		Collection []struct {
			URI       string `json:"uri"`
			Name      string `json:"name"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
			Status    string `json:"status"`
		} `json:"collection"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&result); err != nil {
		return nil, err
	}
	out := make([]tools.CalendlyEvent, 0, len(result.Collection))
	for _, e := range result.Collection {
		// Use last path segment of URI as ID.
		parts := strings.Split(e.URI, "/")
		id := parts[len(parts)-1]
		out = append(out, tools.CalendlyEvent{
			ID:        id,
			Name:      e.Name,
			StartTime: e.StartTime,
			EndTime:   e.EndTime,
			Status:    e.Status,
		})
	}
	return out, nil
}

// mockCalendlyEvents returns realistic mock events for the week starting monday.
func mockCalendlyEvents(monday time.Time) []tools.CalendlyEvent {
	type slot struct {
		day, startH, startM, endH, endM int
		name                            string
	}
	slots := []slot{
		{0, 9, 0, 9, 30, "Démo Legalplace – Marie Dupont"},
		{0, 14, 0, 14, 30, "Appel découverte – Jean Martin"},
		{1, 10, 30, 11, 0, "Suivi contrat – Sophie Bernard"},
		{2, 9, 0, 9, 30, "Démo Legalplace – Paul Leroy"},
		{2, 15, 0, 15, 30, "Appel découverte – Claire Moreau"},
		{3, 11, 0, 11, 30, "Suivi dossier – Thomas Petit"},
		{4, 10, 0, 10, 30, "Démo Legalplace – Emma Blanc"},
	}
	out := make([]tools.CalendlyEvent, 0, len(slots))
	for i, s := range slots {
		day := monday.AddDate(0, 0, s.day)
		start := time.Date(day.Year(), day.Month(), day.Day(), s.startH, s.startM, 0, 0, time.UTC)
		end := time.Date(day.Year(), day.Month(), day.Day(), s.endH, s.endM, 0, 0, time.UTC)
		out = append(out, tools.CalendlyEvent{
			ID:        fmt.Sprintf("mock-%d", i+1),
			Name:      s.name,
			StartTime: start.Format(time.RFC3339),
			EndTime:   end.Format(time.RFC3339),
			Status:    "active",
		})
	}
	return out
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load failed", "err", err)
		os.Exit(1)
	}

	hub := events.NewHub(log)
	go hub.Run()

	// Initialise call store: Postgres when DATABASE_URL is set, in-memory otherwise.
	var calls session.CallStorer
	if cfg.DatabaseURL != "" {
		pgStore, err := session.NewPGCallStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Error("postgres connect failed", "err", err)
			os.Exit(1)
		}
		defer pgStore.Close()
		calls = pgStore
		log.Info("call store: postgres")
	} else {
		calls = session.NewCallStore()
		log.Info("call store: in-memory (set DATABASE_URL for persistence)")
	}

	var dbConn *sql.DB
	if cfg.DatabaseURL != "" {
		var err error
		dbConn, err = sql.Open("postgres", cfg.DatabaseURL)
		if err != nil {
			log.Error("postgres sql connect failed", "err", err)
			os.Exit(1)
		}
		defer dbConn.Close()
	}

	sessCfg := session.Config{
		SonioxAPIKey:      cfg.SonioxAPIKey,
		SonioxWSURL:       cfg.SonioxWSURL,
		GroqAPIKey:        cfg.GroqAPIKey,
		GroqModel:         cfg.GroqModel,
		ElevenLabsAPIKey:  cfg.ElevenLabsAPIKey,
		ElevenLabsVoiceID: cfg.ElevenLabsVoiceID,
		ElevenLabsModel:   cfg.ElevenLabsModel,
		CartesiaAPIKey:    cfg.CartesiaAPIKey,
		CartesiaWSURL:     cfg.CartesiaWSURL,
		GradiumAPIKey:     cfg.GradiumAPIKey,
		CalendlyAPIKey:    cfg.CalendlyAPIKey,
		DB:                dbConn,
	}
	manager := session.NewManager(sessCfg, hub, calls, log)

	mux := http.NewServeMux()

	// /twiml — webhook called by Twilio on incoming call.
	mux.HandleFunc("/twiml", func(w http.ResponseWriter, r *http.Request) {
		publicURL := os.Getenv("PUBLIC_URL")
		if publicURL == "" {
			if app := os.Getenv("FLY_APP_NAME"); app != "" {
				publicURL = "https://" + app + ".fly.dev"
			}
		}
		if publicURL == "" {
			http.Error(w, "PUBLIC_URL not configured", http.StatusServiceUnavailable)
			return
		}
		host := strings.TrimPrefix(strings.TrimPrefix(publicURL, "https://"), "http://")
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Response><Connect><Stream url="wss://%s/twilio/stream"/></Connect></Response>`, host)
	})

	// /twilio/stream — Twilio Media Stream WebSocket (mulaw 8 kHz).
	mux.HandleFunc("/twilio/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("twilio ws upgrade", "err", err)
			return
		}
		defer conn.Close()

		sessionID := r.Header.Get("X-Twilio-Call-Sid")
		if sessionID == "" {
			sessionID = r.RemoteAddr
		}

		tr := transport.NewTwilioWebSocket(conn, log)
		sess := manager.Create(sessionID, "twilio", tr)
		log.Info("twilio stream connected", "session_id", sessionID)

		<-sess.Done()
		manager.Stop(sess.ID)
		log.Info("twilio stream disconnected", "session_id", sessionID)
	})

	// /browser/stream — Browser WebSocket (PCM s16le 16 kHz in, mulaw 8 kHz out).
	mux.HandleFunc("/browser/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("browser ws upgrade", "err", err)
			return
		}
		defer conn.Close()

		sessionID := fmt.Sprintf("browser-%d", time.Now().UnixNano())

		tr := transport.NewBrowserWebSocket(conn, log)
		sess := manager.Create(sessionID, "browser", tr)
		log.Info("browser stream connected", "session_id", sessionID)

		<-sess.Done()
		manager.Stop(sess.ID)
		log.Info("browser stream disconnected", "session_id", sessionID)
	})

	// /events — UI events WebSocket (fan-out to all connected frontends).
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("events ws upgrade", "err", err)
			return
		}
		defer conn.Close()

		cleanup := hub.Register(conn)
		defer cleanup()

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	})

	// /api/settings — GET returns current settings; PUT replaces them.
	mux.Handle("/api/settings", corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(session.GetSettings()) //nolint:errcheck
		case http.MethodPut:
			body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
			if err != nil {
				http.Error(w, "read error", http.StatusBadRequest)
				return
			}
			var req session.Settings
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			if req.VoiceProvider == "" {
				req.VoiceProvider = "elevenlabs"
			}
			if req.VoiceID == "" {
				req.VoiceID = "3C1zYzXNXNzrB66ON8rj"
			}
			if req.VoiceModel == "" {
				req.VoiceModel = "eleven_flash_v2_5"
			}
			session.SetSettings(req)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// /api/calendly/events — GET returns scheduled events for a given week.
	mux.Handle("/api/calendly/events", corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		evts := fetchCalendlyEvents(cfg.CalendlyAPIKey, date)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(evts) //nolint:errcheck
	})))

	// /api/calls — GET returns all call records (newest first).
	mux.Handle("/api/calls", corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		records := manager.Calls.List()
		json.NewEncoder(w).Encode(records) //nolint:errcheck
	})))

	// /api/calls/{id} — GET returns a single call record with full transcript.
	mux.Handle("/api/calls/", corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/calls/")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		rec, ok := manager.Calls.Get(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rec) //nolint:errcheck
	})))

	// /health — liveness probe.
	// Serve recordings statically
	mux.Handle("/recordings/", corsMiddleware(http.StripPrefix("/recordings/", http.FileServer(http.Dir("recordings")))))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","sessions":` + strconv.Itoa(manager.ActiveCount()) + `}`)) //nolint:errcheck
	})

	srv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-stop
	log.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx) //nolint:errcheck
}
