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
	conn         net.Conn
	enc          *json.Encoder
	dec          *json.Decoder
	local        *winvpn.Session
	mu           sync.Mutex
	cancelLaunch context.CancelFunc
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
	// Keep the launcher alive after authorization: osascript waits for the
	// long-lived helper. Only cancel it on startup failure or client shutdown.
	launchCtx, cancelLaunch := context.WithCancel(ctx)
	exited, err := runElevated(launchCtx, exe, args)
	if err != nil {
		cancelLaunch()
		return nil, err
	}
	peer, err := waitForElevatedHelper(ctx, ln, exited, token, 45*time.Second)
	if err != nil {
		cancelLaunch()
		return nil, err
	}
	return &Client{conn: peer.conn, enc: peer.enc, dec: peer.dec, cancelLaunch: cancelLaunch}, nil
}

// Start returns the DNS servers actually configured by the helper.
func (c *Client) Start(config vpnconfig.Config) ([]string, error) {
	if c.conn != nil {
		resp, err := c.call(request{Op: OpStart, Config: config})
		if err != nil {
			return nil, err
		}
		return append([]string{}, resp.DNSServers...), nil
	}
	if c.local != nil {
		_ = c.local.Stop()
		c.local = nil
	}
	session, err := winvpn.Start(config)
	if err != nil {
		return nil, err
	}
	c.local = session
	return session.DNSServers(), nil
}

func (c *Client) Stop() error {
	if c.conn != nil {
		_, err := c.call(request{Op: OpStop})
		return err
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
	if c.cancelLaunch != nil {
		defer c.cancelLaunch()
	}
	if c.conn != nil {
		_, _ = c.call(request{Op: OpExit})
		return c.conn.Close()
	}
	return c.Stop()
}

func (c *Client) call(req request) (response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enc.Encode(req); err != nil {
		return response{}, err
	}
	var resp response
	if err := c.dec.Decode(&resp); err != nil {
		return response{}, err
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "VPN helper command failed"
		}
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func runElevated(ctx context.Context, exe string, args []string) (<-chan error, error) {
	if os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, exe, args...)
		return startElevationProcess(cmd, false)
	}

	switch runtime.GOOS {
	case "linux":
		pkexec, err := exec.LookPath("pkexec")
		if err != nil {
			return nil, fmt.Errorf("system VPN requires administrator permission; install pkexec or run as root: %w", err)
		}
		cmd := exec.CommandContext(ctx, pkexec, append([]string{exe}, args...)...)
		return startElevationProcess(cmd, false)
	case "darwin":
		command := shellQuote(exe)
		for _, arg := range args {
			command += " " + shellQuote(arg)
		}
		script := "do shell script " + appleScriptQuote(command) + " with administrator privileges"
		cmd := exec.CommandContext(ctx, "osascript", "-e", script)
		return startElevationProcess(cmd, true)
	default:
		return nil, fmt.Errorf("system VPN helper is not supported on %s", runtime.GOOS)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func appleScriptQuote(value string) string {
	return strconv.Quote(strings.ReplaceAll(value, "\n", " "))
}
