package winvpn

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
)

type bypassSelection struct {
	Name, IP, Gateway string
	Index             int
}

type bypassRoute struct {
	close           func() error
	ready           func() (bool, error)
	interfaceScoped bool // Remains valid across address changes on the same device.
}

// BypassSession owns only routes created by this session. Refresh never stops TUN.
type BypassSession struct {
	LocalIP        string
	mu             sync.Mutex
	dns, requested string
	selection      bypassSelection
	route          *bypassRoute
	closed         bool
	revision       uint64
	selectExit     func(string, string, bypassSelection) (bypassSelection, error)
	prepare        func(bypassSelection) (*bypassRoute, error)
}

// CurrentExit returns a consistent snapshot to the privileged socket binder.
func (b *BypassSession) CurrentExit() (device, ip, dns string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.route == nil {
		return "", "", "", fmt.Errorf("VPN bypass is unavailable")
	}
	return b.selection.Name, b.LocalIP, b.dns, nil
}

func (b *BypassSession) closeRoute() error {
	if b.route != nil && b.route.close != nil {
		if err := b.route.close(); err != nil {
			return err
		}
	}
	b.route = nil
	return nil
}

func (b *BypassSession) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return b.closeRoute()
}

// A revision also changes when a route is rebuilt with the same source IP.
func (b *BypassSession) Refresh() (string, uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return "", b.revision, fmt.Errorf("VPN bypass is closed")
	}
	next, err := b.selectExit(b.dns, b.requested, b.selection)
	if err == nil && b.route != nil && (next == b.selection ||
		(b.route.interfaceScoped && next.Index == b.selection.Index && next.Name == b.selection.Name)) {
		var ready bool
		ready, err = b.route.ready()
		if err == nil && ready {
			if next != b.selection {
				b.selection, b.LocalIP = next, next.IP
				b.revision++
			}
			return b.LocalIP, b.revision, nil
		}
	}
	// Remove owned obsolete scoped routes even while Wi-Fi is disconnected.
	// Otherwise they can keep selecting a deleted interface address system-wide.
	if cleanupErr := b.closeRoute(); cleanupErr != nil {
		return "", b.revision, errors.Join(err, fmt.Errorf("clean obsolete bypass: %w", cleanupErr))
	}
	if err != nil {
		return "", b.revision, err
	}
	b.selection = next
	b.route, err = b.prepare(next) // Retain partial preparation for cleanup retry.
	if err != nil {
		return "", b.revision, err
	}
	b.LocalIP = next.IP
	b.revision++
	return b.LocalIP, b.revision, nil
}

func PrepareBypass(dns, requestedLocal string) (*BypassSession, error) {
	host, _, err := net.SplitHostPort(dns)
	if err != nil || net.ParseIP(host) == nil {
		return nil, fmt.Errorf("bypass DNS requires an IP and port")
	}
	if requestedLocal != "" && net.ParseIP(requestedLocal) == nil {
		return nil, fmt.Errorf("invalid bypass source IP")
	}
	b := &BypassSession{dns: dns, requested: requestedLocal, selectExit: selectBypassExit, prepare: prepareInterfaceBypass}
	if _, _, err := b.Refresh(); err != nil {
		if cleanupErr := b.Close(); cleanupErr != nil {
			return b, errors.Join(err, cleanupErr)
		}
		return nil, err
	}
	return b, nil
}

func interfaceSource(iface net.Interface, ipv4 bool, preferred, requested string) (string, error) {
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || !usableBypassInterface(iface.Name) {
		return "", fmt.Errorf("physical interface %s is unavailable", iface.Name)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	var first string
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || (ip.To4() != nil) != ipv4 {
			continue
		}
		if requested != "" {
			if ip.Equal(net.ParseIP(requested)) {
				return ip.String(), nil
			}
			continue
		}
		if ip.Equal(net.ParseIP(preferred)) {
			return ip.String(), nil
		}
		if first == "" {
			first = ip.String()
		}
	}
	if first != "" {
		return first, nil
	}
	return "", fmt.Errorf("no current physical address on %s (requested -local %q)", iface.Name, requested)
}

func usableBypassInterface(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"gonc", "utun", "tun", "tap", "wg", "ppp", "ipsec"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}
