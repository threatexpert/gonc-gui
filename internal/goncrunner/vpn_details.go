package goncrunner

import (
	"strings"
	"sync"
	"time"
)

// VPNDetails describes this application's session and route configuration, not
// an audit of the OS routing table or a measurement of the whole network card.
type VPNDetails struct {
	DisconnectProtection bool     `json:"disconnectProtection"`
	CaptureActive        bool     `json:"captureActive"`
	Revision             uint64   `json:"revision"`
	Running              bool     `json:"running"`
	Connected            bool     `json:"connected"`
	Ready                bool     `json:"ready"`
	EverReady            bool     `json:"everReady"`
	ConnectionState      string   `json:"connectionState"`
	TunnelStatus         string   `json:"tunnelStatus"`
	ConnectionError      string   `json:"connectionError"`
	ConfigurationError   string   `json:"configurationError"`
	RetryRemainingMS     int64    `json:"retryRemainingMs"`
	SessionStartedAt     string   `json:"sessionStartedAt"`
	LastConnectedAt      string   `json:"lastConnectedAt"`
	DurationSeconds      int64    `json:"durationSeconds"`
	InBytes              int64    `json:"inBytes"`
	OutBytes             int64    `json:"outBytes"`
	InBps                float64  `json:"inBps"`
	OutBps               float64  `json:"outBps"`
	TrafficAt            string   `json:"trafficAt"`
	TunnelOnly           bool     `json:"tunnelOnly"`
	RouteState           string   `json:"routeState"`
	ConfiguredRoutes     []string `json:"configuredRoutes"`
	Routes               []string `json:"routes"`
	BypassIPs            []string `json:"bypassIps"`
	DNSServers           []string `json:"dnsServers"`
}

type vpnDetailsTracker struct {
	mu       sync.Mutex
	data     VPNDetails
	started  time.Time
	ended    time.Time
	stopping bool
	topic    string
	retryAt  time.Time
	sink     Sink
}

func newVPNDetailsTracker(req Request, sink Sink) *vpnDetailsTracker {
	routes := routeLines(req.RouteCIDRs, req.EnableIPv6)
	state := "pending"
	if req.TunnelOnly {
		routes = nil
		state = "tunnelOnly"
	}
	return &vpnDetailsTracker{sink: sink, data: VPNDetails{
		DisconnectProtection: req.DisconnectProtection && !req.TunnelOnly,
		Running:              true, TunnelOnly: req.TunnelOnly, RouteState: state,
		ConfiguredRoutes: routes, Routes: routes,
		DNSServers: []string{},
	}}
}

func (v *vpnDetailsTracker) connection(status string, now time.Time) {
	v.connectionReport(P2PStatusReport{Status: status}, now)
}

func (v *vpnDetailsTracker) connectionReport(report P2PStatusReport, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.data.Running || v.stopping {
		return
	}
	// Only accepted reports reach this tracker; obsolete/competing connection
	// reports are filtered by the lifecycle before updating the UI snapshot.
	v.data.TunnelStatus = strings.ToLower(strings.TrimSpace(report.Status))
	connected := v.data.TunnelStatus == "connected"
	if connected {
		v.data.ConnectionState, v.data.ConnectionError = "", ""
		v.retryAt = time.Time{}
	} else if reason := vpnConnectionFailureReason(report.Status); reason != "" {
		v.data.ConnectionState, v.data.ConnectionError = "failed", reason
		v.retryAt = time.Time{}
	} else if vpnAttemptProgress(report.Status) && (v.data.ConnectionState == "failed" || (!v.retryAt.IsZero() && !now.Before(v.retryAt))) {
		// Preserve the last failure while the next attempt is in progress.
		v.data.ConnectionState = "retrying"
		v.retryAt = time.Time{}
	}
	newConnection := connected && (!v.data.Connected || report.Topic != v.topic)
	if newConnection {
		if v.started.IsZero() {
			v.started = now
			v.data.SessionStartedAt = now.Format(time.RFC3339Nano)
		}
		v.data.LastConnectedAt = now.Format(time.RFC3339Nano)
		v.topic = report.Topic
	}
	if !connected || newConnection {
		v.data.Ready = false
		if v.data.DisconnectProtection && v.data.CaptureActive {
			v.data.RouteState = "protected"
			v.data.Connected = connected
			return
		}
		v.data.DNSServers = []string{}
		if v.data.RouteState != "error" {
			v.data.RouteState = "pending"
			if !connected && v.data.EverReady {
				v.data.RouteState = "paused"
			}
		}
	}
	v.data.Connected = connected
}

