//go:build windows

package winvpn

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsDNSSettings(t *testing.T) {
	for _, tc := range []struct {
		family  string
		servers []string
		flags   uint64
		text    string
	}{
		{"ipv4", []string{dnsProxyIPv4}, 2, dnsProxyIPv4},
		{"ipv6", nil, 3, ""},
		{"ipv6", []string{"2001:db8::1", "2001:db8::2"}, 3, "2001:db8::1,2001:db8::2"},
	} {
		s, err := makeWindowsDNSSettings(tc.family, tc.servers)
		if err != nil {
			t.Fatal(err)
		}
		if s.Version != 1 || s.Flags != tc.flags || s.NameServer == nil || windows.UTF16PtrToString(s.NameServer) != tc.text {
			t.Fatalf("invalid settings: %+v", s)
		}
		if s.Domain != nil || s.SearchList != nil || s.ProfileNameServer != nil || s.RegistrationEnabled != 0 || s.RegisterAdapterName != 0 || s.EnableLLMNR != 0 || s.QueryAdapterName != 0 {
			t.Fatal("unrelated DNS settings touched")
		}
	}
	for _, tc := range []struct{ family, address string }{{"ipv4", "::1"}, {"ipv6", "8.8.8.8"}, {"ipv4", "bad"}, {"bad", "8.8.8.8"}} {
		if _, err := makeWindowsDNSSettings(tc.family, []string{tc.address}); err == nil {
			t.Fatal("invalid DNS accepted", tc)
		}
	}
	if unsafe.Sizeof(uintptr(0)) == 8 && (unsafe.Sizeof(windowsDNSSettings{}) != 64 || unsafe.Offsetof(windowsDNSSettings{}.NameServer) != 24) {
		t.Fatal("wrong Windows 64-bit DNS ABI layout")
	}
}

func mockDNSBackend(t *testing.T) windowsDNSBackend {
	return windowsDNSBackend{
		available: func() error { return nil },
		lookup: func(name string) (uint64, error) {
			if name != interfaceName {
				t.Fatal(name)
			}
			return 1234, nil
		},
		set:      func(uint64, string, []string) error { t.Fatal("unexpected native write"); return nil },
		fallback: func(string, []string) error { t.Fatal("unexpected fallback"); return nil },
		read: func(luid uint64) ([]string, error) {
			if luid != 1234 {
				t.Fatal(luid)
			}
			return []string{dnsProxyIPv4}, nil
		},
	}
}

func TestWindowsDNSNativeAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		api := mockDNSBackend(t)
		var calls []string
		apply := func(family string, servers []string) error {
			calls = append(calls, family+":"+strings.Join(servers, ","))
			return nil
		}
		if legacy {
			api.available = func() error { return fmt.Errorf("old Windows: %w", windows.ERROR_PROC_NOT_FOUND) }
			api.fallback = apply
		} else {
			api.set = func(luid uint64, family string, servers []string) error {
				if luid != 1234 {
					t.Fatal(luid)
				}
				return apply(family, servers)
			}
		}
		if err := configureWindowsDNSWith([]string{dnsProxyIPv4}, nil, api); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(calls, []string{"ipv4:" + dnsProxyIPv4, "ipv6:"}) {
			t.Fatal(calls)
		}
	}
}

