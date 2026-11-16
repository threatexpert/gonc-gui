//go:build darwin

package winvpn

import (
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/net/route"
)

func ribIP(a route.Addr) net.IP {
	switch a := a.(type) {
	case *route.Inet4Addr:
		return net.IP(a.IP[:])
	case *route.Inet6Addr:
		return net.IP(a.IP[:])
	}
	return nil
}

// Read the kernel RIB, not route(8)'s exit status (which can be zero on EEXIST).
func routeInstalled(r ownedRoute) (bool, error) {
	prefix, err := netip.ParsePrefix(r.cidr)
	if err != nil {
		return false, err
	}
	iface, err := net.InterfaceByName(r.iface)
	if err != nil {
		return false, nil // A removed interface cannot still own this route.
	}
	if r.index != 0 && iface.Index != r.index {
		return false, nil
	}
	buf, err := route.FetchRIB(syscall.AF_UNSPEC, route.RIBTypeRoute, 0)
	if err != nil {
		return false, err
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, buf)
	if err != nil {
		return false, err
	}
	for _, msg := range msgs {
		m, ok := msg.(*route.RouteMessage)
		if !ok || m.Index != iface.Index || m.Flags&syscall.RTF_UP == 0 || len(m.Addrs) <= syscall.RTAX_NETMASK {
			continue
		}
		dst := ribIP(m.Addrs[syscall.RTAX_DST])
		addr, ok := netip.AddrFromSlice(dst)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		bits := 0
		if m.Flags&syscall.RTF_HOST != 0 {
			bits = addr.BitLen()
		} else if mask := ribIP(m.Addrs[syscall.RTAX_NETMASK]); mask != nil {
			var width int
			bits, width = net.IPMask(mask).Size()
			if width != addr.BitLen() {
				continue
			}
		}
		if netip.PrefixFrom(addr, bits).Masked() != prefix.Masked() {
			continue
		}
		if r.gateway != "" {
			gateway, e := netip.ParseAddr(r.gateway)
			actual, ok := netip.AddrFromSlice(ribIP(m.Addrs[syscall.RTAX_GATEWAY]))
			if e != nil || !ok || gateway.WithZone("").Unmap() != actual.Unmap() {
				continue
			}
		}
		return true, nil
	}
	return false, nil
}

func (s *Session) addOwnedRoute(r ownedRoute) error {
	iface, err := net.InterfaceByName(r.iface)
	if err != nil {
		return err
	}
	r.index = iface.Index
	if s.routeLedger == nil {
		s.routeLedger = &routeLedger{present: routeInstalled, command: func(op string, r ownedRoute) error { return run("route", routeArgs(op, r)...) }}
	}
	return s.routeLedger.add(r)
}

// Resolve before installing capture routes, including IPv6 tunnel peers.
func nativeRouteTo(ip string) (iface, gateway string, err error) {
	family := "-inet"
	if strings.Contains(ip, ":") {
		family = "-inet6"
	}
	output, err := exec.Command("route", "-n", "get", family, ip).CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("route to %s: %w: %s", ip, err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch key {
		case "interface":
			iface = strings.TrimSpace(value)
		case "gateway":
			gateway = strings.TrimSpace(value)
		}
	}
	if iface == "" {
		return "", "", fmt.Errorf("no native route to %s", ip)
	}
	if _, err := netip.ParseAddr(gateway); err != nil {
		gateway = ""
	} // On-link peer.
	return iface, gateway, nil
}
