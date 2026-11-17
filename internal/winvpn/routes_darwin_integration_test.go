//go:build darwin

package winvpn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/route"
	"golang.zx2c4.com/wireguard/tun"
)

// Only mutates the explicitly authorized LAN peer's host route. Refuses any
// preexisting owned-style host route, off-link peer, or the local machine.
// No default/capture routes, system DNS or existing utun are changed.
func TestDarwinLANBypassLifecycle(t *testing.T) {
	peerText, name := os.Getenv("GONC_TEST_LAN_PEER"), os.Getenv("GONC_TEST_LAN_IFACE")
	if peerText == "" || name == "" {
		t.Skip("explicit LAN peer/interface required")
	}
	peer, err := netip.ParseAddr(peerText)
	if err != nil || !peer.Is4() || !peer.IsPrivate() || os.Geteuid() != 0 {
		t.Fatal("requires root and private IPv4 peer")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil || len(iface.HardwareAddr) == 0 {
		t.Fatal("physical interface required", err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		t.Fatal(err)
	}
	onLink := false
	for _, a := range addrs {
		p, e := netip.ParsePrefix(a.String())
		if e == nil {
			if p.Addr() == peer {
				t.Fatal("refusing local address")
			}
			if p.Contains(peer) {
				onLink = true
			}
		}
	}
	if !onLink {
		t.Fatal("refusing off-link peer")
	}
	native, err := nativeRouteTo(peerText)
	if err != nil || native.iface != name || native.gateway != "" {
		t.Fatal("unexpected native exit", native, err)
	}
	r := ownedRoute{prefix: netip.PrefixFrom(peer, 32), iface: name, index: iface.Index}
	if exists, err := routePresent(r); err != nil || exists {
		t.Fatal("refusing existing static host route", exists, err)
	}
	ping := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ping", "-n", "-c", "2", "-W", "1000", peerText).CombinedOutput()
		t.Log(string(out))
		if err != nil {
			t.Fatal("peer inaccessible", err)
		}
	}
	ping()
	s := &Session{trace: func(message string) { t.Log(message) }}
	t.Cleanup(func() {
		if s.routeLedger != nil {
			if err := s.routeLedger.cleanup(); err != nil {
				t.Error(err)
			}
		}
	})
	// Global VPN routes lose to the existing LAN /24: no host route or ledger.
	if err := s.ensureDarwinBypass(r, []netip.Prefix{netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1")}); err != nil {
		t.Fatal(err)
	}
	if s.routeLedger != nil {
		t.Fatal("native subnet was not reused")
	}
	// Simulate a conflicting capture rule in planning only; do not install it.
	if err := s.ensureDarwinBypass(r, []netip.Prefix{r.prefix}); err != nil {
		t.Fatal(err)
	}
	if s.routeLedger == nil || len(s.routeLedger.owned) != 1 {
		t.Fatal("new host route not tracked")
	}
	// Do not let a working preexisting ARP cache mask an invalid RTM_ADD.
	buf, err := route.FetchRIB(syscall.AF_INET, route.RIBTypeRoute, 0)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, buf)
	if err != nil {
		t.Fatal(err)
	}
	arpReady := false
	for _, message := range messages {
		if m, ok := message.(*route.RouteMessage); ok && matchesDarwinRoute(r, *iface, []route.Message{m}) && m.Flags&syscall.RTF_LLINFO != 0 {
			arpReady = true
		}
	}
	if !arpReady {
		t.Fatal("static host route is not registered with ARP")
	}
	if err := verifyDarwinBypass([]ownedRoute{r}, "utun-unused", nativeRouteTo, s.trace); err != nil {
		t.Fatal(err)
	}
	ping()
	if ready, err := routeInstalled(r); err != nil || !ready {
		t.Fatal("resolved peer route invalid", ready, err)
	}
	t.Log(diagnoseDarwinRoute(r))
	// Force a post-add readiness failure, proving rollback uses existence,
	// not readiness. No deliberately broken MAC or REJECT route is installed.
	s.routeLedger.ready = func(ownedRoute) (bool, error) { return false, nil }
	if err := s.routeLedger.cleanup(); err != nil {
		t.Fatal(err)
	}
	if exists, err := routePresent(r); err != nil || exists {
		t.Fatal("static host route leaked", exists, err)
	}
	ping()
	// A second creation/cleanup catches stale permanent ARP and reconnect bugs.
	s.routeLedger.ready = routeInstalled
	if err := s.addOwnedRoute(r); err != nil {
		t.Fatal(err)
	}
	ping()
	if err := s.routeLedger.cleanup(); err != nil {
		t.Fatal(err)
	}
	ping()
}

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
	r := ownedRoute{prefix: netip.PrefixFrom(target, 128), iface: name}
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
