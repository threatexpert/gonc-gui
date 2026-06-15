package goncrunner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

type Mode string

const (
	ModeSend    Mode = "send"
	ModeReceive Mode = "receive"
)

type Request struct {
	Mode            Mode
	Password        string
	SharePaths      []string
	SaveDir         string
	DownloadSubPath string
	UseUDP          bool
	ReportURL       string
}

type Event struct {
	Type     string `json:"type"`
	Level    string `json:"level"`
	Message  string `json:"message"`
	Time     string `json:"time"`
	LocalURL string `json:"localUrl,omitempty"`
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
	default:
		return nil, fmt.Errorf("unknown mode: %s", req.Mode)
	}
	return args, nil
}

var localHTTPPattern = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)

func scan(reader io.Reader, stream string, sink Sink) {
	if sink == nil {
		_, _ = io.Copy(io.Discard, reader)
		return
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	level := "info"
	if stream == "stderr" {
		level = "warn"
	}
	for scanner.Scan() {
		line := scanner.Text()
		if localURL := localHTTPPattern.FindString(line); localURL != "" {
			event := newEvent("local_http", "info", "local HTTP endpoint is ready")
			event.LocalURL = localURL
			sink(event)
		}
		emit(sink, "log", level, line)
	}
	if err := scanner.Err(); err != nil {
		emit(sink, "log", "error", err.Error())
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
