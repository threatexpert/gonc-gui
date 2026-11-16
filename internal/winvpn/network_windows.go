//go:build windows

package winvpn

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ipHelper           = windows.NewLazySystemDLL("iphlpapi.dll")
	convertAliasToLUID = ipHelper.NewProc("ConvertInterfaceAliasToLuid")
	initializeAddress  = ipHelper.NewProc("InitializeUnicastIpAddressEntry")
	createAddress      = ipHelper.NewProc("CreateUnicastIpAddressEntry")
	deleteAddress      = ipHelper.NewProc("DeleteUnicastIpAddressEntry")
	setIPInterface     = ipHelper.NewProc("SetIpInterfaceEntry")
	initializeRoute    = ipHelper.NewProc("InitializeIpForwardEntry")
	createRoute        = ipHelper.NewProc("CreateIpForwardEntry2")
	deleteRoute        = ipHelper.NewProc("DeleteIpForwardEntry2")
)

// Mutating IP Helper calls are kept behind this small boundary so failure and
// rollback tests never change the host's real network configuration.
type windowsNetworkAPI struct {
	lookupLUID    func(string) (uint64, error)
	addresses     func() ([]windows.MibUnicastIpAddressRow, error)
	createAddress func(*windows.MibUnicastIpAddressRow) error
	deleteAddress func(*windows.MibUnicastIpAddressRow) error
	getInterface  func(*windows.MibIpInterfaceRow) error
	setInterface  func(*windows.MibIpInterfaceRow) error
	createRoute   func(*windows.MibIpForwardRow2) error
	deleteRoute   func(*windows.MibIpForwardRow2) error
}

var nativeWindowsNetwork = windowsNetworkAPI{
	lookupLUID: windowsInterfaceLUID,
	addresses:  readWindowsAddresses,
	createAddress: func(row *windows.MibUnicastIpAddressRow) error {
		return callIPHelper(createAddress, unsafe.Pointer(row))
	},
	deleteAddress: func(row *windows.MibUnicastIpAddressRow) error {
		return callIPHelper(deleteAddress, unsafe.Pointer(row))
	},
	getInterface: windows.GetIpInterfaceEntry,
	setInterface: func(row *windows.MibIpInterfaceRow) error { return callIPHelper(setIPInterface, unsafe.Pointer(row)) },
	createRoute:  func(row *windows.MibIpForwardRow2) error { return callIPHelper(createRoute, unsafe.Pointer(row)) },
	deleteRoute:  func(row *windows.MibIpForwardRow2) error { return callIPHelper(deleteRoute, unsafe.Pointer(row)) },
}

func callIPHelper(proc *windows.LazyProc, row unsafe.Pointer) error {
	if err := proc.Find(); err != nil {
		return err
	}
	status, _, _ := proc.Call(uintptr(row))
	// NETIOAPI returns a status code, not GetLastError.
	if status != 0 {
		return syscall.Errno(status)
	}
	return nil
}

func windowsInterfaceLUID(name string) (uint64, error) {
	alias, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	if err := convertAliasToLUID.Find(); err != nil {
		return 0, err
	}
	var luid uint64
	status, _, _ := convertAliasToLUID.Call(uintptr(unsafe.Pointer(alias)), uintptr(unsafe.Pointer(&luid)))
	if status != 0 {
		return 0, syscall.Errno(status)
	}
	return luid, nil
}

func readWindowsAddresses() ([]windows.MibUnicastIpAddressRow, error) {
	var table *windows.MibUnicastIpAddressTable
	if err := windows.GetUnicastIpAddressTable(windows.AF_INET, &table); err != nil {
		return nil, err
	}
	if table == nil {
		return nil, fmt.Errorf("GetUnicastIpAddressTable returned a nil table")
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	return append([]windows.MibUnicastIpAddressRow(nil), unsafe.Slice(&table.Table[0], table.NumEntries)...), nil
}

func (s *Session) network() *windowsNetworkAPI {
	if s.networkAPI != nil {
		return s.networkAPI
	}
	return &nativeWindowsNetwork
}

func (s *Session) vpnLUID() (uint64, error) {
	if s.interfaceLUID != 0 {
		return s.interfaceLUID, nil
	}
	luid, err := s.network().lookupLUID(interfaceName)
	if err != nil {
		return 0, fmt.Errorf("find VPN interface %s: %w", interfaceName, err)
	}
	if luid == 0 {
		return 0, fmt.Errorf("invalid VPN interface %s", interfaceName)
	}
	s.interfaceLUID = luid
	return luid, nil
}

func windowsSockaddr(addr netip.Addr) windows.RawSockaddrInet {
	var raw windows.RawSockaddrInet
	if addr.Is4() {
		sa := (*windows.RawSockaddrInet4)(unsafe.Pointer(&raw))
		sa.Family, sa.Addr = windows.AF_INET, addr.As4()
	} else {
		sa := (*windows.RawSockaddrInet6)(unsafe.Pointer(&raw))
		sa.Family, sa.Addr = windows.AF_INET6, addr.As16()
	}
	return raw
}

func newWindowsAddress(luid uint64, cidr string) (windows.MibUnicastIpAddressRow, error) {
	var row windows.MibUnicastIpAddressRow
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return row, err
	}
	if err := initializeAddress.Find(); err != nil {
		return row, err
	}
	initializeAddress.Call(uintptr(unsafe.Pointer(&row))) // void, no status code
	raw := windowsSockaddr(prefix.Addr())                 // Keep host bits for interface addresses.
	row.Address = *(*windows.RawSockaddrInet6)(unsafe.Pointer(&raw))
	row.InterfaceLuid = luid
	row.PrefixOrigin, row.SuffixOrigin = 1, 1 // IpPrefixOriginManual / IpSuffixOriginManual
	row.ValidLifetime, row.PreferredLifetime = ^uint32(0), ^uint32(0)
	row.OnLinkPrefixLength = uint8(prefix.Bits())
	row.SkipAsSource = 0
	row.DadState = 4 // IpDadStatePreferred (optimistic DAD, as used by Wintun clients)
	return row, nil
}

