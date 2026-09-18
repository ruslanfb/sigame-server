//go:build linux

package ws

import "golang.org/x/sys/unix"

// readKernelRTT uses TCP_INFO: Rtt and Rttvar are reported in microseconds.
func readKernelRTT(fd uintptr) (KernelRTT, error) {
	info, err := unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	if err != nil {
		return KernelRTT{}, err
	}
	if info.Rtt == 0 {
		return KernelRTT{}, nil
	}
	return KernelRTT{
		SRTTMs:   float64(info.Rtt) / 1000,
		RTTVarMs: float64(info.Rttvar) / 1000,
		Valid:    true,
	}, nil
}
