package vpnhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

var errAuthorizationCanceled = errors.New("VPN administrator authorization canceled")

// A launcher can inherit output from a long-running helper. Keep only the tail
// needed to identify the AppleScript error code, not an unbounded session log.
type elevationStderr struct{ buf bytes.Buffer }

func (b *elevationStderr) String() string { return b.buf.String() }
func (b *elevationStderr) Len() int       { return b.buf.Len() }

func (b *elevationStderr) Write(p []byte) (int, error) {
	const limit = 4096
	n := len(p)
	if n >= limit {
		b.buf.Reset()
		p = p[n-limit:]
	} else if excess := b.Len() + n - limit; excess > 0 {
		b.buf.Next(excess)
	}
	_, _ = b.buf.Write(p)
	return n, nil
}

// Report process completion without blocking on it: on macOS the launcher
// remains alive for the whole helper session, not just the authorization UI.
func startElevationProcess(cmd *exec.Cmd, appleScript bool) (<-chan error, error) {
	var stderr elevationStderr
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start elevated VPN helper failed: %w", err)
	}
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			// AppleScript's user-cancelled error code is language independent.
			if appleScript && strings.HasSuffix(strings.TrimSpace(stderr.String()), "(-128)") {
				err = errAuthorizationCanceled
			} else {
				// Do not expose stderr: it can contain the helper's secret token.
				err = fmt.Errorf("elevated VPN helper authorization or launch failed: %w", err)
			}
		}
		exited <- err
	}()
	return exited, nil
}

type elevatedPeer struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
}

// The caller owns the launcher; this function owns the listener and any
// accepted connection until a fully authenticated peer is returned.
func waitForElevatedHelper(ctx context.Context, ln net.Listener, exited <-chan error, token string, timeout time.Duration) (*elevatedPeer, error) {
	defer ln.Close()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		peer *elevatedPeer
		err  error
	}
	ready := make(chan result)
	go func() {
		conn, err := ln.Accept()
		var peer *elevatedPeer
		if err == nil {
			// Cancellation also interrupts a helper that connects but never
			// sends its hello, rather than leaving the UI stuck in startup.
			stopClose := context.AfterFunc(waitCtx, func() { _ = conn.Close() })
			peer = &elevatedPeer{conn: conn, enc: json.NewEncoder(conn), dec: json.NewDecoder(conn)}
			var hello request
			if err = peer.dec.Decode(&hello); err != nil {
				err = fmt.Errorf("read helper hello: %w", err)
			} else if hello.Op != OpHello || hello.Token != token {
				err = errors.New("elevated VPN helper authentication failed")
			} else {
				err = peer.enc.Encode(response{OK: true})
			}
			stopClose()
			if err != nil {
				_ = conn.Close()
			}
		}
		select {
		case ready <- result{peer: peer, err: err}:
		case <-waitCtx.Done():
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()

	select {
	case <-waitCtx.Done():
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("timed out waiting for elevated VPN helper")
	case err := <-exited:
		if err == nil {
			err = errors.New("elevated VPN helper exited before startup completed")
		}
		return nil, err
	case res := <-ready:
		// The handshake and cancellation may become ready together. Never
		// hand a canceled (possibly already closed) connection to the caller.
		if waitCtx.Err() != nil {
			if res.peer != nil {
				_ = res.peer.conn.Close()
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("timed out waiting for elevated VPN helper")
		}
		return res.peer, res.err
	}
}
