//go:build linux

package winvpn

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"gonc-gui/internal/vpnconfig"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

const (
	interfaceName = "gonc0"
	interfaceIPv4 = "10.60.173.33"
	interfaceCIDR = interfaceIPv4 + "/24"
	defaultMTU    = 1400
)

type Session struct {
	mu     sync.Mutex
	config vpnconfig.Config
}

type defaultRoute struct {
	gateway string
	dev     string
}

func CheckRuntime() error {
	if _, err := exec.LookPath("ip"); err != nil {
		return fmt.Errorf("Linux system VPN requires the ip command from iproute2: %w", err)
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		return fmt.Errorf("Linux system VPN requires /dev/net/tun: %w", err)
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

	_ = run("ip", "link", "delete", interfaceName)
	engine.Insert(&engine.Key{
		Device:   "tun://" + interfaceName,
		Proxy:    config.SOCKS5Endpoint,
		LogLevel: config.LogLevel,
		MTU:      config.MTU,
	})
	engine.Start()

	session := &Session{config: config}
	if err := session.configure(); err != nil {
		_ = session.Stop()
		return nil, err
	}
	return session, nil
}

func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRoutes()
	engine.Stop()
	_ = run("ip", "link", "delete", interfaceName)
	return nil
}

func (s *Session) configure() error {
	if err := waitInterface(interfaceName, 8*time.Second); err != nil {
		return err
	}
	if err := run("ip", "addr", "replace", interfaceCIDR, "dev", interfaceName); err != nil {
		return fmt.Errorf("configure VPN IPv4 address: %w", err)
	}
	if s.config.EnableIPv6 {
		if err := run("ip", "-6", "addr", "replace", "fd00::2/128", "dev", interfaceName); err != nil {
			return fmt.Errorf("configure VPN IPv6 address: %w", err)
		}
	}
	if err := run("ip", "link", "set", "dev", interfaceName, "mtu", fmt.Sprint(s.config.MTU), "up"); err != nil {
		return fmt.Errorf("bring VPN interface up: %w", err)
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
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return
	}
	var dns []string
	for _, server := range s.config.DNSServers {
		server = strings.TrimSpace(server)
		if server == "" || (strings.Contains(server, ":") && !s.config.EnableIPv6) {
			continue
		}
		dns = append(dns, server)
	}
	if len(dns) == 0 {
		return
	}
	_ = run("resolvectl", append([]string{"dns", interfaceName}, dns...)...)
	_ = run("resolvectl", "domain", interfaceName, "~.")
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
			if err := run("ip", "-6", "route", "replace", cidr, "dev", interfaceName, "metric", "5"); err != nil {
				return fmt.Errorf("add VPN IPv6 route %s: %w", cidr, err)
			}
			continue
		}
		if err := run("ip", "route", "replace", cidr, "dev", interfaceName, "metric", "5"); err != nil {
			return fmt.Errorf("add VPN route %s: %w", cidr, err)
		}
	}
	return nil
}

func (s *Session) configureBypassRoutes() error {
	for _, ip := range s.config.BypassIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() {
			continue
		}
		if addr.Is6() {
			if !s.config.EnableIPv6 {
				continue
			}
			route, err := currentRouteTo(addr.String())
			if err != nil {
				return fmt.Errorf("find IPv6 route for VPN bypass %s: %w", addr, err)
			}
			args := []string{"-6", "route", "replace", addr.String() + "/128"}
			args = appendRouteGateway(args, route)
			args = append(args, "metric", "1")
			if err := run("ip", args...); err != nil {
				return fmt.Errorf("add VPN IPv6 bypass route %s: %w", addr, err)
			}
			continue
		}
		route, err := currentRouteTo(addr.String())
		if err != nil {
			return fmt.Errorf("find IPv4 route for VPN bypass %s: %w", addr, err)
		}
		args := []string{"route", "replace", addr.String() + "/32"}
		args = appendRouteGateway(args, route)
		args = append(args, "metric", "1")
		if err := run("ip", args...); err != nil {
			return fmt.Errorf("add VPN bypass route %s: %w", addr, err)
		}
	}
	return nil
}

func (s *Session) cleanupRoutes() {
	_ = run("resolvectl", "revert", interfaceName)
	for _, ip := range s.config.BypassIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || !addr.IsValid() {
			continue
		}
		if addr.Is6() {
			_ = run("ip", "-6", "route", "delete", addr.String()+"/128")
			continue
		}
		_ = run("ip", "route", "delete", addr.String()+"/32")
	}
	for _, cidr := range s.config.Routes {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if strings.Contains(cidr, ":") {
			_ = run("ip", "-6", "route", "delete", cidr, "dev", interfaceName)
			continue
		}
		_ = run("ip", "route", "delete", cidr, "dev", interfaceName)
	}
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

func currentRouteTo(ip string) (defaultRoute, error) {
	cmd := exec.Command("ip", "route", "get", ip)
	output, err := cmd.Output()
	if err != nil {
		return defaultRoute{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return defaultRoute{}, fmt.Errorf("route not found")
	}
	fields := strings.Fields(lines[0])
	var route defaultRoute
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "via":
			if i+1 < len(fields) {
				route.gateway = fields[i+1]
			}
		case "dev":
			if i+1 < len(fields) {
				route.dev = fields[i+1]
			}
		}
	}
	if route.gateway == "" && route.dev == "" {
		return defaultRoute{}, fmt.Errorf("route not found")
	}
	if route.dev == interfaceName {
		return defaultRoute{}, fmt.Errorf("route already points at %s", interfaceName)
	}
	return route, nil
}

func appendRouteGateway(args []string, route defaultRoute) []string {
	if route.gateway != "" {
		args = append(args, "via", route.gateway)
	}
	if route.dev != "" {
		args = append(args, "dev", route.dev)
	}
	return args
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
