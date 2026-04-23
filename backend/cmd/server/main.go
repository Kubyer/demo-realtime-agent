package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"github.com/legalplace/voiceagent/config"
	"github.com/legalplace/voiceagent/internal/events"
	"github.com/legalplace/voiceagent/internal/session"
	"github.com/legalplace/voiceagent/internal/transport"
)

var upgrader = websocket.Upgrader{
	// In production, validate r.Host against an allowlist.
	CheckOrigin: func(r *http.Request) bool { return true },
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

	// ngrokURL is the public URL exposed by ngrok (e.g. https://xxxx.ngrok-free.app).
	// Set via NGROK_URL env var or .env file.
	ngrokURL := cfg.NgrokURL
	if ngrokURL == "" {
		log.Warn("NGROK_URL not set — /twiml will return 503 until it is configured")
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
	manager := session.NewManager(sessCfg, hub, log)

	mux := http.NewServeMux()

	// /twiml — TwiML webhook appelé par Twilio quand une personne appelle le numéro.
	// Twilio lit ce XML et connecte l'appel au WebSocket /twilio/stream.
	mux.HandleFunc("/twiml", func(w http.ResponseWriter, r *http.Request) {
		if ngrokURL == "" {
			http.Error(w, "NGROK_URL not configured", http.StatusServiceUnavailable)
			return
		}
		wsURL := "wss://" + strings.TrimPrefix(strings.TrimPrefix(ngrokURL, "https://"), "http://") + "/twilio/stream"
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url=%q/>
  </Connect>
</Response>`, wsURL)
		log.Info("/twiml served", "ws_url", wsURL)
	})

	// /twilio/stream — Twilio Media Stream WebSocket.
	// Twilio opens this after the TwiML <Stream> verb.
	// The TwilioWebSocket transport owns all reads from conn; this handler
	// simply blocks until the session finishes so the HTTP connection stays open.
	mux.HandleFunc("/twilio/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("twilio ws upgrade", "err", err)
			return
		}
		defer conn.Close()

		sessionID := r.Header.Get("X-Twilio-Call-Sid")
		if sessionID == "" {
			sessionID = r.RemoteAddr // fallback for local testing
		}

		tr := transport.NewTwilioWebSocket(conn, log)
		sess := manager.Create(sessionID, tr)
		log.Info("twilio stream connected", "session_id", sessionID)

		// Block until the session's goroutine tree exits (WebSocket closed or
		// context cancelled). The transport's ReadStream goroutine owns the conn reads.
		<-sess.Done()
		manager.Stop(sess.ID)
		log.Info("twilio stream disconnected", "session_id", sessionID)
	})

	// /events — UI events WebSocket consumed by the Next.js frontend.
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("events ws upgrade", "err", err)
			return
		}
		defer conn.Close()

		cleanup := hub.Register(conn)
		defer cleanup()

		// Drain pings/control frames; we don't use client→server messages here.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	})

	// /health — liveness probe for load balancers.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","sessions":` + strconv.Itoa(manager.ActiveCount()) + `}`)) //nolint:errcheck
	})

	srv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0, // WebSocket streams require no global write timeout
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
