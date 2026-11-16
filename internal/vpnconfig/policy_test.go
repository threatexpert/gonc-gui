package vpnconfig

import (
	"reflect"
	"testing"
)

func TestRoutesDetermineIPv6(t *testing.T) {
	for _, tc := range []struct {
		input string
		v6    bool
		count int
	}{
		{"", true, 3}, {"0.0.0.0/0", false, 1}, {"::/0", true, 1},
		{"2001:db8::1234/64\n10.1.2.3/8\n10.0.0.0/8", true, 2},
	} {
		routes, err := ParseRoutes(tc.input)
		if err != nil || HasIPv6(routes) != tc.v6 || len(routes) != tc.count {
			t.Fatalf("%q: %v %v", tc.input, routes, err)
		}
	}
	for _, value := range []string{"::/129", "10.0.0.0/33", "hostname/24", "::ffff:192.0.2.1/128"} {
		if _, err := ParseRoutes(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestDesktopDefaultRoutesNeverReplaceNativeDefaults(t *testing.T) {
	got := DesktopRoutes([]string{"::/0", "::/1", "0.0.0.0/0", "2001:db8::/32"})
	want := []string{"::/1", "8000::/1", "0.0.0.0/1", "128.0.0.0/1", "2001:db8::/32"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestDNSFilteringAndRecovery(t *testing.T) {
	configured := "2001:4860:4860::8888"
	got, fallback, err := SelectDNS(configured, false)
	if err != nil || !fallback || !reflect.DeepEqual(got, []string{"8.8.8.8"}) {
		t.Fatal(got, fallback, err)
	}
	got, fallback, err = SelectDNS(configured, true)
	if err != nil || fallback || !reflect.DeepEqual(got, []string{configured}) {
		t.Fatal(got, fallback, err)
	}
	got, fallback, err = SelectDNS("9.9.9.9:53\n[2001:4860:4860::8888]:53", false)
	if err != nil || fallback || !reflect.DeepEqual(got, []string{"9.9.9.9:53"}) {
		t.Fatal(got, fallback, err)
	}
	if _, _, err := SelectDNS("not-an-ip", false); err == nil {
		t.Fatal("invalid DNS accepted")
	}
}
