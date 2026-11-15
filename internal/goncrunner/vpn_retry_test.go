package goncrunner

import (
	"testing"
	"time"
)

func TestVPNRetryLogCountdownAndNextAttempt(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	now := time.Now()
	v.connectionReport(P2PStatusReport{Topic: "a", Status: "error:peer unavailable"}, now)
	if d := v.snapshot(now); d.ConnectionState != "failed" || d.ConnectionError != "peer unavailable" {
		t.Fatal(d)
	}
	if !v.connectionLog("20261115-205026 [gonc] Will retry in 10 seconds..", now) {
		t.Fatal("retry log not recognized")
	}
	for _, tc := range []struct {
		elapsed   time.Duration
		state     string
		remaining int64
	}{
		{0, "retryWait", 10000}, {time.Second, "retryWait", 9000},
		{10 * time.Second, "retrying", 0}, {20 * time.Second, "retrying", 0},
	} {
		d := v.snapshot(now.Add(tc.elapsed))
		if d.ConnectionState != tc.state || d.RetryRemainingMS != tc.remaining || d.ConnectionError != "peer unavailable" {
			t.Fatal(d)
		}
	}
	// A parallel path's progress must not erase a known pending retry timer.
	v.connectionReport(P2PStatusReport{Topic: "b", Status: "connecting"}, now.Add(time.Second))
	if v.snapshot(now.Add(time.Second)).ConnectionState != "retryWait" {
		t.Fatal("lost scheduled retry")
	}
	v.connectionReport(P2PStatusReport{Topic: "c", Status: "connecting"}, now.Add(11*time.Second))
	if v.snapshot(now.Add(11*time.Second)).ConnectionState != "retrying" {
		t.Fatal("missing next attempt")
	}
	v.connectionReport(P2PStatusReport{Topic: "c", Status: "connected"}, now.Add(12*time.Second))
	if d := v.snapshot(now.Add(12 * time.Second)); d.ConnectionState != "" || d.ConnectionError != "" || d.RetryRemainingMS != 0 {
		t.Fatal(d)
	}
}

func TestVPNRetryLogRecognizesOnlyExplicitGoncMessages(t *testing.T) {
	now := time.Now()
	for _, message := range []string{
		"[gonc] Will retry in -1 seconds...", "[gonc] Will retry in 9999999999999999999 seconds...",
		"[gonc] Will retry in 999999 seconds...", "[gonc] Will retry in 0 seconds...",
		"[:mux] Error reading UDP: timeout", "[HTTPSRV] P2P failed: timeout",
		"unrelated Will retry in 10 seconds...", "[gonc] fallback relay unavailable",
	} {
		v := newVPNDetailsTracker(Request{}, nil)
		if v.connectionLog(message, now) {
			t.Fatal("unrelated log changed VPN state:", message)
		}
	}
	v := newVPNDetailsTracker(Request{}, nil)
	if !v.connectionLog("20261115-205026 [gonc] P2P failed: handshake timeout\n20261115-205026 [gonc] Will retry in 7 seconds...", now) {
		t.Fatal("multiline log")
	}
	if d := v.snapshot(now); d.ConnectionError != "handshake timeout" || d.RetryRemainingMS != 7000 {
		t.Fatal(d)
	}
}

func TestVPNRetryLogsCannotOverrideConnectedOrStoppedSession(t *testing.T) {
	now := time.Now()
	v := newVPNDetailsTracker(Request{}, nil)
	lifecycle := newVPNTunnelLifecycle()
	c := &callback{mode: ModeVPNClient, vpnDetails: v, onP2PReport: lifecycle.report, onReady: lifecycle.ready}
	c.P2PReport("winner", "", "connected", "", "", "", 1, 0)
	for _, applied := range []bool{false, true} {
		if applied {
			v.routes("applied", nil, nil, nil)
		}
		c.P2PReport("loser", "", "error:timeout", "", "", "", 1, 0)
		c.Event("info", "20261115-205026 [gonc] P2P failed: timeout")
		c.Event("info", "20261115-205026 [gonc] Will retry in 10 seconds...")
		if d := v.snapshot(now); !d.Connected || d.ConnectionState != "" || d.ConnectionError != "" || d.Ready != applied {
			t.Fatal(d)
		}
	}
	c.P2PReport("winner", "", "disconnected", "", "", "", 2, 0)
	c.Event("info", "[gonc] Will retry in 10 seconds...")
	if d := v.snapshot(time.Now()); d.ConnectionState != "retryWait" || !d.EverReady {
		t.Fatal(d)
	}
	v.beginStop()
	c.Event("info", "[gonc] P2P failed: operation canceled")
	c.Event("info", "[gonc] Will retry in 10 seconds...")
	c.Stopped(1)
	v.finish(now)
	if d := v.snapshot(now); d.ConnectionState != "" || d.ConnectionError != "" || d.RetryRemainingMS != 0 {
		t.Fatal(d)
	}
}

func TestVPNRetryTerminalFailureKeepsReasonAndNewSessionResets(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	c := &callback{mode: ModeVPNClient, vpnDetails: v}
	c.Error("invalid configuration")
	c.Stopped(1)
	v.finish(time.Now())
	if d := v.snapshot(time.Now()); d.Running || d.ConnectionState != "failed" || d.ConnectionError != "invalid configuration" {
		t.Fatal(d)
	}
	if v.connectionLog("Will retry in 10 seconds...", time.Now()) {
		t.Fatal("stopped session accepted retry")
	}
	fresh := newVPNDetailsTracker(Request{}, nil).snapshot(time.Now())
	if fresh.ConnectionState != "" || fresh.ConnectionError != "" || fresh.RetryRemainingMS != 0 {
		t.Fatal(fresh)
	}
}

func TestVPNRetryChangedLogFallsBackToStructuredReports(t *testing.T) {
	v := newVPNDetailsTracker(Request{}, nil)
	lifecycle := newVPNTunnelLifecycle()
	c := &callback{mode: ModeVPNClient, vpnDetails: v, onP2PReport: lifecycle.report, onReady: lifecycle.ready}
	c.P2PReport("a", "", "error:peer discovery timed out", "", "", "", 1, 0)
	// A future Gonc version changes the text. Do not guess a retry delay.
	c.Event("info", "[gonc] Retrying connection after 10s")
	if d := v.snapshot(time.Now()); d.ConnectionState != "failed" || d.ConnectionError != "peer discovery timed out" || d.RetryRemainingMS != 0 {
		t.Fatal("unknown log hid the structured failure or invented a countdown", d)
	}
	c.P2PReport("b", "", "connecting", "", "", "", 2, 0)
	if d := v.snapshot(time.Now()); d.ConnectionState != "retrying" || d.ConnectionError != "peer discovery timed out" {
		t.Fatal("next attempt was not detected from structured report", d)
	}
	c.P2PReport("b", "", "connected", "", "", "", 3, 0)
	c.Ready("socks5://127.0.0.1:1000")
	v.routes("applied", nil, nil, nil)
	if d := v.snapshot(time.Now()); !d.Ready || d.ConnectionState != "" || d.ConnectionError != "" {
		t.Fatal("recovered connection retained failure", d)
	}
}
