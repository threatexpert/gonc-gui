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
	conn           net.Conn
	enc            *json.Encoder
	dec            *json.Decoder
	local          *winvpn.Session
	bypass         *winvpn.BypassSession
	mu             sync.Mutex
	cancelLaunch   context.CancelFunc
	bindingMu      sync.Mutex
	bindAddress    string
	releaseBinding func()
	bindDNS        string
}

func StartElevated(ctx context.Context) (*Client, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	network, address := "tcp", "127.0.0.1:0"
	if runtime.GOOS == "linux" {
		network, address = "unix", "@gonc-control-"+token[:32]
	}
	ln, err := net.Listen(network, address)
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
	if err := verifyElevatedPeer(peer.conn); err != nil {
		peer.conn.Close()
		cancelLaunch()
		return nil, err
	}
	return &Client{conn: peer.conn, enc: peer.enc, dec: peer.dec, cancelLaunch: cancelLaunch}, nil
}

func (c *Client) clearBindingLocked() {
	if c.releaseBinding != nil {
		c.releaseBinding()
		c.releaseBinding = nil
	}
	c.bindAddress = ""
}

// Start returns the DNS servers actually configured by the helper.
func (c *Client) Start(config vpnconfig.Config) ([]string, error) {
	return c.StartWithTrace(config, nil)
}

func (c *Client) StartWithTrace(config vpnconfig.Config, trace func(string)) ([]string, error) {
	if c.conn != nil {
		resp, err := c.callWithTrace(request{Op: OpStart, Config: config, Trace: trace != nil}, trace)
		if err != nil {
			return nil, err
		}
		return append([]string{}, resp.DNSServers...), nil
	}
	if c.local != nil {
		if err := c.local.Stop(); err != nil {
			return nil, err
		}
		c.local = nil
	}
	session, err := winvpn.StartWithTrace(config, trace)
	c.local = session
	if err != nil {
		return nil, err
	}
	return session.DNSServers(), nil
}

func (c *Client) Stop() error {
	if c.conn != nil {
		c.bindingMu.Lock()
		defer c.bindingMu.Unlock()
		_, err := c.call(request{Op: OpStop})
		if err == nil {
			c.clearBindingLocked()
		}
		return err
	}
	var err error
	if c.local != nil {
		err = c.local.Stop()
		if err == nil {
			c.local = nil
		}
	}
	if c.bypass != nil {
		cleanupErr := c.bypass.Close()
		err = errors.Join(err, cleanupErr)
		if cleanupErr == nil {
			c.bypass = nil
		}
	}
	return err
}

func (c *Client) PrepareBypass(dns, local string, blockDNSLeak bool) (string, error) {
	if c.conn != nil {
		c.bindingMu.Lock()
		defer c.bindingMu.Unlock()
		resp, err := c.call(request{Op: OpPrepare, Address: dns, LocalIP: local})
		if err == nil && resp.BindAddress != "" {
			c.bindAddress = resp.BindAddress
			c.bindDNS = ""
			if blockDNSLeak {
				// Mark reconnect queries before the capture rules are installed.
				c.bindDNS = dns
			}
			c.releaseBinding = registerSocketBinding(c, resp.LocalIP)
		}
		return resp.LocalIP, err
	}
	if c.bypass != nil {
		return "", fmt.Errorf("VPN bypass already prepared")
	}
	b, err := winvpn.PrepareBypass(dns, local)
	c.bypass = b
	if err != nil {
		return "", err
	}
	return b.LocalIP, nil
}
func (c *Client) RefreshBypass() (string, uint64, error) {
	if c.conn != nil {
		c.bindingMu.Lock()
		defer c.bindingMu.Unlock()
		resp, err := c.call(request{Op: OpRefresh})
		if err == nil && c.bindAddress != "" {
			prev := c.releaseBinding
			c.releaseBinding = registerSocketBinding(c, resp.LocalIP)
			if prev != nil {
				prev()
			}
		}
		return resp.LocalIP, resp.Revision, err
	}
	if c.bypass == nil {
		return "", 0, fmt.Errorf("VPN bypass is not prepared")
	}
	return c.bypass.Refresh()
}
func (c *Client) UpdateProxy(endpoint string) error {
	if c.conn != nil {
		_, err := c.call(request{Op: OpProxy, Address: endpoint})
		return err
	}
	if c.local == nil {
		return fmt.Errorf("VPN is not running")
	}
	return c.local.UpdateProxy(endpoint)
}

func (c *Client) UpdateDNS(servers []string) ([]string, error) {
	if c.conn != nil {
		resp, err := c.call(request{Op: OpDNS, Config: vpnconfig.Config{DNSServers: servers}})
		return append([]string{}, resp.DNSServers...), err
	}
	if c.local == nil {
		return nil, fmt.Errorf("VPN is not running")
	}
	if err := c.local.UpdateDNS(servers); err != nil {
		return nil, err
	}
	return c.local.DNSServers(), nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.conn != nil {
		c.bindingMu.Lock()
		defer c.bindingMu.Unlock()
		if _, err := c.call(request{Op: OpExit}); err != nil {
			return err
		}
		c.clearBindingLocked()
		err := c.conn.Close()
		if c.cancelLaunch != nil {
			c.cancelLaunch()
		}
		return err
	}
	return c.Stop()
}

func (c *Client) call(req request) (response, error) {
	return c.callWithTrace(req, nil)
}

func (c *Client) callWithTrace(req request, trace func(string)) (response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enc.Encode(req); err != nil {
		return response{}, err
	}
	return readResponse(c.dec, trace)
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
