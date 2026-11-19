//go:build windows || linux || darwin

package vpnhelper

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestProxyUpdateUsesDedicatedHelperOperation(t *testing.T) {
	c, h := net.Pipe()
	defer c.Close()
	defer h.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	_ = h.SetDeadline(time.Now().Add(time.Second))
	client := &Client{conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
	received := make(chan request, 1)
	decoded := make(chan error, 1)
	go func() {
		var r request
		line, err := bufio.NewReader(h).ReadBytes('\n')
		if err == nil {
			err = json.Unmarshal(line, &r)
		}
		decoded <- err
		received <- r
		_ = json.NewEncoder(h).Encode(response{OK: true})
	}()
	if err := client.UpdateProxy("socks5://127.0.0.1:2000"); err != nil {
		t.Fatalf("call=%v decode=%v request=%+v", err, <-decoded, <-received)
	}
	r := <-received
	if r.Op != OpProxy || r.Address != "socks5://127.0.0.1:2000" {
		t.Fatal(r)
	}
}

func TestPrepareBypassReturnsSelectedSource(t *testing.T) {
	c, h := net.Pipe()
	defer c.Close()
	defer h.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	_ = h.SetDeadline(time.Now().Add(time.Second))
	client := &Client{conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
	received := make(chan request, 1)
	go func() {
		var r request
		line, _ := bufio.NewReader(h).ReadBytes('\n')
		_ = json.Unmarshal(line, &r)
		received <- r
		_ = json.NewEncoder(h).Encode(response{OK: true, LocalIP: "192.168.50.2"})
	}()
	got, err := client.PrepareBypass("8.8.8.8:53", "")
	if err != nil || got != "192.168.50.2" {
		t.Fatalf("%s %v", got, err)
	}
	r := <-received
	if r.Op != OpPrepare || r.Address != "8.8.8.8:53" {
		t.Fatal(r)
	}
}

func TestFailedExitKeepsHelperConnectionForCleanupRetry(t *testing.T) {
	c, h := net.Pipe()
	defer c.Close()
	defer h.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	_ = h.SetDeadline(time.Now().Add(2 * time.Second))
	client := &Client{conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
	done := make(chan []string, 1)
	go func() {
		reader := bufio.NewReader(h)
		var ops []string
		for i := 0; i < 3; i++ {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				break
			}
			var req request
			_ = json.Unmarshal(line, &req)
			ops = append(ops, req.Op)
			resp := response{OK: true}
			if i == 0 {
				resp = response{Error: "route delete failed"}
			}
			if err := json.NewEncoder(h).Encode(resp); err != nil {
				break
			}
		}
		done <- ops
	}()
	if err := client.Close(); err == nil {
		t.Fatal("exit hid cleanup error")
	}
	if err := client.Stop(); err != nil {
		t.Fatal("IPC lost after failed exit", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	ops := <-done
	if len(ops) != 3 || ops[0] != OpExit || ops[1] != OpStop || ops[2] != OpExit {
		t.Fatal(ops)
	}
}
