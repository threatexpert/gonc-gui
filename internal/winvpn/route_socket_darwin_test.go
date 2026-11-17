//go:build darwin

package winvpn

import (
	"net"
	"net/netip"
	"syscall"
	"testing"

	"golang.org/x/net/route"
)

func TestDarwinLinkRouteRequest(t *testing.T) {
	iface := net.Interface{Name: "en0", Index: 11, HardwareAddr: net.HardwareAddr{1, 2, 3, 4, 5, 6}}
	for _, cidr := range []string{"172.16.88.141/32", "2001:db8::7/128", "fe80::7/128"} {
		for _, op := range []string{"add", "delete"} {
			m, err := darwinLinkRouteRequest(op, ownedRoute{prefix: netip.MustParsePrefix(cidr)}, iface)
			if err != nil {
				t.Fatal(err)
			}
			if m.Flags&syscall.RTF_GATEWAY != 0 || m.Flags&syscall.RTF_IFSCOPE != 0 {
				t.Fatal("unexpected gateway/scoped flags")
			}
			raw := m.Addrs[syscall.RTAX_GATEWAY].(*route.DefaultAddr).Raw
			if len(raw) < syscall.SizeofSockaddrDatalink || raw[6] != 0 {
				t.Fatal("invalid unresolved sockaddr_dl", raw)
			}
			b, err := m.Marshal()
			if err != nil || len(b) == 0 {
				t.Fatal("marshal", err)
			}
			messages, err := route.ParseRIB(route.RIBTypeRoute, b)
			if err != nil {
				t.Fatal(err)
			}
			decoded := messages[0].(*route.RouteMessage)
			link := decoded.Addrs[syscall.RTAX_GATEWAY].(*route.LinkAddr)
			if link.Index != iface.Index || len(link.Addr) != 0 {
				t.Fatal("gateway contains local MAC", link)
			}
			if result, ok := darwinRouteReply(m, messages); !ok || result.state != routeMutationSucceeded {
				t.Fatal("request roundtrip", result, ok)
			}
		}
	}
	if _, err := darwinLinkRouteRequest("add", ownedRoute{prefix: netip.MustParsePrefix("172.16.88.0/24")}, iface); err == nil {
		t.Fatal("network route accepted")
	}
}

func TestDarwinRouteReply(t *testing.T) {
	r := &route.RouteMessage{ID: 1, Seq: 42, Type: syscall.RTM_ADD}
	for _, m := range []*route.RouteMessage{{ID: 2, Seq: 42, Type: syscall.RTM_ADD}, {ID: 1, Seq: 41, Type: syscall.RTM_ADD}, {ID: 1, Seq: 42, Type: syscall.RTM_DELETE}} {
		if _, ok := darwinRouteReply(r, []route.Message{m}); ok {
			t.Fatal("unrelated reply accepted")
		}
	}
	m := *r
	m.Err = syscall.EEXIST
	if result, ok := darwinRouteReply(r, []route.Message{&m}); !ok || result.state != routeMutationFailed {
		t.Fatal("EEXIST claimed as owned")
	}
}

func TestDarwinPhysicalBypassReadinessAndOwnership(t *testing.T) {
	iface := net.Interface{Name: "en0", Index: 11, HardwareAddr: net.HardwareAddr{1, 2, 3, 4, 5, 6}}
	r := ownedRoute{prefix: netip.MustParsePrefix("172.16.88.141/32"), iface: "en0", index: 11}
	flags := syscall.RTF_UP | syscall.RTF_HOST | syscall.RTF_STATIC | syscall.RTF_LLINFO
	m := &route.RouteMessage{Index: 11, Flags: flags, Addrs: []route.Addr{&route.Inet4Addr{IP: r.prefix.Addr().As4()}, &route.LinkAddr{Index: 11}}}
	for _, tc := range []struct {
		name           string
		mac            []byte
		extra          int
		ready, present bool
	}{
		{"unresolved", nil, 0, true, true},
		{"resolved", []byte{6, 5, 4, 3, 2, 1}, 0, true, true},
		{"own MAC", iface.HardwareAddr, 0, false, true},
		{"reject", nil, syscall.RTF_REJECT, false, true},
		{"blackhole", nil, syscall.RTF_BLACKHOLE, false, true},
		{"scoped", nil, syscall.RTF_IFSCOPE, false, false},
		{"clone", nil, syscall.RTF_WASCLONED, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.Flags = flags | tc.extra
			m.Addrs[syscall.RTAX_GATEWAY] = &route.LinkAddr{Index: 11, Addr: tc.mac}
			if got := matchesDarwinRoute(r, iface, []route.Message{m}); got != tc.ready {
				t.Fatalf("ready %t", got)
			}
			if got := matchesDarwinRouteState(r, iface, []route.Message{m}, false); got != tc.present {
				t.Fatalf("present %t", got)
			}
		})
	}
	for _, missing := range []int{syscall.RTF_UP, syscall.RTF_LLINFO} {
		m.Flags = flags &^ missing
		m.Addrs[syscall.RTAX_GATEWAY] = &route.LinkAddr{Index: 11}
		if matchesDarwinRoute(r, iface, []route.Message{m}) || !matchesDarwinRouteState(r, iface, []route.Message{m}, false) {
			t.Fatal("unusable owned route must remain eligible for rollback", missing)
		}
	}
}

func TestDarwinReuseParentNotARPClone(t *testing.T) {
	iface := net.Interface{Name: "en0", Index: 11}
	r := ownedRoute{prefix: netip.MustParsePrefix("172.16.88.141/32"), iface: "en0"}
	clone := &route.RouteMessage{Index: 11, Flags: syscall.RTF_UP | syscall.RTF_HOST | syscall.RTF_LLINFO | syscall.RTF_WASCLONED | syscall.RTF_IFSCOPE, Addrs: []route.Addr{&route.Inet4Addr{IP: r.prefix.Addr().As4()}, &route.LinkAddr{Index: 11}}}
	parent := &route.RouteMessage{Index: 11, Flags: syscall.RTF_UP | syscall.RTF_STATIC | syscall.RTF_CLONING, Addrs: []route.Addr{&route.Inet4Addr{IP: [4]byte{172, 16, 88, 0}}, &route.LinkAddr{Index: 11}, &route.Inet4Addr{IP: [4]byte{255, 255, 255, 0}}}}
	capture := []netip.Prefix{netip.MustParsePrefix("128.0.0.0/1")}
	if p := reusableDarwinBypass(r, iface, []route.Message{clone}, capture); p.IsValid() {
		t.Fatal("reused ephemeral ARP host")
	}
	if p := reusableDarwinBypass(r, iface, []route.Message{clone, parent}, capture); p.String() != "172.16.88.0/24" {
		t.Fatal(p)
	}
	parent.Index = 12
	if p := reusableDarwinBypass(r, iface, []route.Message{parent}, capture); p.IsValid() {
		t.Fatal("wrong interface reused")
	}
}
