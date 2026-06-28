//go:build linux || darwin

package vpnhelper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gonc-gui/internal/vpnconfig"
	"gonc-gui/internal/winvpn"
)

type Client struct {
	conn  net.Conn
	enc   *json.Encoder
	dec   *json.Decoder
	local *winvpn.Session
	mu    sync.Mutex
}

func StartElevated(ctx context.Context) (*Client, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exe, _ = filepath.Abs(exe)
	args := []string{"--vpn-helper", "--connect", ln.Addr().String(), "--token", token}
	if err := runElevated(ctx, exe, args); err != nil {
		return nil, err
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		acceptCh <- acceptResult{conn: conn, err: err}
	}()

	timeout := time.NewTimer(45 * time.Second)
	defer timeout.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timeout.C:
		return nil, fmt.Errorf("timed out waiting for elevated VPN helper")
	case result := <-acceptCh:
		if result.err != nil {
			return nil, result.err
		}
		dec := json.NewDecoder(result.conn)
		enc := json.NewEncoder(result.conn)
		var hello request
		if err := dec.Decode(&hello); err != nil {
			result.conn.Close()
			return nil, fmt.Errorf("read helper hello: %w", err)
		}
		if hello.Op != OpHello || hello.Token != token {
			result.conn.Close()
			return nil, fmt.Errorf("elevated VPN helper authentication failed")
		}
		if err := enc.Encode(response{OK: true}); err != nil {
			result.conn.Close()
			return nil, err
		}
		return &Client{conn: result.conn, enc: enc, dec: dec}, nil
	}
}

func (c *Client) Start(config vpnconfig.Config) error {
	if c.conn != nil {
		return c.call(request{Op: OpStart, Config: config})
	}
	if c.local != nil {
		_ = c.local.Stop()
		c.local = nil
	}
	session, err := winvpn.Start(config)
	if err != nil {
		return err
	}
	c.local = session
	return nil
}

func (c *Client) Stop() error {
	if c.conn != nil {
		return c.call(request{Op: OpStop})
	}
	if c.local != nil {
		err := c.local.Stop()
		c.local = nil
		return err
	}
	return nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.conn != nil {
		_ = c.call(request{Op: OpExit})
		return c.conn.Close()
	}
	return c.Stop()
}

func (c *Client) call(req request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enc.Encode(req); err != nil {
		return err
	}
	var resp response
	if err := c.dec.Decode(&resp); err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "VPN helper command failed"
		}
		return errors.New(resp.Error)
	}
	return nil
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func runElevated(ctx context.Context, exe string, args []string) error {
	if os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, exe, args...)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start VPN helper failed: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}

	switch runtime.GOOS {
	case "linux":
		pkexec, err := exec.LookPath("pkexec")
		if err != nil {
			return fmt.Errorf("system VPN requires administrator permission; install pkexec or run as root: %w", err)
		}
		cmd := exec.CommandContext(ctx, pkexec, append([]string{exe}, args...)...)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start elevated VPN helper with pkexec failed: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	case "darwin":
		command := shellQuote(exe)
		for _, arg := range args {
			command += " " + shellQuote(arg)
		}
		script := "do shell script " + appleScriptQuote(command) + " with administrator privileges"
		cmd := exec.CommandContext(ctx, "osascript", "-e", script)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start elevated VPN helper with macOS authorization failed: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	default:
		return fmt.Errorf("system VPN helper is not supported on %s", runtime.GOOS)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func appleScriptQuote(value string) string {
	return strconv.Quote(strings.ReplaceAll(value, "\n", " "))
}
