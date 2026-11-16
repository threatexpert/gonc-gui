//go:build windows

package winvpn

import (
	"fmt"
	"net"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

func verifyWindowsRoutes(routes []string) error {
	return verifyWindowsRoutesWith(routes, net.InterfaceByName, readWindowsRouteTable)
}

// Verify the same invariant as the former Get-NetRoute check: every configured
// prefix must exist on Gonc. This is not a best-route or connectivity probe.
func verifyWindowsRoutesWith(routes []string, lookup func(string) (*net.Interface, error), readTable func() ([]windows.MibIpForwardRow2, error)) error {
	if len(routes) == 0 {
		return fmt.Errorf("no VPN routes configured")
	}
	wanted := make([]netip.Prefix, 0, len(routes))
	for _, cidr := range routes {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return fmt.Errorf("invalid VPN route %q: %w", cidr, err)
		}
		wanted = append(wanted, prefix.Masked())
	}
	iface, err := lookup(interfaceName)
	if err != nil {
		return fmt.Errorf("find VPN interface %s: %w", interfaceName, err)
	}
	if iface == nil || iface.Index <= 0 {
		return fmt.Errorf("invalid VPN interface %s", interfaceName)
	}
	rows, err := readTable()
	if err != nil {
		return fmt.Errorf("read Windows VPN route table: %w", err)
	}
	present := make(map[netip.Prefix]struct{}, len(rows))
	for i := range rows {
		row := &rows[i]
		if row.InterfaceIndex != uint32(iface.Index) {
			continue
		}
		if prefix, ok := windowsRoutePrefix(row.DestinationPrefix); ok {
			present[prefix] = struct{}{}
		}
	}
	for _, prefix := range wanted {
		if _, ok := present[prefix]; !ok {
			return fmt.Errorf("VPN route missing on %s: %s", interfaceName, prefix)
		}
	}
	return nil
}

func readWindowsRouteTable() ([]windows.MibIpForwardRow2, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil {
		return nil, err
	}
	if table == nil {
		return nil, fmt.Errorf("GetIpForwardTable2 returned a nil table")
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	// Rows points into Windows-owned memory. Copy before FreeMibTable runs.
	return append([]windows.MibIpForwardRow2(nil), table.Rows()...), nil
}

func windowsRoutePrefix(prefix windows.IpAddressPrefix) (netip.Prefix, bool) {
	var addr netip.Addr
	switch prefix.Prefix.Family {
	case windows.AF_INET:
		addr = netip.AddrFrom4((*windows.RawSockaddrInet4)(unsafe.Pointer(&prefix.Prefix)).Addr)
	case windows.AF_INET6:
		addr = netip.AddrFrom16((*windows.RawSockaddrInet6)(unsafe.Pointer(&prefix.Prefix)).Addr)
	default:
		return netip.Prefix{}, false
	}
	p := netip.PrefixFrom(addr, int(prefix.PrefixLength))
	return p.Masked(), p.IsValid()
}
