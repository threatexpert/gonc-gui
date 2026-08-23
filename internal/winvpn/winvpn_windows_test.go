//go:build windows

package winvpn

import (
	"reflect"
	"testing"
)

func TestDirectDNSServers(t *testing.T) {
	ipv4, ipv6 := directDNSServers([]string{
		"8.8.8.8:53",
		"8.8.8.8",
		"[2001:4860:4860::8888]:53",
		"dns.example:5353",
		"0.0.0.0",
	}, true)

	if want := []string{"8.8.8.8"}; !reflect.DeepEqual(ipv4, want) {
		t.Fatalf("ipv4 = %#v, want %#v", ipv4, want)
	}
	if want := []string{"2001:4860:4860::8888"}; !reflect.DeepEqual(ipv6, want) {
		t.Fatalf("ipv6 = %#v, want %#v", ipv6, want)
	}
}

func TestDirectDNSServersFallbacks(t *testing.T) {
	ipv4, ipv6 := directDNSServers([]string{"dns.example:5353"}, false)
	if want := []string{"8.8.8.8"}; !reflect.DeepEqual(ipv4, want) {
		t.Fatalf("ipv4 = %#v, want %#v", ipv4, want)
	}
	if len(ipv6) != 0 {
		t.Fatalf("ipv6 = %#v, want empty", ipv6)
	}

	ipv4, ipv6 = directDNSServers(nil, true)
	if want := []string{"8.8.8.8"}; !reflect.DeepEqual(ipv4, want) {
		t.Fatalf("ipv4 fallback = %#v, want %#v", ipv4, want)
	}
	if want := []string{"2001:4860:4860::8888"}; !reflect.DeepEqual(ipv6, want) {
		t.Fatalf("ipv6 fallback = %#v, want %#v", ipv6, want)
	}
}
