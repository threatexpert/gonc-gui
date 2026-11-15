package goncrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gonc-gui/internal/ipv6probe"
	"gonc-gui/internal/vpnconfig"
	"gonc-gui/internal/vpnhelper"
	"gonc-gui/internal/winvpn"

	"github.com/threatexpert/gonc/v2/goncembed"
	"github.com/threatexpert/gonc/v2/httpfileshare"
)

type Mode string

const (
	ModeSend      Mode = "send"
	ModeReceive   Mode = "receive"
	ModeVPNServer Mode = "vpnServer"
	ModeVPNClient Mode = "vpnClient"
)

type Request struct {
	Mode            Mode
	Password        string
	SharePaths      []string
	SaveDir         string
	DownloadSubPath string
	UseUDP          bool
	P2PWithLAN      bool
	Upstream        string
	DNSForward      string
	DNSServers      string
	RouteCIDRs      string
	LinkConfig      string
	MTU             int
	RouteMetric     int
	BlockDNSLeak    bool
	EnableIPv6      bool
	TunnelOnly      bool
	ExtraArgs       string
}

type Event struct {
	ClientRunID int64       `json:"clientRunId,omitempty"`
	VPNDetails  *VPNDetails `json:"vpnDetails,omitempty"`
	Type        string      `json:"type"`
	Level       string      `json:"level"`
	Message     string      `json:"message"`
	Time        string      `json:"time"`
	Mode        string      `json:"mode,omitempty"`
	LocalURL    string      `json:"localUrl,omitempty"`
	InBytes     int64       `json:"inBytes,omitempty"`
	OutBytes    int64       `json:"outBytes,omitempty"`
	InBps       float64     `json:"inBps,omitempty"`
	OutBps      float64     `json:"outBps,omitempty"`
	Elapsed     string      `json:"elapsed,omitempty"`
	PeerIPv6    string      `json:"peerIpv6,omitempty"`
}

type P2PStatusReport struct {
	Topic     string `json:"topic"`
	Status    string `json:"status"`
	Network   string `json:"network"`
	Mode      string `json:"mode"`
	Side      string `json:"side"`
	Peer      string `json:"peer"`
	Timestamp int64  `json:"timestamp"`
	PID       int    `json:"pid"`
}

type Sink func(Event)

type ReportSink func(P2PStatusReport)

type Runner struct {
	mu            sync.Mutex
	session       *goncembed.Session
	source        *dynamicFileSource
	helper        *vpnhelper.Client
	stopping      bool
	vpnDetails    *vpnDetailsTracker
	stopVPNRoutes func()
}

func New() *Runner {
	return &Runner{}
}

func fileTransferP2POptions(useUDP bool) goncembed.P2POptions {
	return goncembed.P2POptions{
		UseUDP:         useUDP,
		P2PWithLANMode: true,
	}
}

func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.session != nil
}

func (r *Runner) Start(parent context.Context, req Request, sink Sink, reportSink ReportSink) error {
	if err := validateRequest(req); err != nil {
		return err
	}

	r.mu.Lock()
	if r.session != nil {
		r.mu.Unlock()
		return errors.New("a transfer task is already running")
	}
	r.mu.Unlock()

	cb := &callback{
		mode:       req.Mode,
		sink:       sink,
		reportSink: reportSink,
	}
	if req.Mode == ModeVPNClient {
		cb.vpnDetails = newVPNDetailsTracker(req, sink)
	}

	var (
		session       *goncembed.Session
		source        *dynamicFileSource
		vpnHelper     *vpnhelper.Client
		stopVPNRoutes func()
		err           error
	)
	switch req.Mode {
	case ModeSend:
		source, err = newDynamicFileSource(req.SharePaths)
		if err != nil {
			return err
		}
		session, err = goncembed.StartP2PShareSourceWithOptions(source, req.Password, fileTransferP2POptions(req.UseUDP), cb)
	case ModeReceive:
		session, err = goncembed.StartP2PReceiveWithOptions(req.Password, fileTransferP2POptions(req.UseUDP), cb)
	case ModeVPNServer:
		session, err = goncembed.StartP2PLinkAgent(req.Password, req.UseUDP, req.Upstream, req.DNSForward, vpnExtraArgs(req), cb)
	case ModeVPNClient:
		vpnStart, err := startVPNClient(parent, req, cb, sink)
		if err != nil {
			return err
		}
		session = vpnStart.session
		vpnHelper = vpnStart.helper
		stopVPNRoutes = vpnStart.stopRoutes
	default:
		err = fmt.Errorf("unknown mode: %s", req.Mode)
	}
	if err != nil {
		return err
	}

	r.mu.Lock()
	if r.session != nil {
		r.mu.Unlock()
		session.Stop()
		if stopVPNRoutes != nil {
			stopVPNRoutes()
		}
		if vpnHelper != nil {
			_ = vpnHelper.Close()
		}
		return errors.New("a transfer task is already running")
	}
	r.session = session
	r.source = source
	r.helper = vpnHelper
	r.stopVPNRoutes = stopVPNRoutes
	r.stopping = false
	r.vpnDetails = cb.vpnDetails
	r.mu.Unlock()
	if cb.vpnDetails != nil {
		go cb.vpnDetails.publishUntil(session.Done())
	}

	emit(sink, "status", "info", "gonc embedded session started")
	go r.wait(parent, session, cb, sink)
	return nil
}

