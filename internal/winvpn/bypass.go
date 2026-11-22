package winvpn

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
)

type bypassSelection struct {
	Name, IP, Gateway     string
	Index                 int
	OtherIP, OtherGateway string
}

type bypassRoute struct {
	close           func() error
	ready           func() (bool, error)
	interfaceScoped bool // Remains valid across address changes on the same device.
}

// A dual-stack exit may need two scoped defaults. Keep ownership of the
// first route if preparing the second fails so the caller can unwind both.
func prepareScopedFamilies(s bypassSelection, prepare func(bypassSelection) (*bypassRoute, error)) (*bypassRoute, error) {
	first, err := prepare(s)
	if err != nil || s.OtherIP == "" {
		return first, err
	}
	other := bypassSelection{Name: s.Name, Index: s.Index, IP: s.OtherIP, Gateway: s.OtherGateway}
	second, err := prepare(other)
	both := &bypassRoute{
		ready: func() (bool, error) {
			ok, err := first.ready()
			if err != nil || !ok {
				return ok, err
			}
			return second.ready()
		},
		close: func() error {
			var errs []error
			for _, route := range []*bypassRoute{second, first} {
				if route != nil && route.close != nil {
					errs = append(errs, route.close())
				}
			}
			return errors.Join(errs...)
		},
	}
	return both, err
}

// BypassSession owns only routes created by this session. Refresh never stops TUN.
type BypassSession struct {
	Local          string // Device name for an explicit interface, otherwise the selected IP.
	byDevice       bool
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
func (b *BypassSession) CurrentExit() (device, local, dns string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.route == nil || b.route.ready == nil {
		return "", "", "", fmt.Errorf("VPN bypass is unavailable")
	}
	return b.selection.Name, b.Local, b.dns, nil
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
	if err == nil && b.route != nil && b.route.ready != nil && (next == b.selection ||
		(b.route.interfaceScoped && next.Index == b.selection.Index && next.Name == b.selection.Name && (next.OtherIP == "") == (b.selection.OtherIP == ""))) {
		var ready bool
		ready, err = b.route.ready()
		if err == nil && ready {
			if next != b.selection {
				b.selection = next
				b.Local = b.source(next)
				b.revision++
			}
			return b.Local, b.revision, nil
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
		if b.route != nil {
			b.route.ready = nil
		}
		return "", b.revision, errors.Join(err, b.closeRoute())
	}
	b.Local = b.source(next)
	b.revision++
	return b.Local, b.revision, nil
}

func (b *BypassSession) source(s bypassSelection) string {
	if b.byDevice {
		return s.Name
	}
	return s.IP
}

func PrepareBypass(dns, requestedLocal string) (*BypassSession, error) {
	host, _, err := net.SplitHostPort(dns)
	if err != nil || net.ParseIP(host) == nil {
		return nil, fmt.Errorf("bypass DNS requires an IP and port")
	}
	if requestedLocal != "" && net.ParseIP(requestedLocal) == nil {
		if _, err := net.InterfaceByName(requestedLocal); err != nil {
			return nil, err
		}
	}
	b := &BypassSession{dns: dns, requested: requestedLocal, byDevice: requestedLocal != "" && net.ParseIP(requestedLocal) == nil, selectExit: selectBypassExit, prepare: prepareInterfaceBypass}
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
