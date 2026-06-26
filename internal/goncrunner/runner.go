package goncrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
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
	ReportURL       string
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

type Sink func(Event)

type Runner struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
}

func New() *Runner {
	return &Runner{}
}

func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cmd != nil
}

func (r *Runner) Start(parent context.Context, goncPath string, req Request, sink Sink) error {
	args, err := buildArgs(req)
	if err != nil {
		return err
	}

	r.mu.Lock()
	if r.cmd != nil {
		r.mu.Unlock()
		return errors.New("a transfer task is already running")
	}

	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, goncPath, args...)
	prepareCommand(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		r.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		r.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		r.mu.Unlock()
		return err
	}

	r.cmd = cmd
	r.cancel = cancel
	r.done = make(chan struct{})
	r.mu.Unlock()

	emit(sink, "status", "info", "gonc started: "+goncPath)
	go scan(stdout, "stdout", sink)
	go scan(stderr, "stderr", sink)
	go r.wait(ctx, cmd, sink)
	return nil
}

func (r *Runner) Stop() error {
	_, err := r.StopWait(0)
	return err
}

func (r *Runner) StopWait(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()
	if cancel == nil {
		return true, errors.New("no transfer task is running")
	}
	cancel()
	if timeout <= 0 || done == nil {
		return false, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true, nil
	case <-timer.C:
		return false, nil
	}
}

func (r *Runner) wait(ctx context.Context, cmd *exec.Cmd, sink Sink) {
	err := cmd.Wait()

	r.mu.Lock()
	if r.cmd == cmd {
		r.cmd = nil
		r.cancel = nil
		if r.done != nil {
			close(r.done)
			r.done = nil
		}
	}
	r.mu.Unlock()

	if ctx.Err() != nil {
		emit(sink, "status", "warn", "transfer stopped")
		return
	}
	if err != nil {
		emit(sink, "status", "error", "gonc exited with error: "+err.Error())
		return
	}
	emit(sink, "status", "info", "transfer finished")
}

func buildArgs(req Request) ([]string, error) {
	if req.Password == "" {
		return nil, errors.New("password is required")
	}

	args := []string{"-p2p", req.Password}
	if req.UseUDP {
		args = append(args, "-u")
	}
	if req.ReportURL != "" {
		args = append(args, "-p2p-report-url", req.ReportURL)
	}
	switch req.Mode {
	case ModeSend:
		if len(req.SharePaths) == 0 {
			return nil, errors.New("select at least one file or folder to send")
		}
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
	default:
		return nil, fmt.Errorf("unknown mode: %s", req.Mode)
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

var localHTTPPattern = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)
var trafficPattern = regexp.MustCompile(`IN:\s+.*?\((\d+)\s+bytes\),\s+([^|]+?)/s\s+\|\s+OUT:\s+.*?\((\d+)\s+bytes\),\s+([^|]+?)/s(?:\s+\|\s+\d+)?\s+\|\s+(\d{2}:\d{2}:\d{2})`)

func scan(reader io.Reader, stream string, sink Sink) {
	if sink == nil {
		_, _ = io.Copy(io.Discard, reader)
		return
	}
	level := "info"
	if stream == "stderr" {
		level = "warn"
	}

	buf := make([]byte, 4096)
	var line strings.Builder
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			for _, b := range buf[:n] {
				switch b {
				case '\r', '\n':
					flushLine(line.String(), level, sink)
					line.Reset()
				default:
					line.WriteByte(b)
					if line.Len() > 4*1024*1024 {
						flushLine(line.String(), level, sink)
						line.Reset()
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				emit(sink, "log", "error", err.Error())
			}
			if text := line.String(); text != "" {
				flushLine(text, level, sink)
			}
			return
		}
	}
}

func flushLine(line, level string, sink Sink) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if localURL := localHTTPPattern.FindString(line); localURL != "" {
		event := newEvent("local_http", "info", "local HTTP endpoint is ready")
		event.LocalURL = localURL
		sink(event)
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
