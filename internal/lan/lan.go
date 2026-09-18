// Package lan discovers the server's LAN addresses and builds join URLs so
// that hosts can share them (printed at startup, returned by /api/v1/system/info).
// mDNS advertisement is intentionally deferred to a later version (no dependency
// pinned); the Advertise stub keeps the call site stable.
package lan

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
)

// Address is one usable IPv4/IPv6 address of a network interface.
type Address struct {
	Interface string `json:"interface"`
	IP        string `json:"ip"`
	IsIPv6    bool   `json:"isIPv6"`
	Private   bool   `json:"private" doc:"RFC 1918 / ULA address (typical LAN)"`
}

// Addresses returns non-loopback, up interfaces' unicast addresses, private
// IPv4 first, sorted for stable output.
func Addresses() ([]Address, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("lan: interfaces: %w", err)
	}
	var out []Address
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() || ipn.IP.IsUnspecified() {
				continue
			}
			ip := ipn.IP
			v6 := ip.To4() == nil
			out = append(out, Address{Interface: ifc.Name, IP: ip.String(), IsIPv6: v6, Private: ip.IsPrivate()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.IsIPv6 != b.IsIPv6 {
			return !a.IsIPv6
		}
		if a.Private != b.Private {
			return a.Private
		}
		return a.IP < b.IP
	})
	return out, nil
}

// JoinURLs builds base URLs (scheme://host:port) for every address given the
// listen address (":8080" or "0.0.0.0:8080" → all addresses; "192.168.1.5:8080"
// → only that host). publicURL, when non-empty, is returned first.
func JoinURLs(listenAddr, publicURL string, addrs []Address) []string {
	var urls []string
	if publicURL != "" {
		urls = append(urls, publicURL)
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return urls
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return append(urls, fmt.Sprintf("http://%s", net.JoinHostPort(host, port)))
	}
	for _, a := range addrs {
		urls = append(urls, fmt.Sprintf("http://%s", net.JoinHostPort(a.IP, port)))
	}
	return urls
}

// Advertise is a placeholder for mDNS (_sigame._tcp) service advertisement.
// It returns immediately; a future version will announce the service until ctx
// is cancelled.
func Advertise(ctx context.Context, instance string, port int) (stop func(), err error) {
	_ = ctx
	_ = instance
	_ = port
	return func() {}, nil
}

// Banner renders a human-readable startup banner (Russian, for hosts).
func Banner(urls []string) string {
	var b strings.Builder
	b.WriteString("SIGame server запущен. Адреса для подключения:\n")
	for _, u := range urls {
		fmt.Fprintf(&b, "  %s   (API docs: %s/docs)\n", u, u)
	}
	return b.String()
}
