//go:build windows

package winvpn

import (
	"errors"
	"fmt"
	"net/netip"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	setInterfaceDNS   = ipHelper.NewProc("SetInterfaceDnsSettings")
	convertLUIDToGUID = ipHelper.NewProc("ConvertInterfaceLuidToGuid")
)

// DNS_INTERFACE_SETTINGS_VERSION1. Only Nameserver and, for IPv6, IPv6 flags
// are set; domain/search/registration/LLMNR settings are left untouched.
type windowsDNSSettings struct {
	Version                                                                 uint32
	_                                                                       [4]byte
	Flags                                                                   uint64
	Domain, NameServer, SearchList                                          *uint16
	RegistrationEnabled, RegisterAdapterName, EnableLLMNR, QueryAdapterName uint32
	ProfileNameServer                                                       *uint16
}

func makeWindowsDNSSettings(family string, servers []string) (windowsDNSSettings, error) {
	s := windowsDNSSettings{Version: 1, Flags: 2} // DNS_SETTING_NAMESERVER
	if family == "ipv6" {
		s.Flags |= 1
	} else if family != "ipv4" {
		return s, fmt.Errorf("invalid DNS family %s", family)
	}
	for _, server := range servers {
		addr, err := netip.ParseAddr(server)
		if err != nil || addr.Is4() != (family == "ipv4") {
			return s, fmt.Errorf("invalid %s DNS server %q", family, server)
		}
	}
	// An empty, non-nil UTF-16 string clears static servers for this family.
	var err error
	s.NameServer, err = windows.UTF16PtrFromString(strings.Join(servers, ","))
	return s, err
}

func setWindowsDNSNative(luid uint64, family string, servers []string) error {
	settings, err := makeWindowsDNSSettings(family, servers)
	if err != nil {
		return err
	}
	if err := convertLUIDToGUID.Find(); err != nil {
		return err
	}
	var guid windows.GUID
	status, _, _ := convertLUIDToGUID.Call(uintptr(unsafe.Pointer(&luid)), uintptr(unsafe.Pointer(&guid)))
	if status != 0 {
		return syscall.Errno(status)
	}
	return callSetInterfaceDNS(guid, &settings)
}

func callSetInterfaceDNS(guid windows.GUID, settings *windowsDNSSettings) error {
	if err := setInterfaceDNS.Find(); err != nil {
		return err
	}
	// GUID is passed BY VALUE. The Windows ABI passes this 16-byte argument by
	// pointer on amd64, but in two registers on arm64 (four words on 386).
	var status uintptr
	switch runtime.GOARCH {
	case "amd64":
		status, _, _ = setInterfaceDNS.Call(uintptr(unsafe.Pointer(&guid)), uintptr(unsafe.Pointer(settings)))
	case "arm64":
		words := *(*[2]uintptr)(unsafe.Pointer(&guid))
		status, _, _ = setInterfaceDNS.Call(words[0], words[1], uintptr(unsafe.Pointer(settings)))
	case "386", "arm":
		words := *(*[4]uint32)(unsafe.Pointer(&guid))
		status, _, _ = setInterfaceDNS.Call(uintptr(words[0]), uintptr(words[1]), uintptr(words[2]), uintptr(words[3]), uintptr(unsafe.Pointer(settings)))
	default:
		return fmt.Errorf("unsupported Windows DNS ABI: %s", runtime.GOARCH)
	}
	runtime.KeepAlive(settings)
	if status != 0 {
		return syscall.Errno(status)
	}
	return nil
}

type windowsDNSBackend struct {
	available func() error
	lookup    func(string) (uint64, error)
	set       func(uint64, string, []string) error
	fallback  func(string, []string) error
	read      func(uint64) ([]string, error)
}

func configureWindowsDNS(ipv4, ipv6 []string) error {
	return configureWindowsDNSWith(ipv4, ipv6, windowsDNSBackend{
		available: setInterfaceDNS.Find, lookup: windowsInterfaceLUID, set: setWindowsDNSNative,
		fallback: configureWindowsDNSServersNetsh, read: readWindowsDNSServers,
	})
}

func configureWindowsDNSWith(ipv4, ipv6 []string, api windowsDNSBackend) error {
	// Validate before applying either family, including on the legacy path.
	if _, err := makeWindowsDNSSettings("ipv4", ipv4); err != nil {
		return err
	}
	if _, err := makeWindowsDNSSettings("ipv6", ipv6); err != nil {
		return err
	}
	err := api.available()
	legacy := errors.Is(err, windows.ERROR_PROC_NOT_FOUND)
	if err != nil && !legacy {
		return fmt.Errorf("load Windows DNS API: %w", err)
	}
	luid, err := api.lookup(interfaceName)
	if err != nil {
		return err
	}
	if luid == 0 {
		return fmt.Errorf("invalid VPN DNS interface")
	}
	for _, item := range []struct {
		family  string
		servers []string
	}{{"ipv4", ipv4}, {"ipv6", ipv6}} {
		if legacy {
			err = api.fallback(item.family, item.servers)
		} else {
			err = api.set(luid, item.family, item.servers)
		}
		if err != nil {
			return fmt.Errorf("configure VPN %s DNS: %w", item.family, err)
		}
	}
	wanted := append(append([]string{}, ipv4...), ipv6...)
	// Read back the effective adapter list, including IPv6, rather than assuming
	// that a successful setter also cleared old servers. Allow brief propagation.
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		actual, err := api.read(luid)
		if err != nil {
			return fmt.Errorf("verify VPN DNS: %w", err)
		}
		if equalDNSServers(actual, wanted) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("VPN DNS mismatch: got %v, want %v", actual, wanted)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func equalDNSServers(actual, wanted []string) bool {
	normalize := func(values []string) []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			if addr, err := netip.ParseAddr(value); err == nil {
				value = addr.Unmap().String()
			}
			out = append(out, value)
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(normalize(actual), normalize(wanted))
}

func readWindowsDNSServers(luid uint64) ([]string, error) {
	var size uint32 = 15 * 1024
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		flags := uint32(windows.GAA_FLAG_INCLUDE_ALL_INTERFACES | windows.GAA_FLAG_SKIP_UNICAST | windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for adapter := first; adapter != nil; adapter = adapter.Next {
			if adapter.Luid != luid {
				continue
			}
			var servers []string
			for dns := adapter.FirstDnsServerAddress; dns != nil; dns = dns.Next {
				ip := dns.Address.IP()
				if ip == nil {
					return nil, fmt.Errorf("invalid adapter DNS address")
				}
				servers = append(servers, ip.String())
			}
			runtime.KeepAlive(buf)
			return servers, nil
		}
		return nil, fmt.Errorf("VPN interface disappeared while reading DNS")
	}
	return nil, fmt.Errorf("adapter table kept changing while reading DNS")
}
