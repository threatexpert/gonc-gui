package goncrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
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
	Upstream        string
	DNSForward      string
	DNSServers      string
	RouteCIDRs      string
	LinkConfig      string
	EnableIPv6      bool
	TunnelOnly      bool
	ExtraArgs       string
}

type Event struct {
	Type     string  `json:"type"`
	Level    string  `json:"level"`
	Message  string  `json:"message"`
	Time     string  `json:"time"`
	Mode     string  `json:"mode,omitempty"`
	LocalURL string  `json:"localUrl,omitempty"`
	InBytes  int64   `json:"inBytes,omitempty"`
	OutBytes int64   `json:"outBytes,omitempty"`
	InBps    float64 `json:"inBps,omitempty"`
	OutBps   float64 `json:"outBps,omitempty"`
	Elapsed  string  `json:"elapsed,omitempty"`
	PeerIPv6 string  `json:"peerIpv6,omitempty"`
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
	mu       sync.Mutex
	session  *goncembed.Session
	source   *dynamicFileSource
	helper   *vpnhelper.Client
	stopping bool
}

func New() *Runner {
	return &Runner{}
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

	var (
		session   *goncembed.Session
		source    *dynamicFileSource
		vpnHelper *vpnhelper.Client
		err       error
	)
	switch req.Mode {
	case ModeSend:
		source, err = newDynamicFileSource(req.SharePaths)
		if err != nil {
			return err
		}
		session, err = goncembed.StartP2PShareSource(source, req.Password, req.UseUDP, cb)
	case ModeReceive:
		session, err = goncembed.StartP2PReceive(req.Password, req.UseUDP, cb)
	case ModeVPNServer:
		session, err = goncembed.StartP2PLinkAgent(req.Password, req.UseUDP, req.Upstream, req.DNSForward, req.ExtraArgs, cb)
	case ModeVPNClient:
		vpnStart, err := startVPNClient(parent, req, cb, sink)
		if err != nil {
			return err
		}
		session = vpnStart.session
		vpnHelper = vpnStart.helper
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
		if vpnHelper != nil {
			_ = vpnHelper.Close()
		}
		return errors.New("a transfer task is already running")
	}
	r.session = session
	r.source = source
	r.helper = vpnHelper
	r.stopping = false
	r.mu.Unlock()

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
	if session != nil {
		r.stopping = true
		r.helper = nil
	}
	r.mu.Unlock()
	if session == nil {
		return true, errors.New("no transfer task is running")
	}
	session.Stop()
	if helper != nil {
		_ = helper.Stop()
		_ = helper.Close()
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
		if r.helper != nil {
			_ = r.helper.Stop()
			_ = r.helper.Close()
			r.helper = nil
		}
		r.stopping = false
	}
	r.mu.Unlock()

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
	case ModeReceive, ModeVPNServer, ModeVPNClient:
	default:
		return fmt.Errorf("unknown mode: %s", req.Mode)
	}
	return nil
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
		if extraArgs := strings.TrimSpace(req.ExtraArgs); extraArgs != "" {
			args = append(args, splitExtraArgs(extraArgs)...)
		}
	case ModeVPNClient:
		linkConfig, err := effectiveLinkConfig(req.LinkConfig)
		if err != nil {
			return nil, err
		}
		args = append(args, "-link", linkConfig)
		if extraArgs := strings.TrimSpace(req.ExtraArgs); extraArgs != "" {
			args = append(args, splitExtraArgs(extraArgs)...)
		}
	}
	return args, nil
}

