package goncrunner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIPv6ProbeKeepsInitialResultForCurrentConnection(t *testing.T) {
	for _, available := range []bool{false, true} {
		h := newRouteHarness(t)
		h.a.probe = func(context.Context, string) bool { h.probes++; return available }
		h.a.applied = func(_ vpnTunnelSnapshot, got bool, _ []string) {
			h.applied++
			if got != available {
				t.Fatal("initial result not retained")
			}
			go func() {
				// Wake the lifecycle without changing the connection generation.
				for i := 0; i < 5; i++ {
					select {
					case h.v.wake <- struct{}{}:
					default:
					}
					time.Sleep(5 * time.Millisecond)
				}
				h.cancel()
			}()
		}
		tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
		h.run(t)
		if h.probes != 1 || h.starts != 1 || h.applied != 1 {
			t.Fatalf("unexpected repeat: probes=%d starts=%d applied=%d", h.probes, h.starts, h.applied)
		}
	}
	// Guard against reintroducing an idle periodic probe that a short runtime
	// test cannot observe without waiting the entire production interval.
	source, err := os.ReadFile("vpn_lifecycle.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, timer := range []string{"time.NewTimer", "time.NewTicker", "recheckInterval"} {
		if strings.Contains(string(source), timer) {
			t.Fatalf("periodic probe returned: %s", timer)
		}
	}
}

func TestIPv6ProbeRunsAgainOnTunnelReconnect(t *testing.T) {
	h := newRouteHarness(t)
	h.a.probe = func(context.Context, string) bool { h.probes++; return h.probes == 2 }
	h.a.applied = func(_ vpnTunnelSnapshot, available bool, _ []string) {
		h.applied++
		if available != (h.applied == 2) {
			t.Fatal("reconnection reused old IPv6 result")
		}
		if h.applied == 1 {
			tunnelReport(h.v, "a", "disconnected")
		} else {
			h.cancel()
		}
	}
	h.a.paused = func(string, error) { tunnelReady(h.v, "b", "socks5://127.0.0.1:2000") }
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.run(t)
	if h.probes != 2 || h.starts != 2 || h.applied != 2 {
		t.Fatalf("missing reconnect probe: %+v", h)
	}
}
