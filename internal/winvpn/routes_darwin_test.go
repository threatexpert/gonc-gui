//go:build darwin

package winvpn

import (
	"net"
	"net/netip"
	"syscall"
	"testing"

	"golang.org/x/net/route"
)

func TestDarwinRouteFlagABI(t *testing.T) {
	for _, pair := range [][2]int{
		{darwinRouteUp, syscall.RTF_UP}, {darwinRouteGateway, syscall.RTF_GATEWAY},
		{darwinRouteHost, syscall.RTF_HOST}, {darwinRouteReject, syscall.RTF_REJECT},
		{darwinRouteDynamic, syscall.RTF_DYNAMIC}, {darwinRouteCloning, syscall.RTF_CLONING},
		{darwinRouteLLInfo, syscall.RTF_LLINFO}, {darwinRouteStatic, syscall.RTF_STATIC},
		{darwinRouteBlackhole, syscall.RTF_BLACKHOLE}, {darwinRouteProtoCloning, syscall.RTF_PRCLONING},
		{darwinRouteWasCloned, syscall.RTF_WASCLONED}, {darwinRouteIfScope, syscall.RTF_IFSCOPE},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("Darwin ABI mismatch: %x != %x", pair[0], pair[1])
		}
	}
}

func TestDarwinRIBHostAndNetworkRoutes(t *testing.T) {
	iface := net.Interface{Index: 20, Name: "utun4"}
	for _, tc := range []struct {
		cidr      string
		dst, mask route.Addr
		host      bool
	}{
		{"8.8.8.8/32", &route.Inet4Addr{IP: [4]byte{8, 8, 8, 8}}, nil, true},
		{"2001:4860:4860::8888/128", &route.Inet6Addr{IP: netip.MustParseAddr("2001:4860:4860::8888").As16()}, nil, true},
		{"0.0.0.0/1", &route.Inet4Addr{}, &route.Inet4Addr{IP: [4]byte{128}}, false},
		{"::/1", &route.Inet6Addr{}, &route.Inet6Addr{IP: [16]byte{128}}, false},
	} {
		t.Run(tc.cidr, func(t *testing.T) {
			flags := syscall.RTF_UP | syscall.RTF_STATIC
			// A host route has no mask sockaddr at all, not an all-ones mask.
			addrs := []route.Addr{tc.dst, &route.LinkAddr{Index: 20}}
			if tc.host {
				flags |= syscall.RTF_HOST
				if netip.MustParsePrefix(tc.cidr).Addr().Is6() {
					// nd6_rtrequest adds LLINFO to manual IPv6 interface host
					// routes too, including on point-to-point interfaces.
					flags |= syscall.RTF_LLINFO
				}
			} else {
				addrs = append(addrs, tc.mask)
			}
			msg := &route.RouteMessage{Index: 20, Flags: flags, Addrs: addrs}
			r := ownedRoute{prefix: netip.MustParsePrefix(tc.cidr), iface: "utun4", index: 20}
			if !matchesDarwinRoute(r, iface, []route.Message{msg}) {
				t.Fatal("valid route not found")
			}
			for _, flag := range []int{syscall.RTF_WASCLONED, syscall.RTF_IFSCOPE, syscall.RTF_REJECT, syscall.RTF_BLACKHOLE} {
				msg.Flags = flags | flag
				if matchesDarwinRoute(r, iface, []route.Message{msg}) {
					t.Fatalf("unsafe route accepted: %x", flag)
				}
			}
			msg.Flags = flags
			msg.Index = 11
			if matchesDarwinRoute(r, iface, []route.Message{msg}) {
				t.Fatal("wrong interface accepted")
			}
			msg.Index = 20
			r.index = 21
			if matchesDarwinRoute(r, iface, []route.Message{msg}) {
				t.Fatal("reused interface name accepted")
			}
		})
	}
}

func TestDarwinRIBBypassGateway(t *testing.T) {
	iface := net.Interface{Index: 11, Name: "en0"}
	r := ownedRoute{prefix: netip.MustParsePrefix("2001:db8::7/128"), iface: "en0", gateway: "fe80::1%en0", index: 11}
	gw := &route.Inet6Addr{IP: netip.MustParseAddr("fe80::1").As16(), ZoneID: 11}
	msg := &route.RouteMessage{Index: 11, Flags: syscall.RTF_UP | syscall.RTF_STATIC | syscall.RTF_HOST | syscall.RTF_GATEWAY, Addrs: []route.Addr{&route.Inet6Addr{IP: netip.MustParseAddr("2001:db8::7").As16()}, gw}}
	if !matchesDarwinRoute(r, iface, []route.Message{msg}) {
		t.Fatal("IPv6 bypass gateway not found")
	}
	gw.ZoneID = 12
	if matchesDarwinRoute(r, iface, []route.Message{msg}) {
		t.Fatal("wrong gateway zone accepted")
	}
	gw.ZoneID = 11
	gw.IP = netip.MustParseAddr("fe80::2").As16()
	if matchesDarwinRoute(r, iface, []route.Message{msg}) {
		t.Fatal("wrong gateway accepted")
	}
	r.gateway = ""
	if matchesDarwinRoute(r, iface, []route.Message{msg}) {
		t.Fatal("gateway route confused with on-link route")
	}
}
