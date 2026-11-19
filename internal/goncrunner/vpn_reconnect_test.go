package goncrunner

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/threatexpert/gonc/v2/goncembed"
)

type fakeTunnel struct {
	done chan struct{}
	once sync.Once
}

func (f *fakeTunnel) Stop()                 { f.once.Do(func() { close(f.done) }) }
func (f *fakeTunnel) Done() <-chan struct{} { return f.done }

type attemptRecord struct {
	ip string
	cb goncembed.Callback
	s  *fakeTunnel
}

func TestReconnectRefreshesOnlyAfterLossAndRejectsRetiredCallbacks(t *testing.T) {
	attempts := make(chan attemptRecord, 8)
	var refreshes, ready atomic.Int32
	lifecycle := newVPNTunnelLifecycle()
	cb := &callback{onP2PReport: lifecycle.report, onReady: func(e string) bool { ready.Add(1); return lifecycle.ready(e) }}
	var previous *fakeTunnel
	a := reconnectActions{callback: cb, retry: 5 * time.Millisecond,
		refresh:      func() (string, error) { refreshes.Add(1); return "192.168.50.40:0", nil },
		disconnected: lifecycle.disconnect, report: func(error) {},
		start: func(ip string, c goncembed.Callback) (runnerSession, error) {
			if previous != nil {
				select {
				case <-previous.done:
				default:
					t.Error("overlapping gonc sessions")
				}
			}
			f := &fakeTunnel{done: make(chan struct{})}
			previous = f
			attempts <- attemptRecord{ip, c, f}
			return f, nil
		},
	}
	managed, err := startReconnectSession("192.168.50.220:0", a)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { managed.Stop(); <-managed.Done() }()
	first := <-attempts
	first.cb.P2PReport("a", "", "connected", "tcp4", "P2P", "", 0, 0)
	first.cb.Ready("socks5://127.0.0.1:1000")
	time.Sleep(30 * time.Millisecond)
	if refreshes.Load() != 0 {
		t.Fatal("inspected physical network while connected")
	}
	// A competing failed attempt must not disrupt the established tunnel.
	first.cb.P2PReport("loser", "", "error", "tcp4", "LAN", "", 0, 0)
	time.Sleep(15 * time.Millisecond)
	if refreshes.Load() != 0 {
		t.Fatal("competing report triggered refresh")
	}
	first.cb.P2PReport("a", "", "disconnected", "tcp4", "P2P", "", 0, 0)
	var second attemptRecord
	select {
	case second = <-attempts:
	case <-time.After(time.Second):
		t.Fatal("no rebuild after loss")
	}
	if second.ip != "192.168.50.40:0" || refreshes.Load() != 1 {
		t.Fatal(second.ip, refreshes.Load())
	}
	first.cb.Ready("socks5://127.0.0.1:9999")
	if ready.Load() != 1 {
		t.Fatal("retired callback accepted")
	}
	second.cb.P2PReport("b", "", "connected", "tcp4", "P2P", "", 0, 0)
	second.cb.Ready("socks5://127.0.0.1:2000")
	time.Sleep(30 * time.Millisecond)
	if refreshes.Load() != 1 {
		t.Fatal("periodic refresh after reconnection")
	}
}

func TestStopDuringRebuildCannotLaunchAnotherTunnel(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var starts atomic.Int32
	initial := &fakeTunnel{done: make(chan struct{})}
	managed, err := startReconnectSession("192.168.50.2:0", reconnectActions{
		callback: &callback{}, retry: time.Millisecond,
		start:        func(string, goncembed.Callback) (runnerSession, error) { starts.Add(1); return initial, nil },
		refresh:      func() (string, error) { close(entered); <-release; return "192.168.50.3:0", nil },
		disconnected: func() {}, report: func(error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	initial.Stop()
	<-entered
	managed.Stop()
	close(release)
	select {
	case <-managed.Done():
	case <-time.After(time.Second):
		t.Fatal("stop blocked")
	}
	if starts.Load() != 1 {
		t.Fatal("started after explicit stop")
	}
}

type retryCleanup struct {
	stops, closes int
	fail          bool
}

func (h *retryCleanup) Stop() error {
	h.stops++
	if h.fail {
		return errors.New("route still installed")
	}
	return nil
}
func (h *retryCleanup) Close() error { h.closes++; return nil }

func TestRunnerRetainsFailedCleanupForExplicitRetry(t *testing.T) {
	h := &retryCleanup{fail: true}
	d := newVPNDetailsTracker(Request{DisconnectProtection: true}, nil)
	d.routes("applied", nil, nil, nil)
	r := &Runner{helper: h, vpnDetails: d}
	if _, err := r.StopWait(time.Second); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	if !r.IsRunning() || h.closes != 0 || !d.snapshot(time.Now()).Running {
		t.Fatal("failed helper was discarded")
	}
	if err := r.Start(context.Background(), Request{Mode: ModeVPNClient, Password: "test123456"}, nil, nil); err == nil {
		t.Fatal("started over pending cleanup")
	}
	h.fail = false
	if _, err := r.StopWait(time.Second); err != nil {
		t.Fatal(err)
	}
	if r.IsRunning() || h.closes != 1 || d.snapshot(time.Now()).CaptureActive {
		t.Fatal("retry did not finish cleanup")
	}
}
