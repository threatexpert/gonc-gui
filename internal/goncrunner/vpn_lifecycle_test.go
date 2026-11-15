package goncrunner

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/threatexpert/gonc/v2/goncembed"
)

func tunnelReport(v *vpnTunnelLifecycle, topic, status string) bool {
	return v.report(P2PStatusReport{Topic: topic, Status: status, Peer: "192.0.2.1:8000", Timestamp: 100})
}

func tunnelReady(v *vpnTunnelLifecycle, topic, endpoint string) {
	tunnelReport(v, topic, "connected")
	v.ready(endpoint)
}

type routeHarness struct {
	v                                      *vpnTunnelLifecycle
	ctx                                    context.Context
	cancel                                 context.CancelFunc
	a                                      vpnRouteActions
	starts, stops, probes, applied, paused int
	endpoints                              []string
}

func newRouteHarness(t *testing.T) *routeHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	h := &routeHarness{v: newVPNTunnelLifecycle(), ctx: ctx, cancel: cancel}
	h.a = vpnRouteActions{
		probe: func(context.Context, string) bool { h.probes++; return false },
		start: func(s vpnTunnelSnapshot, _ bool) ([]string, error) {
			h.starts++
			h.endpoints = append(h.endpoints, s.endpoint)
			return []string{"8.8.8.8"}, nil
		},
		stop:     func() error { h.stops++; return nil },
		applying: func(vpnTunnelSnapshot, bool) {},
		applied:  func(vpnTunnelSnapshot, bool, []string) { h.applied++; h.cancel() },
		paused:   func(string, error) { h.paused++ },
	}
	return h
}

func (h *routeHarness) run(t *testing.T) {
	t.Helper()
	if err := runVPNRouteLifecycle(h.ctx, make(chan struct{}), h.v, h.a); err != nil {
		t.Fatal(err)
	}
	if errors.Is(h.ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("state machine stalled")
	}
}

func TestVPNReconnectDoesNotReplayBufferedConnecting(t *testing.T) {
	h := newRouteHarness(t)
	// The old code could select Ready first, apply VPN, then consume connecting
	// from the separate loss queue and stop the recovered VPN permanently.
	tunnelReady(h.v, "old", "socks5://127.0.0.1:1000")
	tunnelReport(h.v, "old", "disconnected")
	tunnelReport(h.v, "new", "connecting")
	tunnelReport(h.v, "new", "negotiating")
	tunnelReady(h.v, "new", "socks5://127.0.0.1:2000")
	h.run(t)
	if h.starts != 1 || h.stops != 0 || h.applied != 1 || h.endpoints[0] != "socks5://127.0.0.1:2000" {
		t.Fatalf("starts=%d stops=%d applied=%d endpoints=%v", h.starts, h.stops, h.applied, h.endpoints)
	}
}

func TestVPNTunnelRejectsOldTopicsAndRegressiveStagesButAcceptsRealLoss(t *testing.T) {
	v := newVPNTunnelLifecycle()
	tunnelReady(v, "old", "socks5://127.0.0.1:1000")
	tunnelReport(v, "old", "disconnected")
	tunnelReady(v, "new", "socks5://127.0.0.1:2000")
	before := v.snapshot()
	for _, report := range []P2PStatusReport{
		{Topic: "old", Status: "disconnected", Timestamp: 100},
		{Topic: "old", Status: "connected", Timestamp: 100},
		{Topic: "new", Status: "connecting", Timestamp: 100},
		{Topic: "new", Status: "negotiating", Timestamp: 100},
		{Topic: "losing-LAN-attempt", Status: "error:timeout", Timestamp: 100},
	} {
		if v.report(report) {
			t.Fatalf("accepted stale report %+v", report)
		}
	}
	if !v.current(before) {
		t.Fatal("stale reports changed current desired state")
	}
	if !tunnelReport(v, "new", "disconnected") || v.snapshot().ready {
		t.Fatal("real loss ignored")
	}
	if v.ready("socks5://127.0.0.1:2000") {
		t.Fatal("late Ready revived a disconnected connection")
	}
}

