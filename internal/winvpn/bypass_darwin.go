//go:build darwin

package winvpn

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"syscall"

	"golang.org/x/net/route"
)

type darwinDefault struct {
	index           int
	gateway, source string
	scoped, usable  bool
}

func darwinDefaults(ipv4 bool) ([]darwinDefault, error) {
	family := syscall.AF_INET6
	if ipv4 {
		family = syscall.AF_INET
	}
	buf, err := route.FetchRIB(family, route.RIBTypeRoute, 0)
	if err != nil {
		return nil, err
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, buf)
	if err != nil {
		return nil, err
	}
	var defaults []darwinDefault
	for _, message := range messages {
		m, ok := message.(*route.RouteMessage)
		if !ok {
			continue
		}
		p := darwinRIBPrefix(m)
		if !p.IsValid() || p.Bits() != 0 || p.Addr().Is4() != ipv4 || len(m.Addrs) <= syscall.RTAX_GATEWAY {
			continue
		}
		gateway := ribIP(m.Addrs[syscall.RTAX_GATEWAY])
		if gateway == nil {
			continue
		}
		d := darwinDefault{index: m.Index, gateway: gateway.String(), scoped: m.Flags&darwinRouteIfScope != 0,
			usable: m.Flags&syscall.RTF_UP != 0 && m.Flags&(syscall.RTF_REJECT|syscall.RTF_BLACKHOLE) == 0}
		if len(m.Addrs) > syscall.RTAX_IFA {
			if source := ribIP(m.Addrs[syscall.RTAX_IFA]); source != nil {
				d.source = source.String()
			}
		}
		defaults = append(defaults, d)
	}
	return defaults, nil
}

// Consult physical default routes and current interface addresses, never an
// unbound socket: a stale scoped route can itself select a deleted source IP.
func selectBypassExit(dns, requested string, _ bypassSelection) (bypassSelection, error) {
	host, _, _ := net.SplitHostPort(dns)
	ipv4 := net.ParseIP(host).To4() != nil
	device := ""
	byDevice := net.ParseIP(requested) == nil
	if byDevice {
		device, requested = requested, ""
	}
	selected, err := darwinPhysicalExit(ipv4, device, requested)
	if err != nil {
		return bypassSelection{}, err
	}
	if byDevice {
		// The DNS family selects the physical device. Prepare the opposite family
		// only if the same device has an address and a physical default for it.
		other, err := darwinPhysicalExit(!ipv4, selected.Name, "")
		if err != nil && !errors.Is(err, errNoPhysicalDefault) {
			return bypassSelection{}, err
		}
		selected.OtherIP, selected.OtherGateway = other.IP, other.Gateway
	}
	return selected, nil
}

var errNoPhysicalDefault = errors.New("no current physical default route/address for reconnect bypass")

func darwinPhysicalExit(ipv4 bool, device, requested string) (bypassSelection, error) {
	defaults, err := darwinDefaults(ipv4)
	if err != nil {
		return bypassSelection{}, err
	}
	// Prefer the physical non-scoped default. Scoped fallback supports an
	// explicitly selected secondary interface without using a captured route.
	for _, scoped := range []bool{false, true} {
		if scoped && requested == "" && device == "" {
			continue
		}
		for _, d := range defaults {
			if d.scoped != scoped || !d.usable {
				continue
			}
			iface, err := net.InterfaceByIndex(d.index)
			if err != nil || (device != "" && iface.Name != device) {
				continue
			}
			ip, err := interfaceSource(*iface, ipv4, d.source, requested)
			if err != nil {
				continue
			}
			gateway := d.gateway
			if g := net.ParseIP(gateway); g != nil && g.To4() == nil && g.IsLinkLocalUnicast() {
				gateway += "%" + iface.Name
			}
			return bypassSelection{Name: iface.Name, Index: iface.Index, IP: ip, Gateway: gateway}, nil
		}
	}
	return bypassSelection{}, fmt.Errorf("%w (interface %q, source %q)", errNoPhysicalDefault, device, requested)
}

func inspectScopedDefault(s bypassSelection) (*darwinDefault, error) {
	defaults, err := darwinDefaults(net.ParseIP(s.IP).To4() != nil)
	if err != nil {
		return nil, err
	}
	for _, d := range defaults {
		if d.scoped && d.index == s.Index {
			return &d, nil
		}
	}
	return nil, nil
}

func scopedSourceCurrent(d darwinDefault, addresses []net.IP) bool {
	source := net.ParseIP(d.source)
	for _, ip := range addresses {
		if source != nil && ip.Equal(source) {
			return true
		}
	}
	return false
}

func scopedRouteReady(s bypassSelection) (bool, error) {
	iface, err := net.InterfaceByName(s.Name)
	if err != nil || iface.Index != s.Index || iface.Flags&net.FlagUp == 0 {
		return false, nil
	}
	d, err := inspectScopedDefault(s)
	if err != nil || d == nil {
		return false, err
	}
	if !d.usable || !sameDarwinGateway(d.gateway, s.Gateway, s.Name) {
		return false, nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return false, err
	}
	var ips []net.IP
	for _, addr := range addrs {
		if ip, _, err := net.ParseCIDR(addr.String()); err == nil {
			ips = append(ips, ip)
		}
	}
	return scopedSourceCurrent(*d, ips), nil
}

func prepareInterfaceBypass(s bypassSelection) (*bypassRoute, error) {
	return prepareScopedFamilies(s, prepareScopedBypass)
}

func prepareScopedBypass(s bypassSelection) (*bypassRoute, error) {
	existing, err := inspectScopedDefault(s)
	if err != nil {
		return nil, err
	}
	ready := func() (bool, error) { return scopedRouteReady(s) }
	if existing != nil {
		valid, err := ready()
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, fmt.Errorf("existing scoped default on %s has stale source %s or gateway %s; not owned by this VPN session, left untouched", s.Name, existing.source, existing.gateway)
		}
		return &bypassRoute{ready: ready}, nil
	}
	family, prefix := "-inet", netip.MustParsePrefix("0.0.0.0/0")
	if net.ParseIP(s.IP).To4() == nil {
		family, prefix = "-inet6", netip.MustParsePrefix("::/0")
	}
	owned := ownedRoute{prefix: prefix, iface: s.Name, index: s.Index, gateway: s.Gateway}
	ledger := &routeLedger{
		// Cleanup deliberately ignores address validity and UP: it must still remove
		// our obsolete route after DHCP, link-down, or interface address removal.
		present: func(ownedRoute) (bool, error) {
			iface, err := net.InterfaceByName(s.Name)
			if err != nil || iface.Index != s.Index {
				return false, nil
			}
			d, err := inspectScopedDefault(s)
			return d != nil && net.ParseIP(d.source).Equal(net.ParseIP(s.IP)) && sameDarwinGateway(d.gateway, s.Gateway, s.Name), err
		},
		ready: func(ownedRoute) (bool, error) { return ready() },
		command: func(op string, _ ownedRoute) routeMutation {
			args := []string{"-n", op, family, "-ifscope", s.Name, "-net", prefix.String(), s.Gateway}
			if op == "add" {
				args = append(args, "-ifa", s.IP)
			}
			out, err := exec.Command("route", args...).CombinedOutput()
			return parseDarwinRouteMutation(op, owned, string(out), err)
		},
	}
	b := &bypassRoute{close: ledger.cleanup, ready: ready}
	if err := ledger.add(owned); err != nil {
		return b, errors.Join(err, ledger.cleanup())
	}
	return b, nil
}