func (r *Runner) UpdateSharePaths(paths []string) error {
	r.mu.Lock()
	source := r.source
	running := r.session != nil
	r.mu.Unlock()
	if !running || source == nil {
		return errors.New("no share session is running")
	}
	return source.UpdatePaths(paths)
}

func (r *Runner) Stop() error {
	_, err := r.StopWait(0)
	return err
}

func (r *Runner) StopWait(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	session := r.session
	helper := r.helper
	details := r.vpnDetails
	stopVPNRoutes := r.stopVPNRoutes
	if session != nil {
		r.stopping = true
		r.helper = nil
		r.stopVPNRoutes = nil
	}
	r.mu.Unlock()
	if session == nil {
		return true, errors.New("no transfer task is running")
	}
	if details != nil {
		details.beginStop()
	}
	session.Stop()
	if stopVPNRoutes != nil {
		stopVPNRoutes()
	}
	if helper != nil {
		_ = helper.Stop()
		_ = helper.Close()
	}
	if details != nil {
		details.finish(time.Now())
	}
	if timeout <= 0 {
		return false, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-session.Done():
		return true, nil
	case <-timer.C:
		return false, nil
	}
}

func (r *Runner) wait(parent context.Context, session *goncembed.Session, cb *callback, sink Sink) {
	<-session.Done()

	r.mu.Lock()
	stopping := false
	if r.session == session {
		stopping = r.stopping
		r.session = nil
		r.source = nil
		if r.stopVPNRoutes != nil {
			r.stopVPNRoutes()
			r.stopVPNRoutes = nil
		}
		if r.helper != nil {
			_ = r.helper.Stop()
			_ = r.helper.Close()
			r.helper = nil
		}
		r.stopping = false
	}
	r.mu.Unlock()
	if cb.vpnDetails != nil && !stopping {
		cb.vpnDetails.finish(time.Now())
	}

	if stopping || (parent != nil && parent.Err() != nil) {
		emit(sink, "status", "warn", "transfer stopped")
		return
	}
	if cb.ExitCode() != 0 {
		return
	}
	emit(sink, "status", "info", "transfer finished")
}

func validateRequest(req Request) error {
	if req.Password == "" {
		return errors.New("password is required")
	}
	switch req.Mode {
	case ModeSend:
		if len(req.SharePaths) == 0 {
			return errors.New("select at least one file or folder to send")
		}
	case ModeVPNServer, ModeVPNClient:
		if hasDaemonArg(req.ExtraArgs) {
			return errors.New("daemon mode is not supported when embedded")
		}
	case ModeReceive:
	default:
		return fmt.Errorf("unknown mode: %s", req.Mode)
	}
	return nil
}

func hasDaemonArg(extraArgs string) bool {
	for _, arg := range splitExtraArgs(extraArgs) {
		if arg == "-daemon" || arg == "--daemon" ||
			strings.HasPrefix(arg, "-daemon=") || strings.HasPrefix(arg, "--daemon=") {
			return true
		}
	}
	return false
}

