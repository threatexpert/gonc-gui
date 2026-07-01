//go:build darwin

package winvpn

import (
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

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

const (
	interfaceName = "utun"
	interfaceIPv4 = "10.60.173.33"
	defaultMTU    = 1400
)

type Session struct {
	mu         sync.Mutex
	config     vpnconfig.Config
	ifaceName  string
	nameFile   string
	dnsRestore []dnsServiceState
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

	engine.Insert(&engine.Key{
		Device:   "tun://" + interfaceName,
		Proxy:    config.SOCKS5Endpoint,
		LogLevel: config.LogLevel,
		MTU:      config.MTU,
	})
	engine.Start()

	actualName, err := waitTUNName(nameFile, 8*time.Second)
	if err != nil {
		engine.Stop()
		_ = os.Remove(nameFile)
		return nil, err
	}
	session := &Session{config: config, ifaceName: actualName, nameFile: nameFile}
	if err := session.configure(); err != nil {
		_ = session.Stop()
		return nil, err
	}
	return session, nil
}

func StartWithTrace(config vpnconfig.Config, trace func(string)) (*Session, error) {
	return Start(config)
}

func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRoutes()
	engine.Stop()
	if s.nameFile != "" {
		_ = os.Remove(s.nameFile)
	}
	return nil
}

func (s *Session) configure() error {
	if err := waitInterface(s.ifaceName, 8*time.Second); err != nil {
		return err
	}
	if err := run("ifconfig", s.ifaceName, interfaceIPv4, interfaceIPv4, "mtu", fmt.Sprint(s.config.MTU), "up"); err != nil {
		return fmt.Errorf("configure VPN IPv4 address: %w", err)
	}
	if s.config.EnableIPv6 {
		if err := run("ifconfig", s.ifaceName, "inet6", "fd00::2", "prefixlen", "128", "up"); err != nil {
			return fmt.Errorf("configure VPN IPv6 address: %w", err)
		}
	}
	s.configureDNS()
	if err := s.configureBypassRoutes(); err != nil {
		return err
	}
	if err := s.configureRoutes(); err != nil {
		return err
	}
	return nil
}

func (s *Session) configureDNS() {
	var dns []string
	for _, server := range s.config.DNSServers {
		server = strings.TrimSpace(server)
		if server != "" {
			dns = append(dns, server)
		}
	}
	if len(dns) == 0 {
		return
	}
	services, err := networkServices()
	if err != nil {
		return
	}
	for _, service := range services {
		state, err := currentDNSServers(service)
		if err == nil {
			s.dnsRestore = append(s.dnsRestore, state)
		}
		_ = run("networksetup", append([]string{"-setdnsservers", service}, dns...)...)
	}
}

func (s *Session) configureBypassRoutes() error {
	gateway, err := currentDefaultGateway()
	if err != nil {
		if len(s.config.BypassIPs) == 0 {
			return nil
		}
		return fmt.Errorf("find default gateway for VPN bypass: %w", err)
	}
	for _, ip := range s.config.BypassIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || !addr.IsValid() || !addr.Is4() || addr.IsLoopback() || addr.IsUnspecified() {
			continue
		}
		if err := run("route", "-n", "add", "-host", addr.String(), gateway); err != nil {
			return fmt.Errorf("add VPN bypass route %s: %w", addr, err)
		}
	}
	return nil
}

func (s *Session) configureRoutes() error {
	for _, cidr := range s.config.Routes {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil || ip == nil {
			return fmt.Errorf("invalid VPN route %s", cidr)
		}
		if strings.Contains(cidr, ":") {
			if !s.config.EnableIPv6 {
				continue
			}
			if err := run("route", "-n", "add", "-inet6", cidr, "-interface", s.ifaceName); err != nil {
				return fmt.Errorf("add VPN IPv6 route %s: %w", cidr, err)
			}
			continue
		}
		if err := run("route", "-n", "add", "-net", cidr, interfaceIPv4); err != nil {
			return fmt.Errorf("add VPN route %s: %w", cidr, err)
		}
	}
	return nil
}

func (s *Session) cleanupRoutes() {
	s.restoreDNS()
	for _, ip := range s.config.BypassIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || !addr.IsValid() || !addr.Is4() {
			continue
		}
		_ = run("route", "-n", "delete", "-host", addr.String())
	}
	for _, cidr := range s.config.Routes {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if strings.Contains(cidr, ":") {
			_ = run("route", "-n", "delete", "-inet6", cidr)
			continue
		}
		_ = run("route", "-n", "delete", "-net", cidr)
	}
}

func (s *Session) restoreDNS() {
	for i := len(s.dnsRestore) - 1; i >= 0; i-- {
		state := s.dnsRestore[i]
		if state.empty || len(state.servers) == 0 {
			_ = run("networksetup", "-setdnsservers", state.name, "Empty")
			continue
		}
		_ = run("networksetup", append([]string{"-setdnsservers", state.name}, state.servers...)...)
	}
	s.dnsRestore = nil
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

func currentDefaultGateway() (string, error) {
	cmd := exec.Command("route", "-n", "get", "default")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "gateway:") {
			continue
		}
		gateway := strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		if gateway != "" {
			return gateway, nil
		}
	}
	return "", fmt.Errorf("default gateway not found")
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
