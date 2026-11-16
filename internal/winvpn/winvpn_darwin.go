//go:build darwin

package winvpn

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gonc-gui/internal/vpnconfig"
)

const (
	interfaceName = "utun"
	interfaceIPv4 = "10.60.173.33"
	defaultMTU    = 1400
)

type Session struct {
	mu           sync.Mutex
	config       vpnconfig.Config
	ifaceName    string
	nameFile     string
	dnsRestore   []dnsServiceState
	appliedDNS   []string
	routeLedger  *routeLedger
	bypassRoutes []ownedRoute
	trace        func(string)
}

type dnsServiceState struct {
	name    string
	servers []string
	empty   bool
}

func CheckRuntime() error {
	for _, name := range []string{"ifconfig", "networksetup", "route"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("macOS system VPN requires %s: %w", name, err)
		}
	}
	return nil
}

func Start(config vpnconfig.Config) (*Session, error) {
	return StartWithTrace(config, nil)
}

func StartWithTrace(config vpnconfig.Config, trace func(string)) (*Session, error) {
	config.Routes = vpnconfig.DesktopRoutes(config.Routes)
	config.EnableIPv6 = vpnconfig.HasIPv6(config.Routes)
	if strings.TrimSpace(config.SOCKS5Endpoint) == "" {
		return nil, fmt.Errorf("SOCKS5 endpoint is required")
	}
	if err := CheckRuntime(); err != nil {
		return nil, err
	}
	if config.MTU <= 0 {
		config.MTU = defaultMTU
	}
	if config.LogLevel == "" {
		config.LogLevel = "warn"
	}

	nameFile, err := makeNameFile()
	if err != nil {
		return nil, err
	}
	previousNameFile := os.Getenv("WG_TUN_NAME_FILE")
	_ = os.Setenv("WG_TUN_NAME_FILE", nameFile)
	defer func() {
		if previousNameFile == "" {
			_ = os.Unsetenv("WG_TUN_NAME_FILE")
		} else {
			_ = os.Setenv("WG_TUN_NAME_FILE", previousNameFile)
		}
	}()

	if err := startEngine(config, "tun://"+interfaceName); err != nil {
		_ = stopEngine()
		_ = os.Remove(nameFile)
		return nil, err
	}

	actualName, err := waitTUNName(nameFile, 8*time.Second)
	if err != nil {
		_ = stopEngine()
		_ = os.Remove(nameFile)
		return nil, err
	}
	session := &Session{config: config, ifaceName: actualName, nameFile: nameFile, trace: trace}
	if err := session.configure(); err != nil {
		return nil, errors.Join(err, session.Stop())
	}
	return session, nil
}

func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.cleanupRoutes()
	err = errors.Join(err, stopEngine())
	if s.nameFile != "" {
		_ = os.Remove(s.nameFile)
	}
	return err
}

func (s *Session) configure() error {
	if err := waitInterface(s.ifaceName, 8*time.Second); err != nil {
		return err
	}
	if err := run("ifconfig", s.ifaceName, interfaceIPv4, interfaceIPv4, "mtu", fmt.Sprint(s.config.MTU), "up"); err != nil {
		return fmt.Errorf("configure VPN IPv4 address: %w", err)
	}
	if s.config.EnableIPv6 {
		if err := run("ifconfig", s.ifaceName, "inet6", "fd60:173:33::2", "prefixlen", "128", "up"); err != nil {
			return fmt.Errorf("configure VPN IPv6 address: %w", err)
		}
	}
	return applyDarwinNetwork(darwinNetworkSteps{
		bypass:       s.configureBypassRoutes,
		capture:      s.configureRoutes,
		dns:          s.configureDNS,
		verifyBypass: s.verifyBypassRoutes,
	})
}

func (s *Session) DNSServers() []string {
	return append([]string{}, s.appliedDNS...)
}

func (s *Session) UpdateDNS(servers []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.DNSServers = append([]string{}, servers...)
	return s.configureDNS()
}

