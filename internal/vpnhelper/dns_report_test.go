//go:build windows || linux || darwin

package vpnhelper

import (
	"encoding/json"
	"net"
	"reflect"
	"testing"
	"time"

	"gonc-gui/internal/vpnconfig"
)

func TestStartReturnsHelperDNSInsteadOfInputConfiguration(t *testing.T) {
	for _, reported := range [][]string{{"8.8.8.8"}, {"2001:4860:4860::8888"}, {}} {
		clientConn, helperConn := net.Pipe()
		_ = clientConn.SetDeadline(time.Now().Add(3 * time.Second))
		_ = helperConn.SetDeadline(time.Now().Add(3 * time.Second))
		client := &Client{conn: clientConn, enc: json.NewEncoder(clientConn), dec: json.NewDecoder(clientConn)}
		done := make(chan error, 1)
		go func() {
			var req request
			if err := json.NewDecoder(helperConn).Decode(&req); err != nil {
				done <- err
				return
			}
			done <- json.NewEncoder(helperConn).Encode(response{OK: true, DNSServers: reported})
		}()
		got, err := client.Start(vpnconfig.Config{DNSServers: []string{"::1"}})
		_ = clientConn.Close()
		_ = helperConn.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, reported) {
			t.Fatalf("got %v, want %v", got, reported)
		}
	}
}
