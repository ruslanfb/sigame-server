//go:build !linux && !darwin

package ws

// readKernelRTT is unavailable on this platform (e.g. Windows); the buzzer
// falls back to the WebSocket ping/pong anchor.
func readKernelRTT(fd uintptr) (KernelRTT, error) {
	_ = fd
	return KernelRTT{}, ErrKernelRTTUnsupported
}