// Called only for an accepted Ready callback, serialized with connection reports.
// System VPN still needs the helper; tunnel-only mode is ready at this point.
func (v *vpnDetailsTracker) socksReady() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.data.Running || v.stopping {
		return
	}
	v.data.Ready = v.data.TunnelOnly && v.data.Connected
	if v.data.DisconnectProtection && v.data.CaptureActive {
		v.data.RouteState = "protected"
		return
	}
	v.data.EverReady = v.data.EverReady || v.data.Ready
	v.data.DNSServers = []string{}
	v.data.RouteState = "pending"
	if v.data.TunnelOnly {
		v.data.RouteState = "tunnelOnly"
	}
}

func (v *vpnDetailsTracker) traffic(in, out int64, inBps, outBps float64, final bool, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.data.Running {
		if final {
			v.data.InBytes, v.data.OutBytes = in, out
			v.data.TrafficAt = now.Format(time.RFC3339Nano)
		}
		return
	}
	// The final progress callback reports average speed, not current speed.
	if final || v.stopping {
		inBps, outBps = 0, 0
	}
	v.data.InBytes, v.data.OutBytes = in, out
	v.data.InBps, v.data.OutBps = inBps, outBps
	v.data.TrafficAt = now.Format(time.RFC3339Nano)
}

func (v *vpnDetailsTracker) routes(state string, routes, bypass, dns []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.data.Running || v.stopping {
		return
	}
	v.data.RouteState = state
	if state == "applied" || state == "protected" {
		v.data.CaptureActive = true
	}
	if state == "paused" {
		v.data.CaptureActive = false
	}
	v.data.Ready = state == "applied" && v.data.Connected
	v.data.EverReady = v.data.EverReady || v.data.Ready
	if routes != nil {
		v.data.Routes = append([]string{}, routes...)
	}
	if bypass != nil {
		v.data.BypassIPs = append([]string{}, bypass...)
	}
	if dns != nil {
		v.data.DNSServers = append([]string{}, dns...)
	}
}

// Keep configuration failures through teardown. A new user connection creates
// a new tracker; transport status/cleanup must not erase the useful cause.
func (v *vpnDetailsTracker) configurationFailed(err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.data.Running || v.stopping {
		return
	}
	v.data.RouteState = "error"
	v.data.Ready = false
	if err != nil {
		v.data.ConfigurationError = err.Error()
	}
}

func (v *vpnDetailsTracker) beginStop() {
	v.mu.Lock()
	v.stopping = true
	v.data.TunnelStatus = "stopping"
	v.data.ConnectionState, v.data.ConnectionError = "", ""
	v.retryAt = time.Time{}
	v.data.Connected = false
	v.data.Ready = false
	v.data.InBps, v.data.OutBps = 0, 0
	v.data.RouteState = "stopping"
	v.mu.Unlock()
	v.publish()
}

func (v *vpnDetailsTracker) finish(now time.Time) {
	v.mu.Lock()
	if v.data.Running {
		v.ended = now
		v.data.TunnelStatus = "stopped"
		v.data.Running, v.data.Connected = false, false
		v.data.CaptureActive = false
		v.data.Ready = false
		if !v.stopping && v.data.ConnectionState != "" {
			v.data.ConnectionState = "failed"
		}
		v.retryAt = time.Time{}
		v.data.InBps, v.data.OutBps = 0, 0
		if v.data.RouteState != "error" {
			v.data.RouteState = "stopped"
		}
	}
	v.mu.Unlock()
	v.publish()
}

func (v *vpnDetailsTracker) snapshot(now time.Time) VPNDetails {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.data.Revision++
	d := v.data
	if !v.retryAt.IsZero() && d.Running && !v.stopping && !d.Connected {
		d.RetryRemainingMS = max(0, v.retryAt.Sub(now).Milliseconds())
		if d.RetryRemainingMS == 0 {
			d.ConnectionState = "retrying"
		}
	}
	if !v.started.IsZero() {
		end := now
		if !v.ended.IsZero() {
			end = v.ended
		}
		d.DurationSeconds = max(0, int64(end.Sub(v.started).Seconds()))
	}
	d.ConfiguredRoutes = append([]string{}, d.ConfiguredRoutes...)
	d.Routes = append([]string{}, d.Routes...)
	d.BypassIPs = append([]string{}, d.BypassIPs...)
	d.DNSServers = append([]string{}, d.DNSServers...)
	return d
}

func (v *vpnDetailsTracker) publish() {
	if v.sink == nil {
		return
	}
	d := v.snapshot(time.Now())
	e := newEvent("vpn_details", "info", "VPN session details")
	e.VPNDetails = &d
	v.sink(e)
}

func (v *vpnDetailsTracker) publishUntil(done <-chan struct{}) {
	v.publish()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			v.publish()
		case <-done:
			return
		}
	}
}

// Cleanup failures retain a running task so Stop remains available for retry.
func (v *vpnDetailsTracker) cleanupFailed(err error) {
	v.mu.Lock()
	v.stopping = false
	v.data.Running = true
	v.data.Connected, v.data.Ready = false, false
	v.data.RouteState = "error"
	v.data.ConfigurationError = err.Error()
	v.mu.Unlock()
	v.publish()
}
