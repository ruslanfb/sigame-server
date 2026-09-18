package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/coder/websocket"

	"sigame/internal/clock"
	"sigame/internal/room"
)

// Sentinel connection errors.
var (
	ErrConnClosed   = errors.New("ws: connection closed")
	ErrSlowConsumer = errors.New("ws: slow consumer")
)

// closeTimeout bounds the close handshake started by Close.
const closeTimeout = 3 * time.Second

// conn is one accepted WebSocket connection. It implements room.Conn: a
// writer goroutine drains the outbound channel and every Send assigns the
// next per-connection seq. A full outbound buffer means the client cannot
// keep up; the connection is closed with 1008 "slow consumer".
type conn struct {
	ws      *websocket.Conn
	clock   clock.Clock
	log     *slog.Logger
	remote  string
	netConn net.Conn

	writeTimeout time.Duration
	out          chan []byte
	writerDone   chan struct{}

	mu          sync.Mutex
	seq         int64
	closed      bool
	closeCode   int
	closeReason string

	closeOnce   sync.Once
	doCloseOnce sync.Once
	cancel      context.CancelFunc
}

func newConn(ws *websocket.Conn, clk clock.Clock, log *slog.Logger, remote string, nc net.Conn, writeTimeout time.Duration, buffer int) *conn {
	if buffer <= 0 {
		buffer = 256
	}
	if writeTimeout <= 0 {
		writeTimeout = 5 * time.Second
	}
	return &conn{ws: ws, clock: clk, log: log, remote: remote, netConn: nc, writeTimeout: writeTimeout,
		out: make(chan []byte, buffer), writerDone: make(chan struct{})}
}

// writeLoop is the single writer of the socket. A nil frame is the close
// sentinel queued by Close: everything before it is flushed first.
func (c *conn) writeLoop(ctx context.Context) {
	defer close(c.writerDone)
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-c.out:
			if frame == nil {
				c.doClose()
				return
			}
			wctx, cancel := context.WithTimeout(ctx, c.writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				c.closeNow(room.ClosePolicy, "write failed")
				return
			}
		}
	}
}

// Send implements room.Conn.
func (c *conn) Send(t string, payload json.RawMessage) (int64, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, ErrConnClosed
	}
	c.seq++
	seq := c.seq
	c.mu.Unlock()
	var p any
	if len(payload) > 0 {
		p = payload
	}
	frame, err := Encode(t, seq, p)
	if err != nil {
		return seq, err
	}
	select {
	case c.out <- frame:
		return seq, nil
	default:
		c.log.Warn("ws: slow consumer", "remote", c.remote)
		c.Close(room.ClosePolicy, "slow consumer")
		return seq, ErrSlowConsumer
	}
}

// sendError writes an ERROR envelope produced by the transport itself.
func (c *conn) sendError(code, msg string, ref int64) {
	b, err := json.Marshal(ErrorPayload{Code: code, Message: msg, Ref: ref})
	if err != nil {
		return
	}
	_, _ = c.Send("ERROR", b)
}

// Close implements room.Conn: no more Sends are accepted, the frames already
// queued are flushed and then the close handshake runs on the writer
// goroutine (the caller may be the room actor and never blocks on the
// network). When the queue is full the handshake starts right away.
func (c *conn) Close(code int, reason string) {
	c.closeOnce.Do(func() {
		c.markClosed(code, reason)
		select {
		case c.out <- nil:
		default:
			go c.doClose()
		}
	})
}

// closeNow skips the flush (the writer itself failed).
func (c *conn) closeNow(code int, reason string) {
	c.closeOnce.Do(func() {
		c.markClosed(code, reason)
		go c.doClose()
	})
}

func (c *conn) markClosed(code int, reason string) {
	if len(reason) > 120 {
		reason = reason[:120]
	}
	c.mu.Lock()
	c.closed = true
	c.closeCode, c.closeReason = code, reason
	c.mu.Unlock()
}

// doClose runs the close handshake once and releases the reader/writer.
func (c *conn) doClose() {
	c.doCloseOnce.Do(func() {
		c.mu.Lock()
		code, reason := c.closeCode, c.closeReason
		c.mu.Unlock()
		_ = c.ws.Close(websocket.StatusCode(code), reason)
		if c.cancel != nil {
			c.cancel()
		}
	})
}

// Info implements room.Conn.
func (c *conn) Info() room.ConnInfo {
	info := room.ConnInfo{RemoteAddr: c.remote}
	info.WSPing = func(ctx context.Context) (float64, error) {
		t0 := c.clock.Mono()
		if err := c.ws.Ping(ctx); err != nil {
			return 0, err
		}
		return c.clock.Mono() - t0, nil
	}
	if c.netConn != nil {
		nc := c.netConn
		info.KernelRTT = func() (float64, float64, bool) {
			k, err := ReadKernelRTT(nc)
			if err != nil || !k.Valid {
				return 0, 0, false
			}
			return k.SRTTMs, k.RTTVarMs, true
		}
	}
	return info
}
