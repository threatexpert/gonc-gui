package goncrunner

import (
	"context"
	"strings"
	"sync"
)

type vpnTunnelSnapshot struct {
	revision uint64
	readyID  uint64
	ready    bool
	endpoint string
	status   string
	bypass   []string
}

// A single latest-state mailbox replaces independent ready/lost queues. Wakeups
// may coalesce, but the state itself is never dropped or reordered by select.
type vpnTunnelLifecycle struct {
	mu        sync.Mutex
	wake      chan struct{}
	state     vpnTunnelSnapshot
	topic     string
	connected bool
	retired   []string
}

func newVPNTunnelLifecycle() *vpnTunnelLifecycle {
	return &vpnTunnelLifecycle{wake: make(chan struct{}, 1)}
}

func (v *vpnTunnelLifecycle) notifyLocked() {
	v.state.revision++
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

// report returns false for reports belonging to an old/competing connection.
// Gonc uses a new Topic for each connection attempt and reports connected before
// publishing the local SOCKS5 Ready callback.
func (v *vpnTunnelLifecycle) report(r P2PStatusReport) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	status := strings.ToLower(strings.TrimSpace(r.Status))
	for _, topic := range v.retired {
		if r.Topic != "" && r.Topic == topic {
			return false
		}
	}
	if status == "connected" {
		newConnection := !v.connected || r.Topic != v.topic
		if v.topic != "" && r.Topic != "" && v.topic != r.Topic {
			v.retired = append(v.retired, v.topic)
			if len(v.retired) > 128 {
				v.retired = v.retired[1:]
			}
		}
		v.topic, v.connected = r.Topic, true
		if newConnection {
			v.state.ready = false // connected alone is not a SOCKS5 Ready signal.
			v.state.status = "connecting"
		}
		ips := extractPeerIPs(r.Peer)
		changed := strings.Join(ips, "\n") != strings.Join(v.state.bypass, "\n")
		if len(ips) > 0 && changed {
			v.state.bypass = ips
		}
		if newConnection || (len(ips) > 0 && changed) {
			v.notifyLocked()
		}
		return true
	}
	if v.connected {
		// A losing LAN/P2P attempt must not stop the winning connection.
		if v.topic != "" && r.Topic != "" && r.Topic != v.topic {
			return false
		}
		// These are pre-connection stages, never a new loss of this Topic.
		switch status {
		case "wait", "waiting", "idle", "ready", "discovering", "connecting", "negotiating", "reconnecting":
			return false
		}
	}
	if !vpnTunnelNeedsRoutePause(status) {
		return true
	}
	v.connected = false
	v.state.ready, v.state.status = false, status
	v.notifyLocked()
	return true
}

func (v *vpnTunnelLifecycle) ready(endpoint string) bool {
	if !strings.HasPrefix(endpoint, "socks5://") {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	// If this connection already reported a loss, a late Ready cannot revive it.
	if v.topic != "" && !v.connected {
		return false
	}
	v.state.ready, v.state.endpoint, v.state.status = true, endpoint, "connected"
	v.state.readyID++
	v.notifyLocked()
	return true
}

func (v *vpnTunnelLifecycle) snapshot() vpnTunnelSnapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	s := v.state
	s.bypass = append([]string{}, s.bypass...)
	return s
}

func (v *vpnTunnelLifecycle) current(s vpnTunnelSnapshot) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state.revision == s.revision
}

type vpnRouteActions struct {
	probe    func(context.Context, string) bool
	start    func(vpnTunnelSnapshot, bool) ([]string, error)
	stop     func() error
	applying func(vpnTunnelSnapshot, bool)
	applied  func(vpnTunnelSnapshot, bool, []string)
	paused   func(string, error)
}

// runVPNRouteLifecycle serializes helper operations and rechecks the revision
// after slow work. A genuine later loss still pauses; a newer Ready still
// restores routes even when it arrives while Stop or Start is in progress.
func runVPNRouteLifecycle(ctx context.Context, done <-chan struct{}, v *vpnTunnelLifecycle, a vpnRouteActions) error {
	var processed, probedReady uint64
	var applied, configuring, ipv6 bool
	stopped := func() bool {
		select {
		case <-ctx.Done():
			return true
		case <-done:
			return true
		default:
			return false
		}
	}
	for {
		if stopped() {
			return nil
		}
		s := v.snapshot()
		if s.revision == processed {
			select {
			case <-ctx.Done():
				return nil
			case <-done:
				return nil
			case <-v.wake:
			}
			continue
		}
		if !s.ready {
			processed = s.revision
			if !applied {
				if configuring && v.current(s) {
					a.paused(s.status, nil)
					configuring = false
				}
				continue
			}
			if !v.current(s) {
				continue
			}
			err := a.stop()
			if err == nil {
				applied = false
			}
			configuring = false
			if !stopped() && v.current(s) {
				a.paused(s.status, err)
			}
			continue
		}
		if s.readyID != probedReady {
			ipv6 = a.probe(ctx, s.endpoint)
			probedReady = s.readyID
		}
		if stopped() {
			return nil
		}
		if !v.current(s) {
			continue
		}
		configuring = true
		a.applying(s, ipv6)
		if applied {
			if err := a.stop(); err != nil {
				if stopped() {
					return nil
				}
				return err
			}
			applied = false
		}
		if stopped() {
			return nil
		}
		if !v.current(s) {
			continue
		}
		dns, err := a.start(s, ipv6)
		applied = err == nil
		processed = s.revision
		if stopped() {
			return nil
		}
		if !v.current(s) {
			continue
		}
		if err != nil {
			return err
		}
		configuring = false
		a.applied(s, ipv6, dns)
	}
}
