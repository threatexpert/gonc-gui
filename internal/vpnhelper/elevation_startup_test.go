package vpnhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestElevationProcessChild(t *testing.T) {
	switch os.Getenv("GONC_TEST_ELEVATION_CHILD") {
	case "cancel":
		fmt.Fprintln(os.Stderr, "execution error: 用户已取消。 (-128)")
		os.Exit(1)
	case "failure":
		fmt.Fprintln(os.Stderr, "helper failed --token secret-test-token")
		os.Exit(1)
	case "exit":
		os.Exit(0)
	}
}

func testElevationListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func TestElevationCancellationAndFailureReturnBeforeTimeout(t *testing.T) {
	for _, mode := range []string{"cancel", "failure", "exit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestElevationProcessChild$")
			cmd.Env = append(os.Environ(), "GONC_TEST_ELEVATION_CHILD="+mode)
			exited, err := startElevationProcess(cmd, true)
			if err != nil {
				t.Fatal(err)
			}
			ln := testElevationListener(t)
			start := time.Now()
			_, err = waitForElevatedHelper(ctx, ln, exited, "token", 10*time.Second)
			if err == nil || strings.Contains(err.Error(), "timed out") {
				t.Fatalf("expected immediate launcher result, got %v", err)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("launcher exit did not promptly finish startup")
			}
			if (mode == "cancel") != errors.Is(err, errAuthorizationCanceled) {
				t.Fatalf("incorrect cancellation classification: %v", err)
			}
			if strings.Contains(err.Error(), "secret-test-token") {
				t.Fatal("launcher stderr leaked a token")
			}
		})
	}
}

func TestElevationProcessStartFailure(t *testing.T) {
	_, err := startElevationProcess(exec.Command("gonc-nonexistent-elevation-command"), true)
	if err == nil {
		t.Fatal("expected launch error")
	}
}

func TestElevationStderrKeepsBoundedTail(t *testing.T) {
	var b elevationStderr
	for _, chunk := range []string{strings.Repeat("x", 10000), strings.Repeat("y", 4090), "cancelled (-128)\n"} {
		if n, err := b.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("write: %d, %v", n, err)
		}
		if b.Len() > 4096 {
			t.Fatal("stderr exceeded buffer limit")
		}
	}
	if !strings.HasSuffix(b.String(), "cancelled (-128)\n") {
		t.Fatal("lost the error code at the end of stderr")
	}
	if _, err := io.Copy(&b, strings.NewReader(strings.Repeat("z", 10000))); err != nil || b.Len() > 4096 {
		t.Fatalf("io.Copy bypassed the limit: length=%d, err=%v", b.Len(), err)
	}
}

func TestElevationSuccessDoesNotWaitForLauncherExit(t *testing.T) {
	ln := testElevationListener(t)
	clientDone := make(chan error, 1)
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			clientDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		enc, dec := json.NewEncoder(conn), json.NewDecoder(conn)
		if err := enc.Encode(request{Op: OpHello, Token: "token"}); err != nil {
			clientDone <- err
			return
		}
		var ack response
		if err := dec.Decode(&ack); err != nil || !ack.OK {
			clientDone <- fmt.Errorf("hello ack: %+v, %v", ack, err)
			return
		}
		// Verify that startup cleanup does not close a successful connection.
		var req request
		if err := dec.Decode(&req); err != nil {
			clientDone <- err
			return
		}
		clientDone <- enc.Encode(response{OK: req.Op == OpStop})
	}()
	peer, err := waitForElevatedHelper(context.Background(), ln, make(chan error), "token", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.conn.Close()
	_ = peer.conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := peer.enc.Encode(request{Op: OpStop}); err != nil {
		t.Fatal(err)
	}
	var resp response
	if err := peer.dec.Decode(&resp); err != nil || !resp.OK {
		t.Fatalf("connection unusable after startup: %+v, %v", resp, err)
	}
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
}

func TestElevationTimeoutAndContextCancellation(t *testing.T) {
	for _, connected := range []bool{false, true} {
		for _, cancelParent := range []bool{false, true} {
			t.Run(fmt.Sprintf("connected=%v/cancel=%v", connected, cancelParent), func(t *testing.T) {
				ln := testElevationListener(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var conn net.Conn
				if connected {
					var err error
					conn, err = net.Dial("tcp", ln.Addr().String())
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
				}
				timeout := 50 * time.Millisecond
				if cancelParent {
					timeout = 5 * time.Second
					timer := time.AfterFunc(50*time.Millisecond, cancel)
					defer timer.Stop()
				}
				_, err := waitForElevatedHelper(ctx, ln, nil, "token", timeout)
				if err == nil {
					t.Fatal("expected startup to be interrupted")
				}
				if cancelParent && !errors.Is(err, context.Canceled) {
					t.Fatalf("expected context cancellation, got %v", err)
				}
				if conn != nil {
					_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
					var b [1]byte
					_, err := conn.Read(b[:])
					if err == nil {
						t.Fatal("abandoned handshake was not closed")
					}
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						t.Fatal("connection leaked after interrupted startup")
					}
				}
			})
		}
	}
}

func TestElevationRejectsWrongToken(t *testing.T) {
	ln := testElevationListener(t)
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{Op: OpHello, Token: "wrong"}); err != nil {
		t.Fatal(err)
	}
	_, err = waitForElevatedHelper(context.Background(), ln, nil, "token", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
}