func buildArgs(req Request) ([]string, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}

	args := []string{"-p2p", req.Password}
	if req.UseUDP {
		args = append(args, "-u")
	}
	switch req.Mode {
	case ModeSend:
		args = append(args, "-httpserver")
		args = append(args, req.SharePaths...)
	case ModeReceive:
		args = append(args, "-httplocal")
	case ModeVPNServer:
		args = append(args, "-k", "-W", "-P")
		mux := ":mux linkagent"
		if upstream := strings.TrimSpace(req.Upstream); upstream != "" {
			mux += " -x " + upstream
		}
		if dnsForward := strings.TrimSpace(req.DNSForward); dnsForward != "" {
			mux += " -dns " + dnsForward
		}
		args = append(args, "-e", mux)
		if extraArgs := vpnExtraArgs(req); extraArgs != "" {
			args = append(args, splitExtraArgs(extraArgs)...)
		}
	case ModeVPNClient:
		linkConfig, err := effectiveLinkConfig(req.LinkConfig)
		if err != nil {
			return nil, err
		}
		args = append(args, "-link", linkConfig)
		if extraArgs := vpnExtraArgs(req); extraArgs != "" {
			args = append(args, splitExtraArgs(extraArgs)...)
		}
	}
	return args, nil
}

type vpnClientStart struct {
	session    *goncembed.Session
	helper     *vpnhelper.Client
	stopRoutes func()
}

func vpnExtraArgs(req Request) string {
	args := strings.TrimSpace(req.ExtraArgs)
	if req.P2PWithLAN {
		args = strings.TrimSpace(args + " -p2p-with-lan")
	}
	return args
}

func startVPNClient(parent context.Context, req Request, cb *callback, sink Sink) (*vpnClientStart, error) {
	linkConfig, err := effectiveLinkConfig(req.LinkConfig)
	if err != nil {
		return nil, err
	}
	var helper *vpnhelper.Client
	if !req.TunnelOnly {
		if err := winvpn.CheckRuntime(); err != nil {
			return nil, err
		}
		emit(sink, "status", "info", "Starting elevated VPN helper")
		helper, err = vpnhelper.StartElevated(parent)
		if err != nil {
			return nil, err
		}
		emit(sink, "status", "info", "Elevated VPN helper is ready")
	}
	if req.TunnelOnly || !req.EnableIPv6 {
		emitPeerIPv6(sink, "disabled")
	} else {
		emitPeerIPv6(sink, "waiting")
	}

	lifecycle := newVPNTunnelLifecycle()
	cb.onReady = lifecycle.ready
	cb.onP2PReport = lifecycle.report
	session, err := goncembed.StartP2PTunnel(req.Password, req.UseUDP, linkConfig, vpnExtraArgs(req), cb)
	if err != nil {
		if helper != nil {
			_ = helper.Close()
		}
		return nil, err
	}
	var stopRoutes func()
	if !req.TunnelOnly {
		ctx, cancel := context.WithCancel(parent)
		routesDone := make(chan struct{})
		stopRoutes = func() { cancel(); <-routesDone }
		go func() {
			defer close(routesDone)
			defer cancel()
			go func() {
				select {
				case <-session.Done():
					cancel()
				case <-ctx.Done():
				}
			}()
			configFor := func(s vpnTunnelSnapshot, ipv6 bool) vpnconfig.Config {
				return vpnconfig.Config{
					SOCKS5Endpoint: s.endpoint,
					Routes:         routeLines(req.RouteCIDRs, ipv6),
					DNSServers:     dnsLines(req.DNSServers, ipv6),
					BypassIPs:      s.bypass,
					EnableIPv6:     ipv6,
					MTU:            normalizeMTU(req.MTU),
					RouteMetric:    normalizeRouteMetric(req.RouteMetric),
					BlockDNSLeak:   req.BlockDNSLeak,
					LogLevel:       "warn",
				}
			}
			err := runVPNRouteLifecycle(ctx, session.Done(), lifecycle, vpnRouteActions{
				probe: func(ctx context.Context, endpoint string) bool {
					return resolveEffectiveIPv6(ctx, req.EnableIPv6, endpoint, sink)
				},
				start: func(s vpnTunnelSnapshot, ipv6 bool) ([]string, error) {
					return helper.Start(configFor(s, ipv6))
				},
				stop: helper.Stop,
				applying: func(s vpnTunnelSnapshot, ipv6 bool) {
					cb.vpnEventMu.Lock()
					defer cb.vpnEventMu.Unlock()
					if !lifecycle.current(s) {
						return
					}
					config := configFor(s, ipv6)
					cb.vpnDetails.routes("applying", config.Routes, config.BypassIPs, []string{})
					cb.vpnDetails.publish()
					emit(sink, "status", "info", "SOCKS5 endpoint ready; applying system VPN configuration")
				},
				applied: func(s vpnTunnelSnapshot, ipv6 bool, dns []string) {
					cb.vpnEventMu.Lock()
					defer cb.vpnEventMu.Unlock()
					if !lifecycle.current(s) {
						return
					}
					config := configFor(s, ipv6)
					cb.vpnDetails.routes("applied", config.Routes, config.BypassIPs, dns)
					cb.vpnDetails.publish()
					emit(sink, "status", "info", "System VPN started")
				},
				paused: func(status string, err error) {
					if err != nil {
						cb.vpnDetails.routes("error", nil, nil, nil)
						emit(sink, "status", "warn", "System VPN pause failed: "+err.Error())
					} else {
						cb.vpnDetails.routes("paused", nil, nil, nil)
						emit(sink, "status", "warn", "Tunnel status changed to "+status+"; system VPN routes paused for reconnect")
						emit(sink, "status", "info", "System VPN routes paused; waiting for SOCKS5 tunnel to be ready again")
					}
					cb.vpnDetails.publish()
					if req.EnableIPv6 {
						emitPeerIPv6(sink, "waiting")
					}
				},
			})
			if err != nil {
				cb.vpnDetails.routes("error", nil, nil, nil)
				cb.vpnDetails.publish()
				emit(sink, "status", "error", "System VPN start failed: "+err.Error())
				session.Stop()
			}
		}()
	}
	return &vpnClientStart{session: session, helper: helper, stopRoutes: stopRoutes}, nil
}

