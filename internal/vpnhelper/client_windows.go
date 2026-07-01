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

func (c *Client) Start(config vpnconfig.Config) ([]string, error) {
	resp, err := c.call(request{Op: OpStart, Config: config})
	if err != nil {
		return resp.Logs, err
	}
	return resp.Logs, nil
}

func (c *Client) Stop() error {
	_, err := c.call(request{Op: OpStop})
	return err
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	_, _ = c.call(request{Op: OpExit})
	return c.conn.Close()
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
