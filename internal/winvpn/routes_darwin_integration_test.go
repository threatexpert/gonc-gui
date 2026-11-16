//go:build darwin

package winvpn

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/netip"
	"os"
	"syscall"
	"testing"

	"golang.org/x/net/route"
	"golang.zx2c4.com/wireguard/tun"
)

// Explicitly opt-in: creates a NEW kernel-assigned utun and a single random
// documentation-prefix host route. Never changes global routes or system DNS,
// and never opens/reconfigures an existing VPN interface or tun2socks engine.
func TestDarwinIsolatedIPv6HostRoute(t *testing.T) {
	if os.Getenv("GONC_TEST_ISOLATED_UTUN") != "1" {
		t.Skip("set GONC_TEST_ISOLATED_UTUN=1 to opt in")
	}
	if os.Geteuid() != 0 {
		t.Fatal("isolated utun test requires root")
	}
	before, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WG_TUN_NAME_FILE", "")
	dev, err := tun.CreateTUN("utun", 1400)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dev.Close(); err != nil {
			t.Error(err)
		}
	})
	go func() {
		for range dev.Events() {
		}
	}()
	name, err := dev.Name()
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range before {
		if old.Name == name {
			t.Fatalf("refusing preexisting interface %s", name)
		}
	}
	t.Logf("created isolated interface %s", name)
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	h := hex.EncodeToString(nonce[:])
	target := netip.MustParseAddr("2001:db8:" + h[:4] + ":" + h[4:8] + ":" + h[8:] + "::53")
	source := netip.MustParseAddr("fd00:" + h[:4] + ":" + h[4:8] + ":" + h[8:] + "::2")
	if err := run("ifconfig", name, "inet6", source.String(), "prefixlen", "128", "up"); err != nil {
		t.Fatal(err)
	}
	s := &Session{ifaceName: name, trace: func(message string) { t.Log(message) }}
	t.Cleanup(func() {
		if s.routeLedger != nil {
			if err := s.routeLedger.cleanup(); err != nil {
				t.Error(err)
			}
		}
	})
	r := ownedRoute{cidr: netip.PrefixFrom(target, 128).String(), iface: name}
	if err := s.addOwnedRoute(r); err != nil {
		t.Fatal(err)
	}
	if s.routeLedger == nil || len(s.routeLedger.owned) != 1 {
		t.Fatal("new test route not owned")
	}
	buf, err := route.FetchRIB(syscall.AF_INET6, route.RIBTypeRoute, 0)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, buf)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range msgs {
		m, ok := msg.(*route.RouteMessage)
		if !ok || len(m.Addrs) <= syscall.RTAX_DST {
			continue
		}
		addr, ok := netip.AddrFromSlice(ribIP(m.Addrs[syscall.RTAX_DST]))
		if !ok || addr != target {
			continue
		}
		found = true
		t.Logf("actual RIB target=%s index=%d flags=0x%X STATIC=%t HOST=%t LLINFO=%t WASCLONED=%t IFSCOPE=%t", target, m.Index, m.Flags, m.Flags&syscall.RTF_STATIC != 0, m.Flags&syscall.RTF_HOST != 0, m.Flags&syscall.RTF_LLINFO != 0, m.Flags&syscall.RTF_WASCLONED != 0, m.Flags&syscall.RTF_IFSCOPE != 0)
	}
	if !found {
		t.Fatal("test route missing in RIB")
	}
	actual, err := nativeRouteTo(target.String())
	if err != nil || actual.iface != name {
		t.Fatalf("test target exit=%+v error=%v", actual, err)
	}
	t.Logf("actual target exit=%s", actual.iface)
	if err := s.routeLedger.cleanup(); err != nil {
		t.Fatal(err)
	}
	if present, err := routeInstalled(r); err != nil || present {
		t.Fatalf("route left after cleanup: present=%t err=%v", present, err)
	}
	actual, err = nativeRouteTo(target.String())
	if err != nil {
		t.Logf("after cleanup: no usable native IPv6 route: %v", err)
	} else if actual.iface == name {
		t.Fatal("target still routed to test interface after cleanup")
	}
	t.Logf("test route removed; restored target exit=%s", actual.iface)
}
