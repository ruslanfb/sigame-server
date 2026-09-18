package ws

import (
	"errors"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestReadKernelRTT exchanges a few bytes over loopback so the kernel has an
// RTT sample, then reads it. On unsupported platforms the sentinel is expected.
func TestReadKernelRTT(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 16)
		for i := 0; i < 5; i++ {
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			if _, err := c.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	buf := make([]byte, 16)
	for i := 0; i < 5; i++ {
		_, err = client.Write([]byte("ping"))
		require.NoError(t, err)
		_, err = io.ReadFull(client, buf[:4])
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond)
	}

	rtt, err := ReadKernelRTT(client)
	switch runtime.GOOS {
	case "linux", "darwin":
		require.NoError(t, err)
		require.True(t, rtt.Valid, "kernel should have an RTT sample after a few round trips")
		require.Less(t, rtt.SRTTMs, 100.0)
		require.GreaterOrEqual(t, rtt.RTTVarMs, 0.0)
	default:
		require.True(t, errors.Is(err, ErrKernelRTTUnsupported))
	}
	<-done
}

func TestReadKernelRTTRejectsNonSocket(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_, err := ReadKernelRTT(a)
	require.Error(t, err)
}