func TestWindowsDNSFailuresDoNotFallback(t *testing.T) {
	for _, phase := range []string{"load", "lookup", "ipv4", "ipv6", "read"} {
		t.Run(phase, func(t *testing.T) {
			api := mockDNSBackend(t)
			failure := windows.ERROR_ACCESS_DENIED
			var writes []string
			api.set = func(_ uint64, family string, _ []string) error {
				writes = append(writes, family)
				if phase == family {
					return failure
				}
				return nil
			}
			switch phase {
			case "load":
				api.available = func() error { return failure }
			case "lookup":
				api.lookup = func(string) (uint64, error) { return 0, failure }
			case "read":
				api.read = func(uint64) ([]string, error) { return nil, failure }
			}
			if err := configureWindowsDNSWith([]string{dnsProxyIPv4}, nil, api); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if (phase == "load" || phase == "lookup") && len(writes) != 0 {
				t.Fatal("write after failure")
			}
			if phase == "ipv4" && len(writes) != 1 {
				t.Fatal("must stop on first family failure")
			}
		})
	}
	for _, failure := range []error{windows.ERROR_INVALID_PARAMETER, windows.ERROR_NOT_SUPPORTED, windows.ERROR_PROC_NOT_FOUND} {
		api := mockDNSBackend(t)
		api.set = func(uint64, string, []string) error { return failure }
		if err := configureWindowsDNSWith([]string{dnsProxyIPv4}, nil, api); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
}

func TestWindowsDNSLegacyFailure(t *testing.T) {
	api := mockDNSBackend(t)
	api.available = func() error { return windows.ERROR_PROC_NOT_FOUND }
	api.fallback = func(string, []string) error { return windows.ERROR_ACCESS_DENIED }
	if err := configureWindowsDNSWith([]string{dnsProxyIPv4}, nil, api); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal(err)
	}
}

func TestWindowsDNSVerification(t *testing.T) {
	api := mockDNSBackend(t)
	api.set = func(uint64, string, []string) error { return nil }
	reads := 0
	api.read = func(uint64) ([]string, error) {
		reads++
		if reads == 1 {
			return []string{"8.8.8.8"}, nil
		}
		return []string{dnsProxyIPv4}, nil
	}
	if err := configureWindowsDNSWith([]string{dnsProxyIPv4}, nil, api); err != nil || reads != 2 {
		t.Fatal(err, reads)
	}
	api.read = func(uint64) ([]string, error) { return []string{dnsProxyIPv4, "2001:db8::53"}, nil }
	if err := configureWindowsDNSWith([]string{dnsProxyIPv4}, nil, api); err == nil {
		t.Fatal("uncleared IPv6 DNS accepted")
	}
	if !equalDNSServers([]string{"2001:0db8::1", "8.8.8.8"}, []string{"8.8.8.8", "2001:db8::1"}) {
		t.Fatal("equivalent lists mismatched")
	}
}

func TestWindowsDNSNetshCommands(t *testing.T) {
	var calls [][]string
	run := func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}
	if err := configureWindowsDNSServersNetshWith("ipv4", []string{"8.8.8.8", "1.1.1.1"}, run); err != nil {
		t.Fatal(err)
	}
	if err := configureWindowsDNSServersNetshWith("ipv6", nil, run); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"netsh", "interface", "ipv4", "set", "dnsservers", "name=Gonc", "static", "8.8.8.8", "primary", "validate=no"},
		{"netsh", "interface", "ipv4", "add", "dnsservers", "name=Gonc", "address=1.1.1.1", "index=2", "validate=no"},
		{"netsh", "interface", "ipv6", "delete", "dnsservers", "name=Gonc", "all"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatal(calls)
	}
	failure := errors.New("netsh failed")
	count := 0
	err := configureWindowsDNSServersNetshWith("ipv4", []string{"8.8.8.8", "1.1.1.1"}, func(string, ...string) error { count++; return failure })
	if !errors.Is(err, failure) || count != 1 {
		t.Fatal(err, count)
	}
}

func TestWindowsDNSReadOnlyTiming(t *testing.T) {
	step := time.Now()
	err := setInterfaceDNS.Find()
	t.Logf("DNS API lookup: %s (%v)", time.Since(step), err)
	if err != nil && !errors.Is(err, windows.ERROR_PROC_NOT_FOUND) {
		t.Fatal(err)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		for pass := 0; pass < 3; pass++ {
			step = time.Now()
			luid, err := windowsInterfaceLUID(iface.Name)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("pass %d interface lookup: %s", pass, time.Since(step))
			step = time.Now()
			if _, err := readWindowsDNSServers(luid); err != nil {
				t.Fatal(err)
			}
			t.Logf("pass %d DNS readback: %s", pass, time.Since(step))
		}
		return
	}
	t.Fatal("no loopback interface")
}
