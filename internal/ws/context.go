package ws

import (
	"context"
	"crypto/tls"
	"net"
)

type connContextKey struct{}

// ConnContextKey is the context key under which ConnContext stores the raw
// net.Conn of an HTTP connection.
var ConnContextKey = connContextKey{}

// ConnContext is meant for http.Server.ConnContext: it records the raw
// net.Conn so that the WebSocket handler can read the kernel RTT of the TCP
// socket (the strongest tamper-proof anchor of the buzzer clock model).
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, ConnContextKey, c)
}

// connFromContext returns the TCP connection recorded by ConnContext,
// unwrapping TLS. It returns nil when the server was not configured with
// ConnContext.
func connFromContext(ctx context.Context) net.Conn {
	c, _ := ctx.Value(ConnContextKey).(net.Conn)
	if tc, ok := c.(*tls.Conn); ok {
		return tc.NetConn()
	}
	return c
}
