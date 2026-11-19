package goncrunner

import (
	"context"
	"testing"
	"time"

	"gonc-gui/internal/vpnhelper"
)

// These modes never create an elevated VPN helper. Exercise the same concrete
// pointer-to-interface conversion as Start, then both shutdown paths.
func TestHelperlessTasksStopAndReturnToIdle(t *testing.T) {
	for _, mode := range []Mode{ModeVPNServer, ModeSend, ModeReceive, ModeVPNClient} {
		for _, explicit := range []bool{true, false} {
			t.Run(string(mode)+map[bool]string{true: "/stop", false: "/exit"}[explicit], func(t *testing.T) {
				var client *vpnhelper.Client
				session := &fakeTunnel{done: make(chan struct{})}
				r := &Runner{session: session, helper: cleanupForClient(client)}
				cb := &callback{mode: mode}
				if !r.IsRunning() {
					t.Fatal("live session was not running")
				}
				if explicit {
					stopped, err := r.StopWait(time.Second)
					if err != nil || !stopped {
						t.Fatalf("stop=%v err=%v", stopped, err)
					}
				} else {
					session.Stop()
				}
				r.waitForSession(context.Background(), session, cb, nil, session.Done())
				if r.IsRunning() {
					t.Fatal("finished task still reports running")
				}
				if r.helper != nil {
					t.Fatal("helper-less task retained a cleanup resource")
				}
			})
		}
	}
}

func TestRealVPNHelperIsRetainedForCleanup(t *testing.T) {
	client := &vpnhelper.Client{}
	if cleanupForClient(client) != client {
		t.Fatal("discarded real VPN helper")
	}
}
