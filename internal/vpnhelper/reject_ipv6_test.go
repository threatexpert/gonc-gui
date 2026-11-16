//go:build windows || linux || darwin

package vpnhelper

import (
	"encoding/json"
	"gonc-gui/internal/vpnconfig"
	"net"
	"testing"
	"time"
)

func TestStartTransmitsIPv6RejectionPolicy(t *testing.T) {
	for _, reject := range []bool{true, false} {
		c, h := net.Pipe()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_ = h.SetDeadline(time.Now().Add(3 * time.Second))
		client := &Client{conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
		got := make(chan request, 1)
		go func() {
			var req request
			_ = json.NewDecoder(h).Decode(&req)
			got <- req
			_ = json.NewEncoder(h).Encode(response{OK: true})
		}()
		_, err := client.Start(vpnconfig.Config{EnableIPv6: true, RejectIPv6: reject, Routes: []string{"::/0"}})
		c.Close()
		h.Close()
		if err != nil {
			t.Fatal(err)
		}
		req := <-got
		if req.Op != OpStart || req.Config.RejectIPv6 != reject || !req.Config.EnableIPv6 || len(req.Config.Routes) != 1 {
			t.Fatalf("lost policy: %+v", req)
		}
	}
}