func (s *Session) configureIPv4Address() error {
	luid, err := s.vpnLUID()
	if err != nil {
		return err
	}
	row, err := newWindowsAddress(luid, interfaceIPv4+"/24")
	if err != nil {
		return err
	}
	rows, err := s.network().addresses()
	if err != nil {
		return fmt.Errorf("read VPN IPv4 addresses: %w", err)
	}
	// Like netsh set address static, replace existing IPv4 addresses on this
	// adapter only. Wintun installs its adapter with DHCP disabled.
	for i := range rows {
		old := &rows[i]
		if old.InterfaceLuid != luid || old.Address.Family != windows.AF_INET {
			continue
		}
		if err := s.network().deleteAddress(old); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
			return fmt.Errorf("remove old VPN IPv4 address: %w", err)
		}
	}
	return s.network().createAddress(&row)
}

func (s *Session) configureIPv6Address() error {
	luid, err := s.vpnLUID()
	if err != nil {
		return err
	}
	for _, cidr := range []string{"fd00::2/128", "fd60:173:33::2/128"} {
		row, err := newWindowsAddress(luid, cidr)
		if err != nil {
			return err
		}
		// Preserve the previous best-effort cleanup of the two legacy addresses.
		_ = s.network().deleteAddress(&row)
	}
	row, err := newWindowsAddress(luid, "fd60:173:33::2/128")
	if err != nil {
		return err
	}
	if err := s.network().createAddress(&row); err != nil {
		return fmt.Errorf("configure VPN IPv6 address: %w", err)
	}
	return nil
}

func (s *Session) configureInterfaceMetric() error {
	luid, err := s.vpnLUID()
	if err != nil {
		return err
	}
	families := []uint16{windows.AF_INET}
	if s.config.EnableIPv6 {
		families = append(families, windows.AF_INET6)
	}
	for _, family := range families {
		row := windows.MibIpInterfaceRow{Family: family, InterfaceLuid: luid}
		if err := s.network().getInterface(&row); err != nil {
			return fmt.Errorf("read VPN interface metric (family %d): %w", family, err)
		}
		// Read-modify-write: do not reset MTU, forwarding, DAD or other settings.
		row.UseAutomaticMetric = 0
		row.Metric = uint32(normalizeRouteMetric(s.config.RouteMetric))
		// Required by SetIpInterfaceEntry, even when Get returned another value.
		if family == windows.AF_INET {
			row.SitePrefixLength = 0
		}
		if err := s.network().setInterface(&row); err != nil {
			return fmt.Errorf("configure VPN interface metric (family %d): %w", family, err)
		}
	}
	return nil
}

func newWindowsRoute(luid uint64, cidr string, metric int) (windows.MibIpForwardRow2, error) {
	var row windows.MibIpForwardRow2
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return row, err
	}
	prefix = prefix.Masked()
	if err := initializeRoute.Find(); err != nil {
		return row, err
	}
	initializeRoute.Call(uintptr(unsafe.Pointer(&row))) // void
	row.InterfaceLuid = luid
	row.DestinationPrefix = windows.IpAddressPrefix{Prefix: windowsSockaddr(prefix.Addr()), PrefixLength: uint8(prefix.Bits())}
	nextHop := netip.IPv4Unspecified()
	if prefix.Addr().Is6() {
		nextHop = netip.IPv6Unspecified()
	}
	row.NextHop = windowsSockaddr(nextHop)
	row.SitePrefixLength = 0
	row.Metric = uint32(normalizeRouteMetric(metric))
	row.Protocol = windows.MIB_IPPROTO_NETMGMT
	row.Publish = 0
	row.ValidLifetime, row.PreferredLifetime = ^uint32(0), ^uint32(0)
	return row, nil
}

func (s *Session) addWindowsRoutes() error {
	luid, err := s.vpnLUID()
	if err != nil {
		return err
	}
	for _, cidr := range s.config.Routes {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		row, err := newWindowsRoute(luid, cidr, s.config.RouteMetric)
		if err != nil {
			return fmt.Errorf("invalid VPN route %s: %w", cidr, err)
		}
		if err := s.network().createRoute(&row); err != nil {
			return fmt.Errorf("add VPN route %s: %w", cidr, err)
		}
		// Record exact interface identity and next hop only after successful add.
		s.ownedRoutes = append(s.ownedRoutes, row)
	}
	return nil
}

func (s *Session) cleanupRoutes() {
	for i := range s.ownedRoutes {
		_ = s.network().deleteRoute(&s.ownedRoutes[i])
	}
	s.ownedRoutes = nil
}