func resolveEffectiveIPv6(parent context.Context, requested bool, socks5Endpoint string, sink Sink) bool {
	if !requested {
		emitPeerIPv6(sink, "disabled")
		return false
	}
	emitPeerIPv6(sink, "checking")
	emit(sink, "status", "info", "Checking remote IPv6 availability")
	result := ipv6probe.Check(parent, socks5Endpoint, ipv6probe.DefaultTimeout)
	if result.Available {
		emitPeerIPv6(sink, "available")
		emit(sink, "status", "info", "Remote IPv6 is available via "+result.Detail+"; enabling IPv6 routes")
		return true
	}
	emitPeerIPv6(sink, "unavailable")
	emit(sink, "status", "warn", "Remote IPv6 is unavailable; IPv6 routes disabled ("+result.Detail+")")
	return false
}

func effectiveLinkConfig(value string) (string, error) {
	clean := strings.TrimSpace(value)
	if clean != "" {
		return clean, nil
	}
	return "x://127.0.0.1:0", nil
}

func normalizeMTU(value int) int {
	if value < 576 || value > 9000 {
		return 1400
	}
	return value
}

func normalizeRouteMetric(value int) int {
	if value < 1 || value > 9999 {
		return 5
	}
	return value
}

func routeLines(value string, enableIPv6 bool) []string {
	lines := splitLines(value)
	if len(lines) == 0 {
		lines = []string{"0.0.0.0/1", "128.0.0.0/1"}
		if enableIPv6 {
			lines = append(lines, "::/0")
		}
		return lines
	}
	if !enableIPv6 {
		filtered := lines[:0]
		for _, line := range lines {
			if !strings.Contains(line, ":") {
				filtered = append(filtered, line)
			}
		}
		return filtered
	}
	return lines
}

func dnsLines(value string, enableIPv6 bool) []string {
	lines := splitLines(value)
	if len(lines) == 0 {
		lines = []string{"8.8.8.8"}
		if enableIPv6 {
			lines = append(lines, "2001:4860:4860::8888")
		}
		return lines
	}
	if !enableIPv6 {
		filtered := lines[:0]
		for _, line := range lines {
			if !strings.Contains(line, ":") {
				filtered = append(filtered, line)
			}
		}
		return filtered
	}
	return lines
}

func extractPeerIPs(peer string) []string {
	peer = strings.TrimSpace(peer)
	if peer == "" {
		return nil
	}
	candidates := []string{peer}
	if host, _, err := net.SplitHostPort(peer); err == nil {
		candidates = append(candidates, host)
	}
	candidates = append(candidates, strings.Trim(peer, "[]"))

	var ips []string
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.Trim(strings.TrimSpace(candidate), "[]")
		if candidate == "" {
			continue
		}
		addr, err := netip.ParseAddr(candidate)
		if err != nil || !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() {
			continue
		}
		ip := addr.String()
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		ips = append(ips, ip)
	}
	return ips
}

func snapshotBypassIPs(mu *sync.Mutex, values map[string]struct{}) []string {
	mu.Lock()
	defer mu.Unlock()
	ips := make([]string, 0, len(values))
	for ip := range values {
		ips = append(ips, ip)
	}
	return ips
}