type vpnClientStart struct {
	session *goncembed.Session
	helper  *vpnhelper.Client
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

	readyCh := make(chan string, 1)
	tunnelLostCh := make(chan string, 8)
	cb.onReady = func(endpoint string) {
		if !strings.HasPrefix(endpoint, "socks5://") {
			return
		}
		select {
		case readyCh <- endpoint:
		default:
		}
	}
	cb.onP2PStatus = func(status string) {
		if !vpnTunnelNeedsRoutePause(status) {
			return
		}
		select {
		case tunnelLostCh <- strings.TrimSpace(status):
		default:
		}
	}
	session, err := goncembed.StartP2PTunnel(req.Password, req.UseUDP, linkConfig, req.ExtraArgs, cb)
	if err != nil {
		if helper != nil {
			_ = helper.Close()
		}
		return nil, err
	}
	go func() {
		var vpnApplied bool
		for {
			select {
			case endpoint := <-readyCh:
				if req.TunnelOnly {
					emit(sink, "status", "info", "SOCKS5 tunnel is ready")
					continue
				}
				effectiveIPv6 := resolveEffectiveIPv6(parent, req.EnableIPv6, endpoint, sink)
				config := vpnconfig.Config{
					SOCKS5Endpoint: endpoint,
					Routes:         routeLines(req.RouteCIDRs, effectiveIPv6),
					DNSServers:     dnsLines(req.DNSServers, effectiveIPv6),
					EnableIPv6:     effectiveIPv6,
					MTU:            1400,
					LogLevel:       "warn",
				}
				if vpnApplied {
					emit(sink, "status", "info", "SOCKS5 endpoint refreshed; reapplying Windows VPN")
					_ = helper.Stop()
				} else {
					emit(sink, "status", "info", "SOCKS5 endpoint ready; starting Windows VPN")
				}
				if err := helper.Start(config); err != nil {
					emit(sink, "status", "error", "Windows VPN start failed: "+err.Error())
					session.Stop()
					return
				}
				vpnApplied = true
				emit(sink, "status", "info", "Windows VPN started")
			case status := <-tunnelLostCh:
				if req.TunnelOnly || !vpnApplied {
					continue
				}
				emit(sink, "status", "warn", "Tunnel status changed to "+status+"; pausing Windows VPN routes for reconnect")
				if err := helper.Stop(); err != nil {
					emit(sink, "status", "warn", "Windows VPN pause failed: "+err.Error())
				} else {
					emit(sink, "status", "info", "Windows VPN routes paused; waiting for SOCKS5 tunnel to be ready again")
				}
				if req.EnableIPv6 {
					emitPeerIPv6(sink, "waiting")
				}
				vpnApplied = false
			case <-session.Done():
				return
			case <-parent.Done():
				return
			}
		}
	}()
	return &vpnClientStart{session: session, helper: helper}, nil
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return fmt.Sprintf("x://127.0.0.1:%d", port), nil
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
	mode        Mode
	sink        Sink
	reportSink  ReportSink
	onReady     func(string)
	onP2PStatus func(string)
	mu          sync.Mutex
	exitCode    int
}

func (c *callback) Event(level string, message string) {
	flushLine(message, level, c.sink)
}

func (c *callback) P2PReport(topic string, side string, status string, network string, mode string, peer string, timestamp int64, pid int) {
	if c.onP2PStatus != nil {
		c.onP2PStatus(status)
	}
	if c.reportSink == nil {
		return
	}
	c.reportSink(P2PStatusReport{
		Topic:     topic,
		Status:    status,
		Network:   network,
		Mode:      mode,
		Side:      string(c.mode),
		Peer:      peer,
		Timestamp: timestamp,
		PID:       pid,
	})
}

func (c *callback) Traffic(side string, inBytes int64, outBytes int64, inBps float64, outBps float64, elapsed int64, connCount int, final bool) {
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
	if endpoint == "" {
		return
	}
	if c.onReady != nil {
		c.onReady(endpoint)
	}
	eventType := "local_http"
	message := "local HTTP endpoint is ready"
	if c.mode == ModeVPNClient {
		eventType = "socks5"
		message = "SOCKS5 endpoint is ready"
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
		emit(c.sink, "status", "error", fmt.Sprintf("gonc exited with code %d", exitCode))
	}
}

func (c *callback) Error(message string) {
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
	next, err := httpfileshare.NewOSFileSource(paths)
	if err != nil {
		return err
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
