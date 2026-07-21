package goncrunner

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestDynamicFileSourceCanBecomeEmptyAndRecover(t *testing.T) {
	file := filepath.Join(t.TempDir(), "one.txt")
	if err := os.WriteFile(file, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := newDynamicFileSource([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.UpdatePaths(nil); err != nil {
		t.Fatal(err)
	}
	entries, err := source.ReadDir("/")
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	info, err := source.Stat("/")
	if err != nil || !info.IsDir() {
		t.Fatalf("root=%v err=%v", info, err)
	}
	if err := source.UpdatePaths([]string{file}); err != nil {
		t.Fatal(err)
	}
	entries, err = source.ReadDir("/")
	if err != nil || len(entries) != 1 {
		t.Fatalf("recovered entries=%v err=%v", entries, err)
	}
}

func TestValidateRequestRejectsEmptyInitialSharePaths(t *testing.T) {
	err := validateRequest(Request{Mode: ModeSend, Password: "pass1234"})
	if err == nil || err.Error() != "select at least one file or folder to send" {
		t.Fatalf("error = %v, want empty initial share error", err)
	}
}

func TestParseTrafficWithConnectionCount(t *testing.T) {
	line := "IN: 74.1 KiB (75891 bytes), 0.0 B/s | OUT: 145.9 MiB (152976138 bytes), 64.0 KiB/s | 2 | 00:07:27"
	event, ok := parseTraffic(line)
	if !ok {
		t.Fatal("parseTraffic returned false")
	}
	if event.Type != "traffic" {
		t.Fatalf("event type = %q, want traffic", event.Type)
	}
	if event.InBytes != 75891 {
		t.Fatalf("in bytes = %d, want 75891", event.InBytes)
	}
	if event.OutBytes != 152976138 {
		t.Fatalf("out bytes = %d, want 152976138", event.OutBytes)
	}
	if event.InBps != 0 {
		t.Fatalf("in bps = %f, want 0", event.InBps)
	}
	if event.OutBps != 64*1024 {
		t.Fatalf("out bps = %f, want %d", event.OutBps, 64*1024)
	}
	if event.Elapsed != "00:07:27" {
		t.Fatalf("elapsed = %q, want 00:07:27", event.Elapsed)
	}
}

func TestParseTrafficWithoutConnectionCount(t *testing.T) {
	line := "IN: 74.1 KiB (75891 bytes), 0.0 B/s | OUT: 145.9 MiB (152976138 bytes), 64.0 KiB/s | 00:07:27"
	if _, ok := parseTraffic(line); !ok {
		t.Fatal("parseTraffic returned false")
	}
}

func TestBuildArgsVPNServer(t *testing.T) {
	args, err := buildArgs(Request{
		Mode:       ModeVPNServer,
		Password:   "pass1234",
		UseUDP:     true,
		Upstream:   "socks5://127.0.0.1:1080",
		DNSForward: "8.8.8.8:53",
		ExtraArgs:  `-plain -x "socks5://with space"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-p2p", "pass1234",
		"-u",
		"-k", "-W", "-P",
		"-e", ":mux linkagent -x socks5://127.0.0.1:1080 -dns 8.8.8.8:53",
		"-plain",
		"-x", "socks5://with space",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestFileTransferP2POptionsAlwaysEnableLAN(t *testing.T) {
	for _, useUDP := range []bool{false, true} {
		options := fileTransferP2POptions(useUDP)
		if !options.P2PWithLANMode {
			t.Fatalf("fileTransferP2POptions(%v) disabled P2P with LAN", useUDP)
		}
		if options.UseUDP != useUDP {
			t.Fatalf("fileTransferP2POptions(%v).UseUDP = %v", useUDP, options.UseUDP)
		}
	}
}

func TestValidateRequestRejectsEmbeddedDaemonMode(t *testing.T) {
	for _, mode := range []Mode{ModeVPNServer, ModeVPNClient} {
		t.Run(string(mode), func(t *testing.T) {
			err := validateRequest(Request{
				Mode:      mode,
				Password:  "pass1234",
				ExtraArgs: `-plain "-daemon"`,
			})
			if err == nil {
				t.Fatal("validateRequest returned nil, want daemon mode error")
			}
			if err.Error() != "daemon mode is not supported when embedded" {
				t.Fatalf("error = %q, want embedded daemon error", err.Error())
			}
		})
	}
}

func TestVPNTunnelNeedsRoutePause(t *testing.T) {
	paused := []string{"wait", "waiting", "connecting", "negotiating", "reconnecting", "disconnected", "failed", "error: timeout", "lost peer"}
	for _, status := range paused {
		if !vpnTunnelNeedsRoutePause(status) {
			t.Fatalf("vpnTunnelNeedsRoutePause(%q) = false, want true", status)
		}
	}
	active := []string{"", "connected", "CONNECTED", "direct"}
	for _, status := range active {
		if vpnTunnelNeedsRoutePause(status) {
			t.Fatalf("vpnTunnelNeedsRoutePause(%q) = true, want false", status)
		}
	}
}

func TestExtractPeerIPs(t *testing.T) {
	tests := []struct {
		name string
		peer string
		want []string
	}{
		{name: "ipv4 hostport", peer: "203.0.113.10:443", want: []string{"203.0.113.10"}},
		{name: "ipv6 hostport", peer: "[2001:db8::10]:443", want: []string{"2001:db8::10"}},
		{name: "plain ipv4", peer: "198.51.100.2", want: []string{"198.51.100.2"}},
		{name: "ignore loopback", peer: "127.0.0.1:1080", want: nil},
		{name: "ignore hostname", peer: "example.com:443", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPeerIPs(tt.peer)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("extractPeerIPs(%q) = %#v, want %#v", tt.peer, got, tt.want)
			}
		})
	}
}

func TestReplaceBypassIPs(t *testing.T) {
	var mu sync.Mutex
	values := map[string]struct{}{"203.0.113.10": {}}
	if replaceBypassIPs(&mu, values, []string{"203.0.113.10"}) {
		t.Fatal("replaceBypassIPs reported change for same set")
	}
	if !replaceBypassIPs(&mu, values, []string{"198.51.100.2"}) {
		t.Fatal("replaceBypassIPs did not report change for new set")
	}
	if _, ok := values["203.0.113.10"]; ok {
		t.Fatal("old bypass IP was not removed")
	}
	if _, ok := values["198.51.100.2"]; !ok {
		t.Fatal("new bypass IP was not added")
	}
}
