package main

import (
	"context"
	"encoding/json"
	"fmt"
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

	sessCfg := session.Config{
		SonioxAPIKey:    cfg.SonioxAPIKey,
		SonioxWSURL:     cfg.SonioxWSURL,
		GroqAPIKey:      cfg.GroqAPIKey,
		GroqModel:       cfg.GroqModel,
		CartesiaAPIKey:  cfg.CartesiaAPIKey,
		CartesiaWSURL:   cfg.CartesiaWSURL,
		CartesiaVoiceID: cfg.CartesiaVoiceID,
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

	// /api/system-prompt — GET returns current prompt; PUT replaces it.
	mux.Handle("/api/system-prompt", corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]string{"prompt": session.GetSystemPrompt()}) //nolint:errcheck
		case http.MethodPut:
			body, err := io.ReadAll(io.LimitReader(r.Body, 32*1024))
			if err != nil {
				http.Error(w, "read error", http.StatusBadRequest)
				return
			}
			var req struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal(body, &req); err != nil || req.Prompt == "" {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			session.SetSystemPrompt(req.Prompt)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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
