package ipv6probe

import "testing"

func TestParseSOCKS5Endpoint(t *testing.T) {
	tests := []struct {
		in       string
		wantHost string
		wantPort string
	}{
		{in: "socks5://127.0.0.1:1080", wantHost: "127.0.0.1", wantPort: "1080"},
		{in: "socks5://[::1]:1080", wantHost: "::1", wantPort: "1080"},
		{in: "127.0.0.1:1080", wantHost: "127.0.0.1", wantPort: "1080"},
	}
	for _, tt := range tests {
		got, err := parseSOCKS5Endpoint(tt.in)
		if err != nil {
			t.Fatalf("parseSOCKS5Endpoint(%q): %v", tt.in, err)
		}
		if got.Host != tt.wantHost || got.Port != tt.wantPort {
			t.Fatalf("parseSOCKS5Endpoint(%q) = %#v, want host=%q port=%q", tt.in, got, tt.wantHost, tt.wantPort)
		}
	}
}

func TestDNSQuery(t *testing.T) {
	query := dnsQuery("example.com")
	if len(query) < 12 {
		t.Fatalf("query length = %d, want at least 12", len(query))
	}
	if query[2] != 0x01 || query[5] != 0x01 {
		t.Fatalf("unexpected DNS header flags/qdcount: % x", query[:12])
	}
	qname := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
	offset := 12
	for i, b := range qname {
		if query[offset+i] != b {
			t.Fatalf("qname byte %d = %x, want %x", i, query[offset+i], b)
		}
	}
}
