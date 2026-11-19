//go:build darwin

package winvpn

import (
	"net"
	"strings"
	"testing"

	"gonc-gui/internal/vpnconfig"
)

func TestProtectedDarwinNetworkWithoutPeerRoutes(t *testing.T) {
	s := &Session{config: vpnconfig.Config{DisconnectProtection: true}, ifaceName: "utun-test"}
	captured, dnsConfigured := false, false
	err := applyDarwinNetwork(darwinNetworkSteps{
		bypass: s.configureBypassRoutes,
		capture: func() error {
			captured = true
			return nil
		},
		dns: func() error {
			dnsConfigured = true
			return nil
		},
		verifyBypass: s.verifyBypassRoutes,
	})
	if err != nil || !captured || !dnsConfigured {
		t.Fatalf("protected network configuration: capture=%v DNS=%v err=%v", captured, dnsConfigured, err)
	}
	if len(s.bypassRoutes) != 0 || s.routeLedger != nil {
		t.Fatal("protected mode must not install global peer bypass routes")
	}
}

func TestUnprotectedDarwinStillRequiresPeerRoutes(t *testing.T) {
	s := &Session{ifaceName: "utun-test"}
	err := s.configureBypassRoutes()
	if err == nil || !strings.Contains(err.Error(), "no tunnel peer IP") {
		t.Fatalf("unprotected mode must retain the peer route check: %v", err)
	}
}

func TestScopedRouteWithDeletedSourceIsNotReusable(t *testing.T) {
	d := darwinDefault{source: "192.168.50.220", gateway: "192.168.50.1", index: 11, scoped: true, usable: true}
	current := []net.IP{net.ParseIP("192.168.50.40")}
	if scopedSourceCurrent(d, current) {
		t.Fatal("old source accepted merely because en0/gateway did not change")
	}
	d.source = "192.168.50.40"
	if !scopedSourceCurrent(d, current) {
		t.Fatal("current source rejected")
	}
	d.source = ""
	if scopedSourceCurrent(d, current) {
		t.Fatal("missing route source accepted")
	}
}
