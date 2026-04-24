package transport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gorilla/websocket"
)

// twilioInbound is the JSON envelope Twilio sends for each media frame.
type twilioInbound struct {
	Event     string         `json:"event"`
	StreamSid string         `json:"streamSid"`
	Media     *twilioMedia   `json:"media,omitempty"`
	Start     *twilioStart   `json:"start,omitempty"`
}

type twilioMedia struct {
	Payload string `json:"payload"` // base64 mulaw
}

type twilioStart struct {
	StreamSid string `json:"streamSid"`
	CallSid   string `json:"callSid"`
}

// twilioOutbound is the JSON envelope the server sends back to Twilio.
type twilioOutbound struct {
	Event     string       `json:"event"`
	StreamSid string       `json:"streamSid"`
	Media     *outMedia    `json:"media,omitempty"`
}

type outMedia struct {
	Payload string `json:"payload"` // base64 mulaw
}

const audioChannelBuf = 16

// TwilioWebSocket implements AudioTransport over a Twilio Media Stream WebSocket.
type TwilioWebSocket struct {
	conn      *websocket.Conn
	streamSid string
	writeMu   sync.Mutex // gorilla requires serialised writes
	log       *slog.Logger
}

func NewTwilioWebSocket(conn *websocket.Conn, log *slog.Logger) *TwilioWebSocket {
	return &TwilioWebSocket{conn: conn, log: log}
}

// ReadStream starts a goroutine that reads Twilio frames and decodes the mulaw
// payload into raw bytes. The channel is closed when the WebSocket closes or
// ctx is cancelled.
func (t *TwilioWebSocket) ReadStream(ctx context.Context) (<-chan []byte, error) {
	ch := make(chan []byte, audioChannelBuf)

	go func() {
		defer close(ch)
		for {
			_, raw, err := t.conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					t.log.Warn("twilio ws read error", "err", err)
				}
				return
			}

			var msg twilioInbound
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.log.Warn("twilio ws unmarshal", "err", err)
				continue
			}

			switch msg.Event {
			case "start":
				if msg.Start != nil {
					t.streamSid = msg.Start.StreamSid
					t.log.Info("twilio stream started", "stream_sid", t.streamSid, "call_sid", msg.Start.CallSid)
				}
			case "media":
				if msg.Media == nil {
					continue
				}
				audio, err := base64.StdEncoding.DecodeString(msg.Media.Payload)
				if err != nil {
					t.log.Warn("twilio base64 decode", "err", err)
					continue
				}
				select {
				case ch <- audio:
				case <-ctx.Done():
					return
				}
			case "stop":
				t.log.Info("twilio stream stopped")
				return
			}
		}
	}()

	return ch, nil
}

// WriteStream reads audio frames from the provided channel, encodes them as
// base64 mulaw, and sends Twilio media events. Blocks until ctx is cancelled
// or the audio channel is closed.
func (t *TwilioWebSocket) WriteStream(ctx context.Context, audio <-chan []byte) error {
	for {
		select {
		case chunk, ok := <-audio:
			if !ok {
				return nil
			}
			if err := t.writeMediaFrame(chunk); err != nil {
				return fmt.Errorf("twilio write: %w", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ClearBuffer sends the Twilio clear event, instructing Twilio to discard its
// audio queue. Safe to call from any goroutine.
func (t *TwilioWebSocket) ClearBuffer() {
	msg := map[string]string{
		"event":     "clear",
		"streamSid": t.streamSid,
	}
	data, _ := json.Marshal(msg)
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if err := t.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.log.Warn("twilio clear buffer write", "err", err)
	}
}

func (t *TwilioWebSocket) writeMediaFrame(chunk []byte) error {
	t.log.Debug("twilio: writing media frame", "bytes", len(chunk), "stream_sid", t.streamSid)
	encoded := base64.StdEncoding.EncodeToString(chunk)
	msg := twilioOutbound{
		Event:     "media",
		StreamSid: t.streamSid,
		Media:     &outMedia{Payload: encoded},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.conn.WriteMessage(websocket.TextMessage, data)
}
