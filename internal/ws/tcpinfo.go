package ws

import (
	"errors"
	"net"
	"syscall"
)

// KernelRTT is the transport-level round-trip estimate kept by the OS TCP
// stack for a connection. It cannot be influenced by JavaScript in the
// browser, which makes it the strongest anchor for the buzzer's RTT model.
type KernelRTT struct {
	SRTTMs   float64 // smoothed RTT
	RTTVarMs float64 // RTT variance (mean deviation)
	Valid    bool
}

// ErrKernelRTTUnsupported is returned on platforms without a TCP info socket option.
var ErrKernelRTTUnsupported = errors.New("ws: kernel RTT not supported on this platform")

// ReadKernelRTT reads the kernel's RTT estimate for a TCP connection.
// conn must be a *net.TCPConn (or wrap one via syscall.Conn).
func ReadKernelRTT(conn net.Conn) (KernelRTT, error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return KernelRTT{}, errors.New("ws: connection does not expose a raw socket")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return KernelRTT{}, err
	}
	var out KernelRTT
	var inner error
	if err := raw.Control(func(fd uintptr) { out, inner = readKernelRTT(fd) }); err != nil {
		return KernelRTT{}, err
	}
	return out, inner
}
