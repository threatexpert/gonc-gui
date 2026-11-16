package goncrunner

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestRejectIPv6PolicyFollowsRoutesAndProbe(t *testing.T) {
	for _, routes := range [][]string{{"0.0.0.0/0"}, {"0.0.0.0/0", "::/0"}, {"2001:db8::/32"}} {
		for _, available := range []bool{false, true} {
			cfg := systemVPNConfig(Request{DNSServers: "2001:4860:4860::8888"}, vpnTunnelSnapshot{endpoint: "socks5://127.0.0.1:1000"}, routes, available)
			wantV6 := routes[len(routes)-1] != "0.0.0.0/0"
			if cfg.RejectIPv6 != (wantV6 && !available) || cfg.EnableIPv6 != wantV6 {
				t.Fatalf("wrong policy: %+v", cfg)
			}
			if !reflect.DeepEqual(cfg.Routes, routes) {
				t.Fatal("probe filtered routes")
			}
			if !available && !reflect.DeepEqual(cfg.DNSServers, []string{"8.8.8.8"}) {
				t.Fatal("DNS fallback missing")
			}
		}
	}
}

func TestRejectIPv6UpdatedAfterEachReconnect(t *testing.T) {
	h := newRouteHarness(t)
	results := []bool{false, true, false}
	routes := []string{"0.0.0.0/0", "::/0"}
	h.a.probe = func(context.Context, string) bool { result := results[h.probes]; h.probes++; return result }
	h.a.start = func(s vpnTunnelSnapshot, available bool) ([]string, error) {
		cfg := systemVPNConfig(Request{}, s, routes, available)
		if cfg.RejectIPv6 != !results[h.starts] || !cfg.EnableIPv6 || !reflect.DeepEqual(cfg.Routes, routes) {
			t.Fatalf("stale policy: %+v", cfg)
		}
		h.starts++
		return cfg.DNSServers, nil
	}
	h.a.applied = func(_ vpnTunnelSnapshot, _ bool, _ []string) {
		h.applied++
		if h.applied == len(results) {
			h.cancel()
			return
		}
		tunnelReport(h.v, fmt.Sprint(h.applied), "disconnected")
	}
	h.a.paused = func(string, error) { tunnelReady(h.v, fmt.Sprint(h.applied+1), "socks5://127.0.0.1:1000") }
	tunnelReady(h.v, "1", "socks5://127.0.0.1:1000")
	h.run(t)
	if h.starts != 3 || h.probes != 3 || h.applied != 3 {
		t.Fatalf("missing reconnect transition: %+v", h)
	}
}