func (s *Session) configureDNS() error {
	var dns []string
	for _, server := range s.config.DNSServers {
		server = strings.TrimSpace(server)
		if server != "" {
			dns = append(dns, server)
		}
	}
	if len(dns) == 0 {
		return fmt.Errorf("no usable VPN DNS servers")
	}
	// Explicit DNS host routes also keep split-tunnel/fallback DNS in the VPN.
	for _, server := range dns {
		addr, err := netip.ParseAddr(server)
		if err != nil {
			return fmt.Errorf("macOS VPN DNS requires a plain IP address: %s", server)
		}
		for _, bypass := range s.bypassRoutes {
			if bypass.prefix.Addr() == addr {
				return fmt.Errorf("VPN DNS %s conflicts with tunnel peer bypass", addr)
			}
		}
		if err := s.addOwnedRoute(ownedRoute{prefix: netip.PrefixFrom(addr, addr.BitLen()), iface: s.ifaceName}); err != nil {
			return fmt.Errorf("configure VPN DNS route for %s: %w", addr, err)
		}
	}
	services, err := networkServices()
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return fmt.Errorf("no network services available to configure VPN DNS")
	}
	for _, service := range services {
		saved := false
		for _, state := range s.dnsRestore {
			if state.name == service {
				saved = true
				break
			}
		}
		if !saved {
			state, err := currentDNSServers(service)
			if err != nil {
				return fmt.Errorf("read DNS for %s: %w", service, err)
			}
			s.dnsRestore = append(s.dnsRestore, state)
		}
		if err := run("networksetup", append([]string{"-setdnsservers", service}, dns...)...); err != nil {
			return err
		}
		actual, err := currentDNSServers(service)
		if err != nil {
			return fmt.Errorf("verify VPN DNS for %s: %w", service, err)
		}
		if !sameDarwinDNS(actual.servers, dns) {
			return fmt.Errorf("VPN DNS verification failed for %s: got %v, want %v", service, actual.servers, dns)
		}
	}
	s.appliedDNS = append([]string{}, dns...)
	return nil
}

func (s *Session) configureBypassRoutes() error {
	// Snapshot all native exits before installing any capture or DNS route.
	routes, err := planDarwinBypass(s.config.BypassIPs, s.ifaceName, nativeRouteTo)
	if err != nil {
		return err
	}
	s.bypassRoutes = routes
	for _, r := range routes {
		if s.trace != nil {
			s.trace(fmt.Sprintf("VPN bypass %s: native interface=%s gateway=%s", r.prefix, r.iface, r.gateway))
		}
		if err := s.addOwnedRoute(r); err != nil {
			return fmt.Errorf("add VPN bypass route %s: %w", r.prefix, err)
		}
	}
	return nil
}

func (s *Session) verifyBypassRoutes() error {
	return verifyDarwinBypass(s.bypassRoutes, s.ifaceName, nativeRouteTo, s.trace)
}

func (s *Session) configureRoutes() error {
	for _, cidr := range s.config.Routes {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return fmt.Errorf("invalid VPN route %s", cidr)
		}
		if err := s.addOwnedRoute(ownedRoute{prefix: prefix.Masked(), iface: s.ifaceName}); err != nil {
			return fmt.Errorf("add VPN route %s: %w", cidr, err)
		}
	}
	return nil
}

func (s *Session) cleanupRoutes() error {
	var failures []error
	if err := s.restoreDNS(); err != nil {
		failures = append(failures, err)
	}
	if s.routeLedger != nil {
		if err := s.routeLedger.cleanup(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Session) restoreDNS() error {
	var failures []error
	var retry []dnsServiceState
	for i := len(s.dnsRestore) - 1; i >= 0; i-- {
		state := s.dnsRestore[i]
		var err error
		if state.empty || len(state.servers) == 0 {
			err = run("networksetup", "-setdnsservers", state.name, "Empty")
		} else {
			err = run("networksetup", append([]string{"-setdnsservers", state.name}, state.servers...)...)
		}
		if err == nil {
			var actual dnsServiceState
			actual, err = currentDNSServers(state.name)
			if err == nil && !sameDarwinDNS(actual.servers, state.servers) {
				err = fmt.Errorf("DNS restore could not be verified for %s", state.name)
			}
		}
		if err != nil {
			failures = append(failures, err)
			retry = append([]dnsServiceState{state}, retry...)
		}
	}
	s.dnsRestore = retry
	return errors.Join(failures...)
}

func networkServices() ([]string, error) {
	cmd := exec.Command("networksetup", "-listallnetworkservices")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var services []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

func currentDNSServers(service string) (dnsServiceState, error) {
	cmd := exec.Command("networksetup", "-getdnsservers", service)
	output, err := cmd.Output()
	if err != nil {
		return dnsServiceState{}, err
	}
	state := dnsServiceState{name: service}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "aren't any DNS Servers") {
			state.empty = true
			state.servers = nil
			return state, nil
		}
		if _, err := netip.ParseAddr(line); err != nil {
			return dnsServiceState{}, fmt.Errorf("read DNS for %s: %s", service, line)
		}
		state.servers = append(state.servers, line)
	}
	state.empty = len(state.servers) == 0
	return state, nil
}

func makeNameFile() (string, error) {
	f, err := os.CreateTemp("", "gonc-utun-name-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return filepath.Abs(name)
}

func waitTUNName(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			name := strings.TrimSpace(string(data))
			if name != "" {
				return name, nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "", fmt.Errorf("VPN utun interface name did not appear")
}

func waitInterface(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := net.InterfaceByName(name); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("VPN interface %q did not appear", name)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
