package goncrunner

import (
	"context"
	"errors"
	"github.com/threatexpert/gonc/v2/goncembed"
	"testing"
	"time"
)

func TestProtectedReconnectRetainsCaptureAndUpdatesOnlyChangedEndpoint(t *testing.T) {
	h := newRouteHarness(t)
	h.a.protect = true
	updates := 0
	h.a.update = func(endpoint string) error {
		updates++
		if endpoint != "socks5://127.0.0.1:2000" {
			t.Fatal(endpoint)
		}
		return nil
	}
	h.a.applied = func(s vpnTunnelSnapshot, _ bool, dns []string) {
		h.applied++
		if len(dns) != 1 || dns[0] != "8.8.8.8" {
			t.Fatal(dns)
		}
		if h.applied < 3 {
			tunnelReport(h.v, "topic"+string(rune('0'+h.applied)), "disconnected")
		} else {
			h.cancel()
		}
	}
	h.a.paused = func(string, error) {
		h.paused++
		endpoint := "socks5://127.0.0.1:1000"
		if h.paused == 2 {
			endpoint = "socks5://127.0.0.1:2000"
		}
		tunnelReady(h.v, "topic"+string(rune('1'+h.paused)), endpoint)
	}
	tunnelReady(h.v, "topic1", "socks5://127.0.0.1:1000")
	h.run(t)
	if h.starts != 1 || h.stops != 0 || h.probes != 1 || updates != 1 || h.applied != 3 || h.paused != 2 {
		t.Fatalf("%+v updates=%d", h, updates)
	}
}

func TestProtectedLossDuringStartRetainsCapture(t *testing.T) {
	h := newRouteHarness(t)
	h.a.protect = true
	start := h.a.start
	h.a.start = func(s vpnTunnelSnapshot, ipv6 bool) ([]string, error) {
		result, err := start(s, ipv6)
		tunnelReport(h.v, "a", "disconnected")
		return result, err
	}
	h.a.paused = func(string, error) { h.paused++; h.cancel() }
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.run(t)
	if h.starts != 1 || h.stops != 0 || h.paused != 1 || h.applied != 0 {
		t.Fatalf("%+v", h)
	}
}

func TestProtectedUpdateFailureNeverStopsCapture(t *testing.T) {
	h := newRouteHarness(t)
	h.a.protect = true
	want := errors.New("update failed")
	h.a.update = func(string) error { return want }
	h.a.applied = func(vpnTunnelSnapshot, bool, []string) { tunnelReady(h.v, "b", "socks5://127.0.0.1:2000") }
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	err := runVPNRouteLifecycle(h.ctx, make(chan struct{}), h.v, h.a)
	if !errors.Is(err, want) || h.stops != 0 || h.starts != 1 {
		t.Fatalf("err=%v %+v", err, h)
	}
}

func TestProtectionValidationAndScope(t *testing.T) {
	base := Request{Mode: ModeVPNClient, Password: "test", DisconnectProtection: true}
	for _, arg := range []string{"-local 1.2.3.4:0", "--local=1.2.3.4:0", "-dns 1.1.1.1", "--dns=1.1.1.1"} {
		req := base
		req.ExtraArgs = arg
		if err := validateRequest(req); err == nil {
			t.Fatal(arg)
		}
		req.BypassOverrideConfirmed = true
		if err := validateRequest(req); err != nil {
			t.Fatal(err)
		}
		req.DisconnectProtection = false
		if err := validateRequest(req); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"dns.example.com", "127.0.0.53", "[::1]:53", "8.8.8.8:0", "8.8.8.8:65536"} {
		if _, err := reconnectDNSServer(value); err == nil {
			t.Fatal(value)
		}
	}
	for value, want := range map[string]string{"": "8.8.8.8:53", "9.9.9.9:5353": "9.9.9.9:5353", "2001:4860:4860::8888": "[2001:4860:4860::8888]:53"} {
		if got, err := reconnectDNSServer(value); err != nil || got != want {
			t.Fatalf("%s %s %v", value, got, err)
		}
	}
	cfg := systemVPNConfig(base, vpnTunnelSnapshot{bypass: []string{"8.8.8.8"}}, []string{"0.0.0.0/1", "128.0.0.0/1"}, false)
	if len(cfg.BypassIPs) != 0 {
		t.Fatal("protection added global destination bypass routes")
	}
}

func TestProtectionDetailsRetainDNSAndCaptureUntilStop(t *testing.T) {
	d := newVPNDetailsTracker(Request{DisconnectProtection: true}, nil)
	d.connection("connected", time.Now())
	d.routes("applied", []string{"0.0.0.0/1"}, []string{}, []string{"8.8.8.8"})
	d.connection("disconnected", time.Now())
	state := d.snapshot(time.Now())
	if !state.CaptureActive || state.Ready || state.RouteState != "protected" || len(state.DNSServers) != 1 {
		t.Fatal(state)
	}
	d.connectionFailure("tunnel ended", true)
	if !d.snapshot(time.Now()).Running {
		t.Fatal("ended tunnel discarded protection")
	}
	d.beginStop()
	d.finish(time.Now())
	if d.snapshot(time.Now()).CaptureActive {
		t.Fatal("capture still shown after explicit stop")
	}
}

func TestEndedSessionStaysRunningUntilUserStopsProtection(t *testing.T) {
	p := newVPNProtection()
	p.captured.Store(true)
	session := &goncembed.Session{}
	d := newVPNDetailsTracker(Request{DisconnectProtection: true}, nil)
	d.connection("connected", time.Now())
	d.routes("applied", nil, nil, []string{"8.8.8.8"})
	r := &Runner{session: session, protection: p, vpnDetails: d}
	terminal := make(chan struct{})
	close(terminal)
	held := make(chan struct{}, 1)
	finished := make(chan struct{})
	sink := func(e Event) {
		if e.Level == "warn" {
			select {
			case held <- struct{}{}:
			default:
			}
		}
	}
	go func() {
		r.waitForSession(context.Background(), session, &callback{vpnDetails: d}, sink, terminal)
		close(finished)
	}()
	select {
	case <-held:
	case <-time.After(time.Second):
		t.Fatal("session did not enter protection hold")
	}
	if !r.IsRunning() || !d.snapshot(time.Now()).CaptureActive {
		t.Fatal("ended session released capture")
	}
	if _, err := r.StopWait(0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("explicit stop did not finish")
	}
	if r.IsRunning() || d.snapshot(time.Now()).CaptureActive {
		t.Fatal("explicit stop retained protection")
	}
}

func TestAdvancedBypassValuesOverrideAutomaticValues(t *testing.T) {
	p, err := PlanProtection(Request{ReconnectDNS: "8.8.8.8", ExtraArgs: `-dns 1.1.1.1 --dns=9.9.9.9:5353 --local "192.168.50.2:6000" -e ":nc -dns 4.4.4.4 host 80"`})
	if err != nil || p.DNS != "9.9.9.9:5353" || p.Local != "192.168.50.2:6000" || len(p.Overrides) != 2 {
		t.Fatalf("%+v %v", p, err)
	}
}
