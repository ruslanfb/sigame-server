package ws

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"sigame/internal/clock"
	"sigame/internal/room"
)

// Config are the transport limits.
type Config struct {
	MaxMessageBytes int64         // read limit per frame (default 64 KiB)
	MessagesPerSec  int           // sustained client message rate (default 40; burst = 2×)
	AllowedOrigins  []string      // origin host patterns; empty = accept any origin (LAN default)
	WriteTimeout    time.Duration // per frame (default 5 s)
	OutboundBuffer  int           // frames queued per connection before "slow consumer" (default 256)
}

// maxRateLimitStrikes closes the socket after this many consecutive
// rate-limited messages.
const maxRateLimitStrikes = 20

// Handler serves GET /ws?room=CODE&token=SESSION.
type Handler struct {
	Rooms  *room.Manager
	Cfg    Config
	Clock  clock.Clock
	Logger *slog.Logger
}

// Mount registers the handler at /ws.
func Mount(r chi.Router, h *Handler) { r.Get("/ws", h.ServeHTTP) }

func (h *Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h *Handler) clock() clock.Clock {
	if h.Clock != nil {
		return h.Clock
	}
	return clock.NewReal()
}

// ServeHTTP upgrades the connection, attaches it to the room and runs the
// reader loop until the socket closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("room")
	token := r.URL.Query().Get("token")
	if code == "" || token == "" {
		http.Error(w, "room and token query parameters are required", http.StatusBadRequest)
		return
	}
	if h.Rooms == nil {
		http.Error(w, "rooms unavailable", http.StatusServiceUnavailable)
		return
	}
	maxBytes := h.Cfg.MaxMessageBytes
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	perSec := h.Cfg.MessagesPerSec
	if perSec <= 0 {
		perSec = 40
	}
	netConn := connFromContext(r.Context())
	opts := &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled}
	if len(h.Cfg.AllowedOrigins) == 0 {
		opts.InsecureSkipVerify = true
	} else {
		opts.OriginPatterns = h.Cfg.AllowedOrigins
	}
	sock, err := websocket.Accept(w, r, opts)
	if err != nil {
		h.logger().Debug("ws: accept failed", "remote", r.RemoteAddr, "err", err)
		return
	}
	sock.SetReadLimit(maxBytes)

	// The request context is not usable after Accept; the connection gets its own.
	ctx, cancel := context.WithCancel(context.Background())
	c := newConn(sock, h.clock(), h.logger(), r.RemoteAddr, netConn, h.Cfg.WriteTimeout, h.Cfg.OutboundBuffer)
	c.cancel = cancel
	go c.writeLoop(ctx)

	att, err := h.Rooms.Attach(code, token, c)
	if err != nil {
		h.logger().Debug("ws: attach refused", "remote", r.RemoteAddr, "room", code, "err", err)
		c.sendError("badToken", "unknown room or session", 0)
		c.Close(room.CloseBadToken, "bad room or token")
		<-c.writerDone
		return
	}
	h.readLoop(ctx, c, att, maxBytes, perSec)
	att.Detach()
	c.Close(room.CloseNormal, "bye")
	<-c.writerDone
}

// readLoop reads frames, stamps them, rate-limits and delivers them.
func (h *Handler) readLoop(ctx context.Context, c *conn, att *room.Attachment, maxBytes int64, perSec int) {
	defer func() {
		if rec := recover(); rec != nil {
			h.logger().Error("ws reader panic", "panic", rec, "stack", string(debug.Stack()))
			c.Close(int(websocket.StatusInternalError), "internal error")
		}
	}()
	clk := h.clock()
	limiter := NewLimiter(perSec, 2*perSec, clk.Mono())
	strikes := 0
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		tRecv := clk.Mono() // stamped before any decoding
		if typ != websocket.MessageText {
			c.sendError("badPayload", "binary frames are not accepted", 0)
			continue
		}
		if !limiter.Allow(tRecv) {
			strikes++
			if strikes >= maxRateLimitStrikes {
				c.Close(room.ClosePolicy, "rate limit exceeded")
				return
			}
			c.sendError("rateLimited", "too many messages", 0)
			continue
		}
		strikes = 0
		env, err := Decode(data, maxBytes)
		if err != nil {
			c.sendError("badPayload", err.Error(), 0)
			continue
		}
		att.Deliver(room.Inbound{T: env.T, Seq: env.Seq, P: env.P, TRecv: tRecv})
	}
}