func TestVPNRealDisconnectPausesAndNextReadyRestoresDNS(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	details := newVPNDetailsTracker(Request{}, nil)
	h.a.applying = func(s vpnTunnelSnapshot, ipv6 bool) { details.routes("applying", nil, nil, []string{}) }
	h.a.applied = func(s vpnTunnelSnapshot, ipv6 bool, dns []string) {
		h.applied++
		details.routes("applied", nil, nil, dns)
		if h.applied == 1 {
			tunnelReport(h.v, "a", "disconnected")
		} else {
			h.cancel()
		}
	}
	h.a.paused = func(status string, err error) {
		h.paused++
		if err != nil {
			t.Fatal(err)
		}
		details.routes("paused", nil, nil, nil)
		tunnelReady(h.v, "b", "socks5://127.0.0.1:2000")
	}
	h.run(t)
	if h.starts != 2 || h.stops != 1 || h.paused != 1 || h.applied != 2 {
		t.Fatalf("counts %+v", h)
	}
	got := details.snapshot(time.Now())
	if got.RouteState != "applied" || !reflect.DeepEqual(got.DNSServers, []string{"8.8.8.8"}) {
		t.Fatal(got)
	}
}

func TestVPNNewReadyDuringIPv6ProbeSkipsOldConfiguration(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.a.probe = func(context.Context, string) bool {
		h.probes++
		if h.probes == 1 {
			tunnelReport(h.v, "a", "disconnected")
			tunnelReady(h.v, "b", "socks5://127.0.0.1:2000")
		}
		return false
	}
	h.run(t)
	if h.probes != 2 || h.starts != 1 || h.stops != 0 || h.endpoints[0] != "socks5://127.0.0.1:2000" {
		t.Fatalf("counts %+v", h)
	}
}

func TestVPNLossDuringHelperStartStillPauses(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.a.start = func(vpnTunnelSnapshot, bool) ([]string, error) {
		h.starts++
		tunnelReport(h.v, "a", "disconnected")
		return []string{"8.8.8.8"}, nil
	}
	h.a.paused = func(string, error) { h.paused++; h.cancel() }
	h.run(t)
	if h.starts != 1 || h.stops != 1 || h.applied != 0 || h.paused != 1 {
		t.Fatalf("counts %+v", h)
	}
}

func TestVPNReadyDuringHelperStopIsNotLost(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.a.applied = func(vpnTunnelSnapshot, bool, []string) {
		h.applied++
		if h.applied == 1 {
			tunnelReport(h.v, "a", "disconnected")
		} else {
			h.cancel()
		}
	}
	h.a.stop = func() error {
		h.stops++
		tunnelReady(h.v, "b", "socks5://127.0.0.1:2000")
		return nil
	}
	h.run(t)
	if h.starts != 2 || h.stops != 1 || h.applied != 2 || h.paused != 0 {
		t.Fatalf("counts %+v", h)
	}
}

func TestVPNNewReadyDuringHelperStartReplacesObsoleteResult(t *testing.T) {
	for _, failOld := range []bool{false, true} {
		h := newRouteHarness(t)
		tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
		h.a.start = func(s vpnTunnelSnapshot, _ bool) ([]string, error) {
			h.starts++
			h.endpoints = append(h.endpoints, s.endpoint)
			if h.starts == 1 {
				tunnelReport(h.v, "a", "disconnected")
				tunnelReady(h.v, "b", "socks5://127.0.0.1:2000")
				if failOld {
					return nil, errors.New("old tunnel closed")
				}
				return []string{"8.8.8.8"}, nil
			}
			return []string{"9.9.9.9"}, nil
		}
		h.a.applied = func(s vpnTunnelSnapshot, _ bool, dns []string) {
			h.applied++
			if s.endpoint != "socks5://127.0.0.1:2000" || !reflect.DeepEqual(dns, []string{"9.9.9.9"}) {
				t.Fatal("published obsolete result")
			}
			h.cancel()
		}
		h.run(t)
		wantStops := 1
		if failOld {
			wantStops = 0
		}
		if h.starts != 2 || h.applied != 1 || h.stops != wantStops {
			t.Fatalf("counts %+v", h)
		}
	}
}

func TestVPNManualStopDuringProbeNeverStartsHelper(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.a.probe = func(context.Context, string) bool { h.cancel(); return false }
	h.run(t)
	if h.starts != 0 {
		t.Fatal("started after manual cancellation")
	}
}

