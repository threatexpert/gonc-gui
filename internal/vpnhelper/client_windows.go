//go:build windows

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
	"path/filepath"
	"sync"
	"time"

	"gonc-gui/internal/vpnconfig"

	"golang.org/x/sys/windows"
)

type Client struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
	mu   sync.Mutex
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
	if err := runElevated(exe, args); err != nil {
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

// Start returns the DNS servers actually configured by the helper.
func (c *Client) Start(config vpnconfig.Config) ([]string, error) {
	return c.StartWithTrace(config, nil)
}

func (c *Client) StartWithTrace(config vpnconfig.Config, trace func(string)) ([]string, error) {
	resp, err := c.callWithTrace(request{Op: OpStart, Config: config, Trace: trace != nil}, trace)
	if err != nil {
		return nil, err
	}
	return append([]string{}, resp.DNSServers...), nil
}

func (c *Client) Stop() error {
	_, err := c.call(request{Op: OpStop})
	return err
}

func (c *Client) PrepareBypass(dns, local string, _ bool) (string, error) {
	resp, err := c.call(request{Op: OpPrepare, Address: dns, Local: local})
	return resp.Local, err
}
func (c *Client) RefreshBypass() (string, bool, error) {
	if c.conn != nil {
		resp, err := c.call(request{Op: OpRefresh})
		return resp.Local, resp.RoutesSuspended, err
	}
	return "", false, fmt.Errorf("VPN helper is not connected")
}
func (c *Client) UpdateProxy(endpoint string) error {
	_, err := c.call(request{Op: OpProxy, Address: endpoint})
	return err
}

func (c *Client) UpdateDNS(servers []string) ([]string, error) {
	resp, err := c.call(request{Op: OpDNS, Config: vpnconfig.Config{DNSServers: servers}})
	return append([]string{}, resp.DNSServers...), err
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	if _, err := c.call(request{Op: OpExit}); err != nil {
		return err
	}
	return c.conn.Close()
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

func runElevated(exe string, args []string) error {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	parameters, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return err
	}
	directory, err := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, file, parameters, directory, windows.SW_HIDE); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return fmt.Errorf("administrator permission was cancelled; VPN was not started")
		}
		return fmt.Errorf("start elevated VPN helper failed: %w", err)
	}
	return nil
}
