//go:build darwin

package ws

import "golang.org/x/sys/unix"

// readKernelRTT uses TCP_CONNECTION_INFO: Srtt/Rttvar are reported in milliseconds.
func readKernelRTT(fd uintptr) (KernelRTT, error) {
	info, err := unix.GetsockoptTCPConnectionInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_CONNECTION_INFO)
	if err != nil {
		return KernelRTT{}, err
	}
	if info.Srtt == 0 && info.Rttcur == 0 {
		return KernelRTT{}, nil
	}
	srtt := float64(info.Srtt)
	if srtt == 0 {
		srtt = float64(info.Rttcur)
	}
	return KernelRTT{
		SRTTMs:   srtt,
		RTTVarMs: float64(info.Rttvar),
		Valid:    true,
	}, nil
}
