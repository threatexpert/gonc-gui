package goncrunner

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLinuxDetailsDuringRouteWithdrawal(t *testing.T) {
	v := newVPNDetailsTracker(Request{DisconnectProtection: true}, nil)
	now := time.Now()
	v.linuxOutput(true, true)
	v.connection("connected", now)
	v.routes("applied", []string{"0.0.0.0/1"}, []string{"192.0.2.1"}, []string{"8.8.8.8"})
	v.linuxOutput(false, false)
	v.connection("disconnected", now)
	v.linuxOutput(true, false)
	if d := v.snapshot(now); d.LinuxOutput != "sealed" || len(d.Routes) != 1 {
		t.Fatal("sealing alone must preserve installed routes", d)
	}
	v.linuxOutput(true, true)
	v.socksReady()
	if d := v.snapshot(now); len(d.Routes) != 0 || len(d.BypassIPs) != 0 || len(d.DNSServers) != 1 {
		t.Fatal("withdrawal should preserve only the active DNS settings", d)
	}
	v.finish(now)
	if d := v.snapshot(now); d.LinuxOutput != "" || len(d.DNSServers) != 0 {
		t.Fatal("stopped session still shows active protection or DNS", d)
	}
}

func TestVPNConfigurationFailureSurvivesTeardown(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	now := time.Now()
	v.connection("connected", now)
	v.routes("applying", nil, nil, nil)
	reason := "configure VPN DNS route for 2001:4860:4860::8888: route was not installed"
	v.configurationFailed(errors.New(reason))
	d := v.snapshot(now)
	if d.RouteState != "error" || d.Ready || d.ConfigurationError != reason {
		t.Fatal("configuration failure lost", d)
	}
	encoded, err := json.Marshal(d)
	if err != nil || !strings.Contains(string(encoded), `"configurationError":"`+reason+`"`) {
		t.Fatal("failure missing from frontend payload", string(encoded), err)
	}
	// Duplicate transport reports and explicit stop must not erase the cause.
	v.connection("connected", now)
	v.beginStop()
	v.configurationFailed(errors.New("late failure during cleanup"))
	v.finish(now)
	d = v.snapshot(now)
	if d.ConfigurationError != reason || d.Running || d.Ready {
		t.Fatal("teardown replaced original failure", d)
	}
	next := newVPNDetailsTracker(Request{}, nil).snapshot(now)
	if next.ConfigurationError != "" || next.RouteState == "error" {
		t.Fatal("new connection inherited old failure", next)
	}
}

func TestVPNConfigurationFailureWithoutReasonAndLateCallback(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	v.configurationFailed(nil)
	if d := v.snapshot(time.Now()); d.RouteState != "error" || d.ConfigurationError != "" || d.Ready {
		t.Fatal(d)
	}
	v.finish(time.Now())
	v.configurationFailed(errors.New("obsolete callback"))
	if v.snapshot(time.Now()).ConfigurationError != "" {
		t.Fatal("late callback changed finished session")
	}
}

func TestVPNDetailsPublishesOnlyAcceptedTunnelStages(t *testing.T) {
	lifecycle := newVPNTunnelLifecycle()
	v := newVPNDetailsTracker(Request{}, nil)
	c := &callback{mode: ModeVPNClient, vpnDetails: v, onP2PReport: lifecycle.report, onReady: lifecycle.ready}
	for _, stage := range []string{"wait", "discovering", "connecting", "negotiating", "connected"} {
		c.P2PReport("a", "", stage, "", "", "", 1, 0)
		if d := v.snapshot(time.Now()); d.TunnelStatus != stage || d.Ready {
			t.Fatal("stage lost or tunnel incorrectly considered ready", d)
		}
	}
	c.Ready("socks5://127.0.0.1:1000")
	v.routes("applied", nil, nil, nil)
	c.P2PReport("a", "", "connecting", "", "", "", 1, 0)
	c.P2PReport("loser", "", "wait", "", "", "", 1, 0)
	if d := v.snapshot(time.Now()); d.TunnelStatus != "connected" || !d.Ready {
		t.Fatal("obsolete attempt replaced ready status", d)
	}
	c.P2PReport("a", "", "disconnected", "", "", "", 2, 0)
	c.P2PReport("b", "", "wait", "", "", "", 3, 0)
	if d := v.snapshot(time.Now()); d.TunnelStatus != "wait" || d.Ready {
		t.Fatal("next attempt stage not published", d)
	}
	v.beginStop()
	c.P2PReport("b", "", "negotiating", "", "", "", 4, 0)
	if v.snapshot(time.Now()).TunnelStatus != "stopping" {
		t.Fatal("late stage overwrote manual stop")
	}
	v.finish(time.Now())
	if v.snapshot(time.Now()).TunnelStatus != "stopped" {
		t.Fatal("missing stopped state")
	}
}

