package goncrunner

import "testing"

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
