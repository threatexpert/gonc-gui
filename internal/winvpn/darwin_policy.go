package winvpn

import (
	"fmt"
	"net/netip"
	"strings"
)

// Darwin ABI flags, kept with the platform-independent policy so its tests run
// on every host. routes_darwin_test.go checks them against syscall constants.
const (
	darwinRouteUp           = 0x1
	darwinRouteGateway      = 0x2
	darwinRouteHost         = 0x4
	darwinRouteReject       = 0x8
	darwinRouteDynamic      = 0x10
	darwinRouteCloning      = 0x100
	darwinRouteLLInfo       = 0x400
	darwinRouteStatic       = 0x800
	darwinRouteBlackhole    = 0x1000
	darwinRouteProtoCloning = 0x10000
	darwinRouteWasCloned    = 0x20000
	darwinRouteIfScope      = 0x1000000
)

func stableDarwinRoute(flags int, prefix netip.Prefix) bool {
	return ownedDarwinRoute(flags, prefix) && flags&darwinRouteUp != 0 && flags&(darwinRouteReject|darwinRouteBlackhole) == 0
}

// Identity/ownership matching deliberately does not require a usable route.
// An owned route that became REJECT/down must still be found during rollback.
func ownedDarwinRoute(flags int, prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return false
	}
	host := prefix.Bits() == prefix.Addr().BitLen()
	const required = darwinRouteStatic
	const excluded = darwinRouteWasCloned | darwinRouteIfScope | darwinRouteDynamic
	if flags&required != required || flags&excluded != 0 {
		return false
	}
	// Both ARP (IPv4) and ND (IPv6) attach LLINFO to manual on-link host
	// routes. STATIC + LLINFO is not the same as a temporary WASCLONED entry.
	if flags&darwinRouteLLInfo != 0 && !(host && flags&darwinRouteGateway == 0) {
		return false
	}
	if host {
		return flags&darwinRouteHost != 0 && flags&(darwinRouteCloning|darwinRouteProtoCloning) == 0
	}
	return flags&darwinRouteHost == 0
}

type darwinRouteLookup struct {
	iface, gateway string
}

func darwinRouteSurvivesCapture(peer netip.Addr, native netip.Prefix, capture []netip.Prefix) bool {
	if !native.IsValid() || !native.Contains(peer) {
		return false
	}
	for _, p := range capture {
		// Equal specificity is ambiguous; do not rely on insertion order.
		if p.Contains(peer) && p.Bits() >= native.Bits() {
			return false
		}
	}
	return true
}

func parseDarwinRouteLookup(output string) (darwinRouteLookup, error) {
	var result darwinRouteLookup
	flags := ""
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch key {
		case "interface":
			result.iface = strings.TrimSpace(value)
		case "gateway":
			result.gateway = strings.TrimSpace(value)
		case "flags":
			flags = strings.TrimSpace(value)
		}
	}
	flagSet := make(map[string]bool)
	for _, flag := range strings.Split(strings.Trim(flags, "<>"), ",") {
		flagSet[strings.TrimSpace(flag)] = true
	}
	if result.iface == "" || !flagSet["UP"] || flagSet["REJECT"] || flagSet["BLACKHOLE"] {
		return result, fmt.Errorf("no usable route: %s", strings.TrimSpace(output))
	}
	if flagSet["GATEWAY"] {
		addr, err := netip.ParseAddr(result.gateway)
		if err != nil {
			return result, fmt.Errorf("invalid route gateway %q", result.gateway)
		}
		if addr.Is6() && addr.IsLinkLocalUnicast() && addr.Zone() == "" {
			addr = addr.WithZone(result.iface)
		}
		result.gateway = addr.String()
	} else {
		// On-link lookups can print a link# index or a neighbour MAC address.
		result.gateway = ""
	}
	return result, nil
}

func planDarwinBypass(ips []string, tun string, lookup func(string) (darwinRouteLookup, error)) ([]ownedRoute, error) {
	var routes []ownedRoute
	seen := map[netip.Addr]bool{}
	for _, ip := range ips {
		addr, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback() {
			return nil, fmt.Errorf("invalid tunnel peer IP %q", ip)
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		native, err := lookup(addr.String())
		if err != nil {
			return nil, fmt.Errorf("resolve bypass for %s: %w", addr, err)
		}
		if native.iface == "" || native.iface == tun {
			return nil, fmt.Errorf("tunnel peer %s already routes to VPN interface %s; refusing recursive bypass", addr, native.iface)
		}
		routes = append(routes, ownedRoute{prefix: netip.PrefixFrom(addr, addr.BitLen()), iface: native.iface, gateway: native.gateway})
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("no tunnel peer IP available to protect from VPN capture")
	}
	return routes, nil
}

func verifyDarwinBypass(routes []ownedRoute, tun string, lookup func(string) (darwinRouteLookup, error), trace func(string)) error {
	for _, r := range routes {
		prefix := r.prefix
		actual, err := lookup(prefix.Addr().String())
		if err != nil {
			return fmt.Errorf("verify VPN bypass %s: %w", r.prefix, err)
		}
		if trace != nil {
			trace(fmt.Sprintf("VPN bypass %s: actual interface=%s gateway=%s; expected interface=%s gateway=%s", prefix.Addr(), actual.iface, actual.gateway, r.iface, r.gateway))
		}
		if actual.iface == tun || actual.iface != r.iface || !sameDarwinGateway(actual.gateway, r.gateway, r.iface) {
			return fmt.Errorf("VPN bypass verification failed for %s: actual %s via %s, expected %s via %s; VPN is not ready", prefix.Addr(), actual.iface, actual.gateway, r.iface, r.gateway)
		}
	}
	return nil
}

func sameDarwinGateway(a, b, iface string) bool {
	if a == "" || b == "" {
		return a == b
	}
	normalize := func(value string) netip.Addr {
		addr, _ := netip.ParseAddr(value)
		if addr.Is6() && addr.IsLinkLocalUnicast() && addr.Zone() == "" {
			addr = addr.WithZone(iface)
		}
		return addr.Unmap()
	}
	aa, bb := normalize(a), normalize(b)
	return aa.IsValid() && bb.IsValid() && aa == bb
}

// Preserve resolver priority, but compare IP values rather than IPv6 spelling.
func sameDarwinDNS(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		aa, ea := netip.ParseAddr(strings.TrimSpace(a[i]))
		bb, eb := netip.ParseAddr(strings.TrimSpace(b[i]))
		if ea != nil || eb != nil || aa.Unmap() != bb.Unmap() {
			return false
		}
	}
	return true
}

// The first verification detects capture-induced recursion before changing DNS;
// the second catches conflicts introduced by DNS host routes before readiness.
type darwinNetworkSteps struct {
	bypass, capture, dns, verifyBypass func() error
}

func applyDarwinNetwork(steps darwinNetworkSteps) error {
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"configure bypass routes", steps.bypass},
		{"configure capture routes", steps.capture},
		{"verify bypass after capture", steps.verifyBypass},
		{"configure DNS", steps.dns},
		{"verify bypass after DNS", steps.verifyBypass},
	} {
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}
