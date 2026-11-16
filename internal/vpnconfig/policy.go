package vpnconfig

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Routes, never the reachability probe or a legacy checkbox, define ownership.
func ParseRoutes(value string) ([]string, error) {
	lines := strings.Fields(value)
	if len(lines) == 0 {
		lines = []string{"0.0.0.0/1", "128.0.0.0/1", "::/0"}
	}
	var result []string
	seen := map[string]bool{}
	for _, line := range lines {
		prefix, err := netip.ParsePrefix(line)
		if err != nil || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" {
			return nil, fmt.Errorf("invalid VPN route %q", line)
		}
		cidr := prefix.Masked().String()
		if !seen[cidr] {
			seen[cidr] = true
			result = append(result, cidr)
		}
	}
	return result, nil
}

// ShouldRejectIPv6 does not change route ownership. A new tunnel probe supplies
// a fresh result when the helper/stack is recreated after reconnect.
func ShouldRejectIPv6(routes []string, available bool) bool {
	return HasIPv6(routes) && !available
}

func HasIPv6(routes []string) bool {
	for _, value := range routes {
		p, err := netip.ParsePrefix(value)
		if err == nil && p.Addr().Is6() {
			return true
		}
	}
	return false
}

// More-specific routes take precedence over the native default without replacing
// it. In particular, macOS must never add/delete the user's native ::/0 route.
func DesktopRoutes(routes []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, value := range routes {
		values := []string{value}
		if value == "::/0" {
			values = []string{"::/1", "8000::/1"}
		} else if value == "0.0.0.0/0" {
			values = []string{"0.0.0.0/1", "128.0.0.0/1"}
		}
		for _, cidr := range values {
			if !seen[cidr] {
				seen[cidr] = true
				result = append(result, cidr)
			}
		}
	}
	return result
}

// DNS selection is independent of routes. An unavailable IPv6 resolver is not
// replaced by the OS's physical-network DNS. Always return an explicit fallback.
func SelectDNS(value string, ipv6Available bool) (servers []string, fallback bool, err error) {
	lines := strings.Fields(value)
	if len(lines) == 0 {
		lines = []string{"8.8.8.8", "2001:4860:4860::8888"}
	}
	for _, line := range lines {
		host := line
		if h, _, e := net.SplitHostPort(line); e == nil {
			host = h
		}
		addr, e := netip.ParseAddr(strings.Trim(host, "[]"))
		if e != nil || addr.IsUnspecified() || addr.IsMulticast() || addr.Zone() != "" {
			return nil, false, fmt.Errorf("invalid VPN DNS server %q (an IP address is required)", line)
		}
		if addr.Is6() && !ipv6Available {
			continue
		}
		servers = append(servers, line)
	}
	if len(servers) == 0 {
		return []string{"8.8.8.8"}, true, nil
	}
	return servers, false, nil
}
