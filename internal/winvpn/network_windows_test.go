//go:build windows

package winvpn

import (
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
	"gonc-gui/internal/vpnconfig"
)

func addressIP(row windows.MibUnicastIpAddressRow) netip.Addr {
	if row.Address.Family == windows.AF_INET {
		return netip.AddrFrom4((*windows.RawSockaddrInet4)(unsafe.Pointer(&row.Address)).Addr)
	}
	return netip.AddrFrom16(row.Address.Addr)
}

func mockNetworkSession(t *testing.T) *Session {
	t.Helper()
	fail := func() { t.Helper(); t.Fatal("unexpected network mutation") }
	return &Session{config: vpnconfig.Config{RouteMetric: 7}, networkAPI: &windowsNetworkAPI{
		lookupLUID: func(name string) (uint64, error) {
			if name != interfaceName {
				t.Fatalf("unexpected alias %s", name)
			}
			return 1234, nil
		},
		addresses:     func() ([]windows.MibUnicastIpAddressRow, error) { return nil, nil },
		createAddress: func(*windows.MibUnicastIpAddressRow) error { fail(); return nil },
		deleteAddress: func(*windows.MibUnicastIpAddressRow) error { fail(); return nil },
		getInterface:  func(*windows.MibIpInterfaceRow) error { fail(); return nil },
		setInterface:  func(*windows.MibIpInterfaceRow) error { fail(); return nil },
		createRoute:   func(*windows.MibIpForwardRow2) error { fail(); return nil },
		deleteRoute:   func(*windows.MibIpForwardRow2) error { fail(); return nil },
	}}
}

func TestWindowsAddressRows(t *testing.T) {
	for _, cidr := range []string{"10.60.173.33/24", "fd60:173:33::2/128"} {
		row, err := newWindowsAddress(1234, cidr)
		if err != nil {
			t.Fatal(err)
		}
		p := netip.MustParsePrefix(cidr)
		if addressIP(row) != p.Addr() || int(row.OnLinkPrefixLength) != p.Bits() {
			t.Fatalf("wrong address: %+v", row)
		}
		if row.InterfaceLuid != 1234 || row.PrefixOrigin != 1 || row.SuffixOrigin != 1 || row.SkipAsSource != 0 || row.DadState != 4 || row.ValidLifetime != ^uint32(0) || row.PreferredLifetime != ^uint32(0) {
			t.Fatalf("wrong address policy: %+v", row)
		}
	}
	if _, err := newWindowsAddress(1234, "broken"); err == nil {
		t.Fatal("invalid address accepted")
	}
}

func TestWindowsIPv4Replacement(t *testing.T) {
	s := mockNetworkSession(t)
	old, _ := newWindowsAddress(1234, "10.60.173.99/24")
	foreign, _ := newWindowsAddress(999, "192.0.2.1/24")
	v6, _ := newWindowsAddress(1234, "fe80::1/64")
	s.networkAPI.addresses = func() ([]windows.MibUnicastIpAddressRow, error) {
		return []windows.MibUnicastIpAddressRow{old, foreign, v6}, nil
	}
	var calls []string
	s.networkAPI.deleteAddress = func(row *windows.MibUnicastIpAddressRow) error {
		if row.InterfaceLuid != 1234 {
			t.Fatal("deleting another adapter's address")
		}
		calls = append(calls, "delete:"+addressIP(*row).String())
		return nil
	}
	s.networkAPI.createAddress = func(row *windows.MibUnicastIpAddressRow) error {
		calls = append(calls, "create:"+addressIP(*row).String())
		return nil
	}
	if err := s.configureIPv4Address(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"delete:10.60.173.99", "create:10.60.173.33"}) {
		t.Fatal(calls)
	}
}

