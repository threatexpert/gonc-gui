//go:build windows || linux || darwin

package vpnhelper

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"gonc-gui/internal/vpnconfig"
)

func TestStartTraceStreamsBeforeCompletion(t *testing.T) {
	for _, failure := range []bool{false, true} {
		c, h := net.Pipe()
		t.Cleanup(func() { _ = c.Close(); _ = h.Close() })
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_ = h.SetDeadline(time.Now().Add(3 * time.Second))
		client := &Client{conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
		seen := make(chan string, 2)
		done := make(chan error, 1)
		go func() {
			dec, enc := json.NewDecoder(h), json.NewEncoder(h)
			var req request
			if err := dec.Decode(&req); err != nil {
				done <- err
				return
			}
			if req.Op != OpStart || !req.Trace {
				done <- errors.New("missing trace request")
				return
			}
			for _, message := range []string{"System VPN step: configure DNS completed in 20ms", "System VPN step: total completed in 50ms"} {
				if err := enc.Encode(response{Progress: true, Logs: []string{message}}); err != nil {
					done <- err
					return
				}
				// Do not send final completion until the caller receives the log.
				select {
				case got := <-seen:
					if got != message {
						done <- errors.New("wrong trace")
						return
					}
				case <-time.After(2 * time.Second):
					done <- errors.New("trace was not delivered live")
					return
				}
			}
			final := response{OK: true, DNSServers: []string{"8.8.8.8"}}
			if failure {
				final = response{Error: "route failed"}
			}
			if err := enc.Encode(final); err != nil {
				done <- err
				return
			}
			if err := dec.Decode(&req); err != nil {
				done <- err
				return
			}
			if req.Op != OpStop {
				done <- errors.New("expected stop")
				return
			}
			done <- enc.Encode(response{OK: true})
		}()
		dns, err := client.StartWithTrace(vpnconfig.Config{}, func(message string) { seen <- message })
		if failure {
			if err == nil || err.Error() != "route failed" {
				t.Fatalf("lost final failure: %v", err)
			}
		} else if err != nil || !reflect.DeepEqual(dns, []string{"8.8.8.8"}) {
			t.Fatalf("incorrect final result: %v %v", dns, err)
		}
		if err := client.Stop(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
		_ = h.Close()
	}
}

func TestReadResponseIgnoresProgressWithoutCallback(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(`{"progress":true,"logs":["step"]}
{"ok":true,"dnsServers":["::1"]}`))
	resp, err := readResponse(dec, nil)
	if err != nil || !resp.OK || !reflect.DeepEqual(resp.DNSServers, []string{"::1"}) {
		t.Fatal(resp, err)
	}
}

func TestReadResponseDisconnectIsNotSuccess(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(`{"progress":true,"logs":["step"]}`))
	if _, err := readResponse(dec, nil); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want EOF", err)
	}
}