func TestVPNMailboxRetainsLatestReadyWhenWakeupIsAlreadyPending(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	for i := 0; i < 100; i++ {
		h.v.ready("socks5://127.0.0.1:2000")
	}
	h.run(t)
	if h.starts != 1 || h.endpoints[0] != "socks5://127.0.0.1:2000" {
		t.Fatalf("dropped latest endpoint: %+v", h)
	}
}

func TestVPNBypassRefreshDoesNotRepeatIPv6Probe(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.a.applied = func(s vpnTunnelSnapshot, _ bool, _ []string) {
		h.applied++
		if h.applied == 1 {
			h.v.report(P2PStatusReport{Topic: "a", Status: "connected", Peer: "192.0.2.2:9000", Timestamp: 100})
		} else {
			if !reflect.DeepEqual(s.bypass, []string{"192.0.2.2"}) {
				t.Fatal(s.bypass)
			}
			h.cancel()
		}
	}
	h.run(t)
	if h.starts != 2 || h.probes != 1 || h.stops != 1 {
		t.Fatalf("counts %+v", h)
	}
}

func TestVPNCallbackDoesNotPublishObsoleteConnectionState(t *testing.T) {
	v := newVPNTunnelLifecycle()
	d := newVPNDetailsTracker(Request{}, nil)
	var reports []P2PStatusReport
	c := &callback{mode: ModeVPNClient, vpnDetails: d, onP2PReport: v.report, onReady: v.ready, reportSink: func(r P2PStatusReport) { reports = append(reports, r) }}
	c.P2PReport("a", "", "connected", "", "", "", 100, 0)
	c.Ready("socks5://127.0.0.1:1000")
	c.P2PReport("a", "", "connecting", "", "", "", 100, 0)
	if len(reports) != 1 || !d.snapshot(time.Now()).Connected {
		t.Fatal("stale status reached UI/details")
	}
	c.P2PReport("a", "", "disconnected", "", "", "", 100, 0)
	if len(reports) != 2 || d.snapshot(time.Now()).Connected {
		t.Fatal("genuine loss not published")
	}
}

func TestVPNTunnelLifecycleConcurrentCallbacks(t *testing.T) {
	v := newVPNTunnelLifecycle()
	tunnelReady(v, "current", "socks5://127.0.0.1:1000")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tunnelReport(v, "current", "connecting")
				v.ready("socks5://127.0.0.1:1000")
				v.current(v.snapshot())
			}
		}()
	}
	wg.Wait()
	if !v.snapshot().ready {
		t.Fatal("stale connecting paused current tunnel")
	}
}

func TestVPNClockAdjustmentDoesNotHideGenuineDisconnect(t *testing.T) {
	v := newVPNTunnelLifecycle()
	tunnelReady(v, "a", "socks5://127.0.0.1:1000")
	if !v.report(P2PStatusReport{Topic: "a", Status: "disconnected", Timestamp: 90}) || v.snapshot().ready {
		t.Fatal("wall clock rollback suppressed a real disconnect")
	}
}

func TestVPNStopWaitCancelsAndJoinsRouteWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { <-ctx.Done(); close(done) }()
	r := &Runner{session: &goncembed.Session{}, stopVPNRoutes: func() { cancel(); <-done }}
	if _, err := r.StopWait(0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("route worker survived manual stop")
	}
	if r.stopVPNRoutes != nil {
		t.Fatal("retained stopped route worker")
	}
}

func TestVPNSessionExitDuringProbeNeverStartsHelper(t *testing.T) {
	h := newRouteHarness(t)
	done := make(chan struct{})
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	h.a.probe = func(context.Context, string) bool { close(done); return false }
	if err := runVPNRouteLifecycle(h.ctx, done, h.v, h.a); err != nil {
		t.Fatal(err)
	}
	if h.starts != 0 {
		t.Fatal("started after session exit")
	}
}

func TestVPNCurrentHelperFailureIsNotSuppressed(t *testing.T) {
	h := newRouteHarness(t)
	tunnelReady(h.v, "a", "socks5://127.0.0.1:1000")
	want := errors.New("helper failed")
	h.a.start = func(vpnTunnelSnapshot, bool) ([]string, error) { return nil, want }
	if err := runVPNRouteLifecycle(h.ctx, make(chan struct{}), h.v, h.a); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if h.applied != 0 {
		t.Fatal("failed configuration reported success")
	}
}