func replaceBypassIPs(mu *sync.Mutex, values map[string]struct{}, ips []string) bool {
	mu.Lock()
	defer mu.Unlock()
	next := map[string]struct{}{}
	for _, ip := range ips {
		next[ip] = struct{}{}
	}
	if len(next) == len(values) {
		same := true
		for ip := range next {
			if _, ok := values[ip]; !ok {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}
	for ip := range values {
		delete(values, ip)
	}
	for ip := range next {
		values[ip] = struct{}{}
	}
	return true
}

func vpnTunnelNeedsRoutePause(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "wait", "waiting", "idle", "ready", "connecting", "negotiating", "reconnecting", "disconnected", "disconnect", "closed", "stopped":
		return true
	}
	lower := strings.ToLower(status)
	return strings.Contains(lower, "fail") ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "lost") ||
		strings.Contains(lower, "timeout")
}

func splitLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitExtraArgs(extraArgs string) []string {
	var args []string
	var current strings.Builder
	inToken := false
	var quote rune
	for _, r := range extraArgs {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inToken {
				args = append(args, current.String())
				current.Reset()
				inToken = false
			}
		default:
			current.WriteRune(r)
			inToken = true
		}
	}
	if inToken {
		args = append(args, current.String())
	}
	return args
}

type callback struct {
	vpnDetails  *vpnDetailsTracker
	mode        Mode
	sink        Sink
	reportSink  ReportSink
	onReady     func(string) bool
	onP2PReport func(P2PStatusReport) bool
	vpnEventMu  sync.Mutex
	mu          sync.Mutex
	exitCode    int
}

func (c *callback) Event(level string, message string) {
	if c.vpnDetails != nil {
		c.vpnEventMu.Lock()
		if c.vpnDetails.connectionLog(message, time.Now()) {
			c.vpnDetails.publish()
		}
		c.vpnEventMu.Unlock()
	}
	flushLine(message, level, c.sink)
}

func (c *callback) P2PReport(topic string, side string, status string, network string, mode string, peer string, timestamp int64, pid int) {
	c.vpnEventMu.Lock()
	defer c.vpnEventMu.Unlock()
	report := P2PStatusReport{
		Topic: topic, Status: status, Network: network, Mode: mode,
		Side: string(c.mode), Peer: peer, Timestamp: timestamp, PID: pid,
	}
	if c.onP2PReport != nil && !c.onP2PReport(report) {
		return
	}
	if c.vpnDetails != nil {
		c.vpnDetails.connectionReport(report, time.Now())
		c.vpnDetails.publish()
	}
	if c.reportSink == nil {
		return
	}
	c.reportSink(report)
}

func (c *callback) Traffic(side string, inBytes int64, outBytes int64, inBps float64, outBps float64, elapsed int64, connCount int, final bool) {
	if c.vpnDetails != nil {
		c.vpnDetails.traffic(inBytes, outBytes, inBps, outBps, final, time.Now())
		c.vpnDetails.publish()
	}
	if c.sink == nil {
		return
	}
	event := Event{
		Type:     "traffic",
		Level:    "info",
		Message:  "traffic snapshot",
		Time:     time.Now().Format(time.RFC3339),
		Mode:     string(c.mode),
		InBytes:  inBytes,
		OutBytes: outBytes,
		InBps:    inBps,
		OutBps:   outBps,
		Elapsed:  formatElapsed(elapsed),
	}
	c.sink(event)
}

func (c *callback) Ready(endpoint string) {
	c.vpnEventMu.Lock()
	defer c.vpnEventMu.Unlock()
	if endpoint == "" {
		return
	}
	if c.onReady != nil && !c.onReady(endpoint) {
		return
	}
	if c.vpnDetails != nil {
		c.vpnDetails.socksReady()
		c.vpnDetails.publish()
	}
	eventType := "local_http"
	message := "local HTTP endpoint is ready"
	if c.mode == ModeVPNClient {
		eventType = "socks5"
		message = "SOCKS5 endpoint is ready: " + endpoint
	}
	event := newEvent(eventType, "info", message)
	event.LocalURL = endpoint
	if c.sink != nil {
		c.sink(event)
	}
}

