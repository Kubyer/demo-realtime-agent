package transport

import (
	"context"
	"log/slog"
	"sync"

	"github.com/gorilla/websocket"
)

// BrowserWebSocket implements AudioTransport for a direct browser connection.
// The browser sends raw PCM s16le frames as binary WebSocket messages.
// The server sends raw mulaw 8 kHz frames as binary WebSocket messages.
type BrowserWebSocket struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	log     *slog.Logger
}

func NewBrowserWebSocket(conn *websocket.Conn, log *slog.Logger) *BrowserWebSocket {
	return &BrowserWebSocket{conn: conn, log: log}
}

// ReadStream returns a channel of raw audio frames from the browser.
func (b *BrowserWebSocket) ReadStream(ctx context.Context) (<-chan []byte, error) {
	ch := make(chan []byte, audioChannelBuf)
	go func() {
		defer close(ch)
		for {
			msgType, data, err := b.conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					b.log.Warn("browser ws read error", "err", err)
				}
				return
			}
			if msgType != websocket.BinaryMessage {
				continue
			}
			select {
			case ch <- data:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// WriteStream sends raw mulaw audio frames to the browser as binary messages.
func (b *BrowserWebSocket) WriteStream(ctx context.Context, audio <-chan []byte) error {
	for {
		select {
		case chunk, ok := <-audio:
			if !ok {
				return nil
			}
			b.writeMu.Lock()
			err := b.conn.WriteMessage(websocket.BinaryMessage, chunk)
			b.writeMu.Unlock()
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ClearBuffer is a no-op for browser transport; the Web Audio API handles
// scheduling and there is no server-side audio queue to discard.
func (b *BrowserWebSocket) ClearBuffer() {}
