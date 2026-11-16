//go:build windows

package winvpn

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func testWindowsRoute(cidr string, index uint32) windows.MibIpForwardRow2 {
	p := netip.MustParsePrefix(cidr)
	row := windows.MibIpForwardRow2{InterfaceIndex: index}
	row.DestinationPrefix.PrefixLength = uint8(p.Bits())
	if p.Addr().Is4() {
		sa := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row.DestinationPrefix.Prefix))
		sa.Family, sa.Addr = windows.AF_INET, p.Addr().As4()
	} else {
		sa := (*windows.RawSockaddrInet6)(unsafe.Pointer(&row.DestinationPrefix.Prefix))
		sa.Family, sa.Addr = windows.AF_INET6, p.Addr().As16()
	}
	return row
}

func TestVerifyWindowsRoutes(t *testing.T) {
	const index = 42
	v4 := testWindowsRoute("10.20.0.0/16", index)
	v6 := testWindowsRoute("2001:db8::/32", index)
	unknownFamily := v6
	unknownFamily.DestinationPrefix.Prefix.Family = windows.AF_UNSPEC
	badMask := v4
	badMask.DestinationPrefix.PrefixLength = 33
	for _, tc := range []struct {
		name    string
		wanted  []string
		rows    []windows.MibIpForwardRow2
		wantErr string
	}{
		{"dual stack", []string{"10.20.0.0/16", "2001:db8::/32"}, []windows.MibIpForwardRow2{v4, v6}, ""},
		{"IPv4 only", []string{"10.20.0.0/16"}, []windows.MibIpForwardRow2{v4}, ""},
		{"IPv6 only", []string{"2001:db8::/32"}, []windows.MibIpForwardRow2{v6}, ""},
		{"default routes", []string{"0.0.0.0/0", "::/0"}, []windows.MibIpForwardRow2{testWindowsRoute("0.0.0.0/0", index), testWindowsRoute("::/0", index)}, ""},
		{"host routes", []string{"10.20.1.2/32", "2001:db8::1/128"}, []windows.MibIpForwardRow2{testWindowsRoute("10.20.1.2/32", index), testWindowsRoute("2001:db8::1/128", index)}, ""},
		{"normalize and duplicates", []string{"10.20.1.2/16", "10.20.0.0/16", "2001:0db8:0000::1/32"}, []windows.MibIpForwardRow2{v4, v4, v6}, ""},
		{"missing IPv6", []string{"10.20.0.0/16", "2001:db8::/32"}, []windows.MibIpForwardRow2{v4}, "VPN route missing on Gonc: 2001:db8::/32"},
		{"wrong interface", []string{"2001:db8::/32"}, []windows.MibIpForwardRow2{testWindowsRoute("2001:db8::/32", index+1)}, "VPN route missing"},
		{"covering route not exact", []string{"2001:db8::/32"}, []windows.MibIpForwardRow2{testWindowsRoute("::/0", index)}, "VPN route missing"},
		{"wrong prefix length", []string{"10.20.0.0/16"}, []windows.MibIpForwardRow2{testWindowsRoute("10.20.0.0/24", index)}, "VPN route missing"},
		{"empty table", []string{"::/0"}, nil, "VPN route missing"},
		{"empty configuration", nil, nil, "no VPN routes configured"},
		{"malformed configuration", []string{"not-a-route"}, nil, "invalid VPN route"},
		{"unknown family", []string{"2001:db8::/32"}, []windows.MibIpForwardRow2{unknownFamily}, "VPN route missing"},
		{"invalid mask", []string{"10.20.0.0/16"}, []windows.MibIpForwardRow2{badMask}, "VPN route missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			err := verifyWindowsRoutesWith(tc.wanted, func(name string) (*net.Interface, error) {
				if name != interfaceName {
					t.Fatalf("unexpected interface: %s", name)
				}
				return &net.Interface{Index: index}, nil
			}, func() ([]windows.MibIpForwardRow2, error) {
				reads++
				return tc.rows, nil
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if reads != 1 {
					t.Fatalf("read table %d times, want once", reads)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestVerifyWindowsRoutesQueryFailures(t *testing.T) {
	wanted := []string{"::/0"}
	failure := errors.New("query failed")
	err := verifyWindowsRoutesWith(wanted, func(string) (*net.Interface, error) {
		return nil, failure
	}, func() ([]windows.MibIpForwardRow2, error) {
		t.Fatal("must not read routes after interface lookup failed")
		return nil, nil
	})
	if !errors.Is(err, failure) {
		t.Fatalf("lost lookup error: %v", err)
	}
	err = verifyWindowsRoutesWith(wanted, func(string) (*net.Interface, error) {
		return &net.Interface{Index: 42}, nil
	}, func() ([]windows.MibIpForwardRow2, error) { return nil, failure })
	if !errors.Is(err, failure) {
		t.Fatalf("lost route query error: %v", err)
	}
	for _, iface := range []*net.Interface{nil, {Index: 0}} {
		err = verifyWindowsRoutesWith(wanted, func(string) (*net.Interface, error) {
			return iface, nil
		}, func() ([]windows.MibIpForwardRow2, error) {
			t.Fatal("must not read routes with invalid interface")
			return nil, nil
		})
		if err == nil {
			t.Fatal("invalid interface accepted")
		}
	}
}

// Read-only integration test: exercise the actual Windows ABI, including both
// sockaddr layouts and the lifetime of the copied table after FreeMibTable.
func TestReadWindowsRouteTableNative(t *testing.T) {
	started := time.Now()
	rows, err := readWindowsRouteTable()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native route query: %s, %d rows", time.Since(started), len(rows))
	if len(rows) == 0 {
		t.Fatal("expected at least Windows loopback routes")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		for _, row := range rows {
			p, ok := windowsRoutePrefix(row.DestinationPrefix)
			if row.InterfaceIndex == uint32(iface.Index) && ok && p.Addr().IsLoopback() {
				found = true
				// Exercise the full matcher against real OS data, without creating a VPN.
				err := verifyWindowsRoutesWith([]string{p.String()}, func(string) (*net.Interface, error) {
					return &iface, nil
				}, func() ([]windows.MibIpForwardRow2, error) { return rows, nil })
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if !found {
		t.Fatal("native table did not match a loopback interface route")
	}
}
