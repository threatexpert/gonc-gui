package goncrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/threatexpert/gonc/v2/goncembed"
	"github.com/threatexpert/gonc/v2/httpfileshare"
)

type Mode string

const (
	ModeSend      Mode = "send"
	ModeReceive   Mode = "receive"
	ModeVPNServer Mode = "vpnServer"
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
		session *goncembed.Session
		source  *dynamicFileSource
		err     error
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
		return errors.New("a transfer task is already running")
	}
	r.session = session
	r.source = source
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
	if session != nil {
		r.stopping = true
	}
	r.mu.Unlock()
	if session == nil {
		return true, errors.New("no transfer task is running")
	}
	session.Stop()
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
	case ModeReceive, ModeVPNServer:
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
	}
	return args, nil
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
	mode       Mode
	sink       Sink
	reportSink ReportSink
	mu         sync.Mutex
	exitCode   int
}

func (c *callback) Event(level string, message string) {
	flushLine(message, level, c.sink)
}

func (c *callback) P2PReport(topic string, side string, status string, network string, mode string, peer string, timestamp int64, pid int) {
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

func (c *callback) Ready(endpoint string) {
	if endpoint == "" {
		return
	}
	event := newEvent("local_http", "info", "local HTTP endpoint is ready")
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

func newEvent(typ, level, message string) Event {
	return Event{
		Type:    typ,
		Level:   level,
		Message: message,
		Time:    time.Now().Format(time.RFC3339),
	}
}