func (c *callback) Stopped(exitCode int) {
	c.mu.Lock()
	c.exitCode = exitCode
	c.mu.Unlock()
	if exitCode != 0 {
		if c.vpnDetails != nil {
			c.vpnDetails.connectionFailure(fmt.Sprintf("gonc exited with code %d", exitCode), true)
			c.vpnDetails.publish()
		}
		emit(c.sink, "status", "error", fmt.Sprintf("gonc exited with code %d", exitCode))
	}
}

func (c *callback) Error(message string) {
	if c.vpnDetails != nil {
		c.vpnDetails.connectionFailure(message, false)
		c.vpnDetails.publish()
	}
	emit(c.sink, "status", "error", message)
}

func (c *callback) ExitCode() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitCode
}

func formatElapsed(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

type dynamicFileSource struct {
	mu     sync.RWMutex
	source *httpfileshare.OSFileSource
}

func newDynamicFileSource(paths []string) (*dynamicFileSource, error) {
	source := &dynamicFileSource{}
	if err := source.UpdatePaths(paths); err != nil {
		return nil, err
	}
	return source, nil
}

func (s *dynamicFileSource) UpdatePaths(paths []string) error {
	var next *httpfileshare.OSFileSource
	var err error
	if len(paths) == 0 {
		next = &httpfileshare.OSFileSource{}
	} else {
		next, err = httpfileshare.NewOSFileSource(paths)
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.source = next
	s.mu.Unlock()
	return nil
}

func (s *dynamicFileSource) snapshot() *httpfileshare.OSFileSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.source
}

func (s *dynamicFileSource) Description() string {
	source := s.snapshot()
	if source == nil {
		return "empty file source"
	}
	return source.Description()
}

func (s *dynamicFileSource) Stat(name string) (fs.FileInfo, error) {
	source := s.snapshot()
	if source == nil {
		return nil, os.ErrNotExist
	}
	return source.Stat(name)
}

func (s *dynamicFileSource) Open(name string) (fs.File, error) {
	source := s.snapshot()
	if source == nil {
		return nil, os.ErrNotExist
	}
	return source.Open(name)
}

func (s *dynamicFileSource) ReadDir(name string) ([]fs.FileInfo, error) {
	source := s.snapshot()
	if source == nil {
		return nil, os.ErrNotExist
	}
	return source.ReadDir(name)
}

func (s *dynamicFileSource) Walk(name string, fn func(sourcePath string, info fs.FileInfo, err error) error) error {
	source := s.snapshot()
	if source == nil {
		return os.ErrNotExist
	}
	return source.Walk(name, fn)
}

var trafficPattern = regexp.MustCompile(`IN:\s+.*?\((\d+)\s+bytes\),\s+([^|]+?)/s\s+\|\s+OUT:\s+.*?\((\d+)\s+bytes\),\s+([^|]+?)/s(?:\s+\|\s+\d+)?\s+\|\s+(\d{2}:\d{2}:\d{2})`)

func flushLine(line, level string, sink Sink) {
	if sink == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if event, ok := parseTraffic(line); ok {
		sink(event)
		return
	}
	emit(sink, "log", level, line)
}

func parseTraffic(line string) (Event, bool) {
	match := trafficPattern.FindStringSubmatch(line)
	if match == nil {
		return Event{}, false
	}
	inBytes, _ := strconv.ParseInt(match[1], 10, 64)
	outBytes, _ := strconv.ParseInt(match[3], 10, 64)
	return Event{
		Type:     "traffic",
		Level:    "info",
		Message:  line,
		Time:     time.Now().Format(time.RFC3339),
		InBytes:  inBytes,
		OutBytes: outBytes,
		InBps:    parseRate(match[2]),
		OutBps:   parseRate(match[4]),
		Elapsed:  match[5],
	}, true
}

func parseRate(text string) float64 {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return 0
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(fields[1]) {
	case "b":
		return value
	case "kb", "kib":
		return value * 1024
	case "mb", "mib":
		return value * 1024 * 1024
	case "gb", "gib":
		return value * 1024 * 1024 * 1024
	default:
		return value
	}
}

func emit(sink Sink, typ, level, message string) {
	if sink == nil {
		return
	}
	sink(newEvent(typ, level, message))
}

func emitPeerIPv6(sink Sink, state string) {
	if sink == nil {
		return
	}
	event := newEvent("peer_ipv6", "info", state)
	event.PeerIPv6 = state
	sink(event)
}

func newEvent(typ, level, message string) Event {
	return Event{
		Type:    typ,
		Level:   level,
		Message: message,
		Time:    time.Now().Format(time.RFC3339),
	}
}