func TestVPNReadinessRequiresCurrentSystemConfiguration(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	now := time.Now()
	v.connectionReport(P2PStatusReport{Topic: "a", Status: "connected"}, now)
	v.socksReady()
	if d := v.snapshot(now); !d.Connected || d.Ready || d.EverReady {
		t.Fatal("tunnel Ready must not imply system VPN readiness", d)
	}
	v.routes("applying", nil, nil, nil)
	if v.snapshot(now).Ready {
		t.Fatal("configuration in progress is not ready")
	}
	v.routes("applied", nil, nil, []string{"8.8.8.8"})
	if d := v.snapshot(now); !d.Ready || !d.EverReady {
		t.Fatal(d)
	}
	// Duplicate reports do not invalidate a healthy connection.
	v.connectionReport(P2PStatusReport{Topic: "a", Status: "connected"}, now)
	if !v.snapshot(now).Ready {
		t.Fatal("duplicate connected cleared readiness")
	}
	// A new topic must invalidate readiness even if the old disconnect was absent.
	v.connectionReport(P2PStatusReport{Topic: "b", Status: "connected"}, now)
	if d := v.snapshot(now); d.Ready || !d.EverReady || d.RouteState != "pending" || len(d.DNSServers) != 0 {
		t.Fatal("new connection inherited old configuration", d)
	}
	v.socksReady()
	v.routes("applied", nil, nil, nil)
	v.connectionReport(P2PStatusReport{Topic: "b", Status: "disconnected"}, now)
	if d := v.snapshot(now); d.Ready || d.Connected || !d.EverReady || len(d.DNSServers) != 0 {
		t.Fatal("disconnect retained readiness", d)
	}
	v.connectionReport(P2PStatusReport{Topic: "c", Status: "connected"}, now)
	v.socksReady()
	v.routes("error", nil, nil, nil)
	if v.snapshot(now).Ready {
		t.Fatal("configuration failure reported ready")
	}
	v.beginStop()
	v.routes("applied", nil, nil, nil)
	v.socksReady()
	if v.snapshot(now).Ready {
		t.Fatal("late callbacks revived stopped VPN")
	}
}

func TestVPNTunnelOnlyReadinessRequiresAcceptedLocalReady(t *testing.T) {
	lifecycle := newVPNTunnelLifecycle()
	v := newVPNDetailsTracker(Request{TunnelOnly: true}, nil)
	c := &callback{mode: ModeVPNClient, vpnDetails: v, onP2PReport: lifecycle.report, onReady: lifecycle.ready}
	c.P2PReport("a", "", "connected", "", "", "", 1, 0)
	if v.snapshot(time.Now()).Ready {
		t.Fatal("connected report preceded local SOCKS5 service")
	}
	c.Ready("socks5://127.0.0.1:1000")
	if d := v.snapshot(time.Now()); !d.Ready || !d.EverReady || d.RouteState != "tunnelOnly" {
		t.Fatal(d)
	}
	c.P2PReport("a", "", "disconnected", "", "", "", 2, 0)
	c.Ready("socks5://127.0.0.1:1000")
	if v.snapshot(time.Now()).Ready {
		t.Fatal("late local Ready revived disconnected tunnel")
	}
	c.P2PReport("b", "", "connected", "", "", "", 3, 0)
	if v.snapshot(time.Now()).Ready {
		t.Fatal("reconnection inherited old SOCKS5 readiness")
	}
	c.Ready("socks5://127.0.0.1:2000")
	if !v.snapshot(time.Now()).Ready {
		t.Fatal("new local SOCKS5 service did not become ready")
	}
}

func TestVPNDetailsSessionAndReconnectTimes(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	now := time.Now()
	if got := v.snapshot(now); got.SessionStartedAt != "" || got.LastConnectedAt != "" {
		t.Fatal(got)
	}
	v.connection("connecting", now)
	v.connection("connected", now.Add(time.Minute))
	first := v.snapshot(now.Add(2 * time.Minute))
	if first.DurationSeconds != 60 {
		t.Fatal(first)
	}
	v.connection("connected", now.Add(3*time.Minute))
	if got := v.snapshot(now); got.LastConnectedAt != first.LastConnectedAt {
		t.Fatal("duplicate report reset last connection")
	}
	v.connection("disconnected", now.Add(4*time.Minute))
	v.connection("connecting", now.Add(5*time.Minute))
	v.connection("connected", now.Add(6*time.Minute))
	got := v.snapshot(now.Add(10*24*time.Hour + 7*time.Minute))
	if got.SessionStartedAt != first.SessionStartedAt || got.LastConnectedAt != now.Add(6*time.Minute).Format(time.RFC3339Nano) || got.DurationSeconds != 10*86400+360 {
		t.Fatal(got)
	}
	v.beginStop()
	v.connection("connected", now.Add(11*24*time.Hour))
	v.finish(now.Add(10*24*time.Hour + 8*time.Minute))
	stopped := v.snapshot(now.Add(30 * 24 * time.Hour))
	if stopped.Running || stopped.Connected || stopped.DurationSeconds != 10*86400+420 || stopped.LastConnectedAt != got.LastConnectedAt {
		t.Fatal(stopped)
	}
	fresh := newVPNDetailsTracker(Request{}, nil)
	fresh.connection("connected", now.Add(31*24*time.Hour))
	if fresh.snapshot(now.Add(31*24*time.Hour)).DurationSeconds != 0 {
		t.Fatal("new manual session retained old duration")
	}
}

