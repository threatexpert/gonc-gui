//go:build darwin

package winvpn

import (
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
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
	if _, err := netip.ParsePrefix(r.cidr); err != nil {
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
	return matchesDarwinRoute(r, *iface, msgs), nil
}

func matchesDarwinRoute(r ownedRoute, iface net.Interface, msgs []route.Message) bool {
	prefix, err := netip.ParsePrefix(r.cidr)
	if err != nil || (r.index != 0 && r.index != iface.Index) {
		return false
	}
	for _, msg := range msgs {
		m, ok := msg.(*route.RouteMessage)
		if !ok || m.Index != iface.Index || !stableDarwinRoute(m.Flags, prefix.Bits() == prefix.Addr().BitLen()) || len(m.Addrs) <= syscall.RTAX_DST {
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
		} else if len(m.Addrs) > syscall.RTAX_NETMASK && m.Addrs[syscall.RTAX_NETMASK] != nil {
			mask := ribIP(m.Addrs[syscall.RTAX_NETMASK])
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
			if m.Flags&syscall.RTF_GATEWAY == 0 || len(m.Addrs) <= syscall.RTAX_GATEWAY {
				continue
			}
			actual, ok := netip.AddrFromSlice(ribIP(m.Addrs[syscall.RTAX_GATEWAY]))
			if !ok {
				continue
			}
			if a, ok := m.Addrs[syscall.RTAX_GATEWAY].(*route.Inet6Addr); ok && a.ZoneID != 0 {
				zone := strconv.Itoa(a.ZoneID)
				if a.ZoneID == iface.Index {
					zone = iface.Name
				}
				actual = actual.WithZone(zone)
			}
			if !sameDarwinGateway(r.gateway, actual.String(), r.iface) {
				continue
			}
		} else if m.Flags&syscall.RTF_GATEWAY != 0 {
			continue
		}
		return true
	}
	return false
}

func (s *Session) addOwnedRoute(r ownedRoute) error {
	iface, err := net.InterfaceByName(r.iface)
	if err != nil {
		return err
	}
	r.index = iface.Index
	if s.routeLedger == nil {
		s.routeLedger = &routeLedger{present: routeInstalled, command: runDarwinRoute, trace: s.trace}
	}
	return s.routeLedger.add(r)
}

// Resolve before installing capture routes, including IPv6 tunnel peers.
func nativeRouteTo(ip string) (darwinRouteLookup, error) {
	family := "-inet"
	if strings.Contains(ip, ":") {
		family = "-inet6"
	}
	output, err := exec.Command("route", "-n", "get", family, ip).CombinedOutput()
	if err != nil {
		return darwinRouteLookup{}, fmt.Errorf("route to %s: %w: %s", ip, err, output)
	}
	return parseDarwinRouteLookup(string(output))
}

func runDarwinRoute(op string, r ownedRoute) error {
	output, err := exec.Command("route", routeArgs(op, r)...).CombinedOutput()
	return checkDarwinRouteOutput(op, r, string(output), err)
}