func TestWindowsIPv4Failures(t *testing.T) {
	for _, phase := range []string{"lookup", "enumerate", "delete", "create"} {
		t.Run(phase, func(t *testing.T) {
			s := mockNetworkSession(t)
			failure := errors.New(phase)
			old, _ := newWindowsAddress(1234, "10.60.173.99/24")
			s.networkAPI.addresses = func() ([]windows.MibUnicastIpAddressRow, error) { return []windows.MibUnicastIpAddressRow{old}, nil }
			s.networkAPI.deleteAddress = func(*windows.MibUnicastIpAddressRow) error { return nil }
			switch phase {
			case "lookup":
				s.networkAPI.lookupLUID = func(string) (uint64, error) { return 0, failure }
			case "enumerate":
				s.networkAPI.addresses = func() ([]windows.MibUnicastIpAddressRow, error) { return nil, failure }
			case "delete":
				s.networkAPI.deleteAddress = func(*windows.MibUnicastIpAddressRow) error { return failure }
			case "create":
				s.networkAPI.createAddress = func(*windows.MibUnicastIpAddressRow) error { return failure }
			}
			if err := s.configureIPv4Address(); !errors.Is(err, failure) {
				t.Fatalf("lost failure: %v", err)
			}
		})
	}
}

func TestWindowsIPv6CleanupAndCreate(t *testing.T) {
	s := mockNetworkSession(t)
	var calls []string
	s.networkAPI.deleteAddress = func(row *windows.MibUnicastIpAddressRow) error {
		calls = append(calls, "delete:"+addressIP(*row).String())
		return windows.ERROR_NOT_FOUND // Previous implementation ignored cleanup errors.
	}
	failure := windows.ERROR_ACCESS_DENIED
	s.networkAPI.createAddress = func(row *windows.MibUnicastIpAddressRow) error {
		calls = append(calls, "create:"+addressIP(*row).String())
		return failure
	}
	if err := s.configureIPv6Address(); !errors.Is(err, failure) {
		t.Fatalf("lost create error: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"delete:fd00::2", "delete:fd60:173:33::2", "create:fd60:173:33::2"}) {
		t.Fatal(calls)
	}
}

func TestWindowsInterfaceMetrics(t *testing.T) {
	for _, v6 := range []bool{false, true} {
		s := mockNetworkSession(t)
		s.config.EnableIPv6 = v6
		var families []uint16
		s.networkAPI.getInterface = func(row *windows.MibIpInterfaceRow) error {
			if row.InterfaceLuid != 1234 {
				t.Fatal("wrong interface")
			}
			row.UseAutomaticMetric = 1
			row.NlMtu, row.DadTransmits, row.SitePrefixLength = 1400, 1, 64
			row.ForwardingEnabled, row.WeakHostReceive = 1, 1
			return nil
		}
		s.networkAPI.setInterface = func(row *windows.MibIpInterfaceRow) error {
			families = append(families, row.Family)
			if row.UseAutomaticMetric != 0 || row.Metric != 7 || row.NlMtu != 1400 || row.DadTransmits != 1 || row.ForwardingEnabled != 1 || row.WeakHostReceive != 1 {
				t.Fatalf("other settings clobbered: %+v", row)
			}
			if row.Family == windows.AF_INET && row.SitePrefixLength != 0 {
				t.Fatal("IPv4 site prefix must be zero")
			}
			if row.Family == windows.AF_INET6 && row.SitePrefixLength != 64 {
				t.Fatal("IPv6 site prefix changed")
			}
			return nil
		}
		if err := s.configureInterfaceMetric(); err != nil {
			t.Fatal(err)
		}
		want := []uint16{windows.AF_INET}
		if v6 {
			want = append(want, windows.AF_INET6)
		}
		if !reflect.DeepEqual(families, want) {
			t.Fatal(families)
		}
	}
}

func TestWindowsMetricFailures(t *testing.T) {
	for _, failGet := range []bool{true, false} {
		s := mockNetworkSession(t)
		s.config.EnableIPv6 = true
		failure := errors.New("metric failed")
		s.networkAPI.getInterface = func(*windows.MibIpInterfaceRow) error {
			if failGet {
				return failure
			}
			return nil
		}
		s.networkAPI.setInterface = func(*windows.MibIpInterfaceRow) error {
			if failGet {
				t.Fatal("set after failed get")
			}
			return failure
		}
		if err := s.configureInterfaceMetric(); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
}

func TestWindowsRouteRows(t *testing.T) {
	for _, cidr := range []string{"0.0.0.0/0", "128.0.0.0/1", "10.2.3.4/16", "::/0", "2001:db8::123/64"} {
		row, err := newWindowsRoute(1234, cidr, 7)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := windowsRoutePrefix(row.DestinationPrefix)
		if !ok || got != netip.MustParsePrefix(cidr).Masked() {
			t.Fatalf("bad prefix %v", got)
		}
		nextHop, ok := windowsRoutePrefix(windows.IpAddressPrefix{Prefix: row.NextHop, PrefixLength: uint8(got.Addr().BitLen())})
		if !ok || !nextHop.Addr().IsUnspecified() || nextHop.Addr().Is4() != got.Addr().Is4() {
			t.Fatal("wrong on-link next hop")
		}
		if row.InterfaceLuid != 1234 || row.Publish != 0 || row.Metric != 7 || row.Protocol != windows.MIB_IPPROTO_NETMGMT || row.SitePrefixLength != 0 || row.ValidLifetime != ^uint32(0) {
			t.Fatalf("wrong route policy: %+v", row)
		}
	}
}

func TestWindowsRoutesRollback(t *testing.T) {
	for _, failure := range []error{windows.ERROR_ACCESS_DENIED, windows.ERROR_OBJECT_ALREADY_EXISTS} {
		s := mockNetworkSession(t)
		s.config.Routes = []string{"0.0.0.0/1", "128.0.0.0/1", "::/0"}
		var created, deleted []windows.MibIpForwardRow2
		s.networkAPI.createRoute = func(row *windows.MibIpForwardRow2) error {
			if len(created) == 1 {
				return failure
			}
			created = append(created, *row)
			return nil
		}
		s.networkAPI.deleteRoute = func(row *windows.MibIpForwardRow2) error {
			deleted = append(deleted, *row)
			return nil
		}
		if err := s.addWindowsRoutes(); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if len(s.ownedRoutes) != 1 {
			t.Fatal("must only own successfully added routes")
		}
		// A replacement interface with the same alias must not be targeted.
		s.networkAPI.lookupLUID = func(string) (uint64, error) { t.Fatal("cleanup must retain old identity"); return 9999, nil }
		s.cleanupRoutes()
		if !reflect.DeepEqual(created, deleted) || len(s.ownedRoutes) != 0 {
			t.Fatal("incorrect rollback")
		}
		s.cleanupRoutes()
		if len(deleted) != 1 {
			t.Fatal("cleanup not idempotent")
		}
	}
}

func TestWindowsRoutesSuccessAndMalformed(t *testing.T) {
	s := mockNetworkSession(t)
	s.config.Routes = []string{" ", "0.0.0.0/1", "128.0.0.0/1", "::/0"}
	s.networkAPI.createRoute = func(*windows.MibIpForwardRow2) error { return nil }
	if err := s.addWindowsRoutes(); err != nil {
		t.Fatal(err)
	}
	if len(s.ownedRoutes) != 3 {
		t.Fatal("IPv6 route skipped")
	}
	s.config.Routes = []string{"invalid"}
	if err := s.addWindowsRoutes(); err == nil {
		t.Fatal("invalid route accepted")
	}
}

func TestWindowsNetworkNativeReadOnly(t *testing.T) {
	// Check ABI / exported entry points without requiring elevation or changing
	// any real adapter. Mutating paths above use fake functions instead.
	for _, proc := range []*windows.LazyProc{convertAliasToLUID, initializeAddress, createAddress, deleteAddress, setIPInterface, initializeRoute, createRoute, deleteRoute} {
		if err := proc.Find(); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := readWindowsAddresses()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected loopback address")
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		luid, err := windowsInterfaceLUID(iface.Name)
		if err != nil || luid == 0 {
			t.Fatalf("lookup failed: %v", err)
		}
		row := windows.MibIpInterfaceRow{Family: windows.AF_INET, InterfaceLuid: luid}
		if err := windows.GetIpInterfaceEntry(&row); err != nil {
			t.Fatal(err)
		}
		if row.InterfaceIndex != uint32(iface.Index) || row.NlMtu == 0 {
			t.Fatal("invalid IP interface layout")
		}
		for _, address := range rows {
			if address.InterfaceLuid == luid && addressIP(address).IsLoopback() {
				return
			}
		}
	}
	t.Fatal("loopback address not found in native table")
}