func TestVPNDetailsTrafficCountersAndFinalSample(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	now := time.Now()
	v.connection("connected", now)
	v.traffic(123, 456, 12, 45, false, now)
	d := v.snapshot(now)
	if d.InBytes != 123 || d.OutBytes != 456 || d.InBps != 12 || d.OutBps != 45 {
		t.Fatal(d)
	}
	v.connection("disconnected", now)
	v.connection("connected", now.Add(time.Second))
	if v.snapshot(now).InBytes != 123 {
		t.Fatal("automatic reconnect cleared session totals")
	}
	v.traffic(200, 500, 99, 99, true, now)
	if d = v.snapshot(now); d.InBps != 0 || d.OutBps != 0 || d.InBytes != 200 {
		t.Fatal(d)
	}
	v.finish(now)
	v.traffic(201, 501, 999, 999, true, now)
	if d = v.snapshot(now); d.InBps != 0 || d.OutBps != 0 || d.OutBytes != 501 {
		t.Fatal(d)
	}
}

func TestVPNDetailsProbeFailureNeverFiltersIPv6Routes(t *testing.T) {
	v := newVPNDetailsTracker(Request{EnableIPv6: true}, nil)
	if got := v.snapshot(time.Now()); len(got.Routes) != 4 || len(got.DNSServers) != 0 || got.RouteState != "pending" {
		t.Fatal(got)
	}
	actual := routeLines("", false)
	v.routes("applied", actual, []string{"192.0.2.1"}, dnsLines("9.9.9.9\n2001:4860:4860::8888", false))
	d := v.snapshot(time.Now())
	if len(d.ConfiguredRoutes) != 4 || len(d.Routes) != 4 || len(d.DNSServers) != 1 || d.DNSServers[0] != "9.9.9.9" {
		t.Fatal(d)
	}
	d.Routes[0] = "modified"
	d.DNSServers[0] = "modified"
	if v.snapshot(time.Now()).Routes[0] == "modified" || v.snapshot(time.Now()).DNSServers[0] == "modified" {
		t.Fatal("snapshot aliases tracker")
	}
	v.routes("paused", nil, nil, nil)
	if v.snapshot(time.Now()).RouteState != "paused" {
		t.Fatal("missing paused state")
	}
	v.routes("error", nil, nil, nil)
	v.finish(time.Now())
	if v.snapshot(time.Now()).RouteState != "error" {
		t.Fatal("lost apply error")
	}
	tunnel := newVPNDetailsTracker(Request{TunnelOnly: true, EnableIPv6: true}, nil).snapshot(time.Now())
	if !tunnel.TunnelOnly || len(tunnel.Routes) != 0 || len(tunnel.DNSServers) != 0 {
		t.Fatal(tunnel)
	}
}

func TestVPNDetailsConcurrentUpdates(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				v.connection("connected", time.Now())
				v.traffic(int64(j), int64(j), 1, 2, false, time.Now())
				v.routes("applied", []string{"0.0.0.0/0"}, nil, []string{"8.8.8.8"})
				v.snapshot(time.Now())
			}
		}()
	}
	wg.Wait()
}

func TestVPNDetailsReportsFallbackDNSOnlyAfterApplication(t *testing.T) {
	v := newVPNDetailsTracker(Request{DNSServers: "2001:4860:4860::8888"}, nil)
	if len(v.snapshot(time.Now()).DNSServers) != 0 {
		t.Fatal("DNS not yet applied")
	}
	v.routes("applied", nil, nil, []string{"8.8.8.8"})
	if got := v.snapshot(time.Now()); len(got.DNSServers) != 1 || got.DNSServers[0] != "8.8.8.8" {
		t.Fatal(got)
	}
	v.routes("applying", nil, nil, []string{})
	if len(v.snapshot(time.Now()).DNSServers) != 0 {
		t.Fatal("old DNS survived reapplication")
	}
}
