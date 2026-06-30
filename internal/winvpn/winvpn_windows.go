//go:build windows

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
	"syscall"
	"time"

	"gonc-gui/internal/vpnconfig"

	"github.com/xjasonlyu/tun2socks/v2/engine"
	"golang.org/x/sys/windows"
)

const (
	interfaceName      = "Gonc"
	interfaceIPv4      = "10.60.173.33"
	interfaceMask      = "255.255.255.0"
	defaultMTU         = 1400
	defaultRouteMetric = 1
	dnsLeakRuleUDPLow  = "Gonc VPN DNS Leak Protection Dnscache UDP 53 non-VPN local low"
	dnsLeakRuleUDPHigh = "Gonc VPN DNS Leak Protection Dnscache UDP 53 non-VPN local high"
	dnsLeakRuleTCPLow  = "Gonc VPN DNS Leak Protection Dnscache TCP 53 non-VPN local low"
	dnsLeakRuleTCPHigh = "Gonc VPN DNS Leak Protection Dnscache TCP 53 non-VPN local high"
	legacyDNSLeakTCP   = "Gonc VPN DNS Leak Protection TCP 53"
	legacyDNSLeakUDP   = "Gonc VPN DNS Leak Protection UDP 53"
	legacyDNSCacheTCP  = "Gonc VPN DNS Leak Protection Dnscache TCP 53"
	legacyDNSCacheUDP  = "Gonc VPN DNS Leak Protection Dnscache UDP 53"
)

type Session struct {
	mu     sync.Mutex
	config vpnconfig.Config
}

type routeTarget struct {
	ifIndex string
	nextHop string
}

func CheckRuntime() error {
	dllPath, err := findWintunDLL()
	if err != nil {
		return err
	}
	dll, err := windows.LoadDLL(dllPath)
	if err != nil {
		return fmt.Errorf("load Wintun runtime %s: %w", dllPath, err)
	}
	_ = dll.Release()
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
	config.RouteMetric = normalizeRouteMetric(config.RouteMetric)
	if config.LogLevel == "" {
		config.LogLevel = "warn"
	}
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
	err := cleanupDNSLeakFirewallRules()
	engine.Stop()
	return err
}

func (s *Session) configure() error {
	if err := waitInterface(interfaceName, 8*time.Second); err != nil {
		return err
	}
	if err := run("netsh", "interface", "ipv4", "set", "address", "name="+interfaceName, "static", interfaceIPv4, interfaceMask); err != nil {
		return fmt.Errorf("configure VPN IPv4 address: %w", err)
	}
	if s.config.EnableIPv6 {
		_ = run("netsh", "interface", "ipv6", "delete", "address", "interface="+interfaceName, "address=fd00::2")
		if err := run("netsh", "interface", "ipv6", "add", "address", "interface="+interfaceName, "address=fd00::2/128"); err != nil {
			return fmt.Errorf("configure VPN IPv6 address: %w", err)
		}
	}
	if err := s.configureInterfaceMetric(); err != nil {
		return err
	}
	if err := s.configureDNS(); err != nil {
		return err
	}
	if err := s.configureBypassRoutes(); err != nil {
		return err
	}
	if err := s.configureRoutes(); err != nil {
		return err
	}
	if err := s.configureDNSLeakProtection(); err != nil {
		return err
	}
	return nil
}

func normalizeRouteMetric(value int) int {
	if value < 1 || value > 9999 {
		return defaultRouteMetric
	}
	return value
}

func (s *Session) configureInterfaceMetric() error {
	metric := fmt.Sprint(s.config.RouteMetric)
	if err := run("netsh", "interface", "ipv4", "set", "interface", "interface="+interfaceName, "metric="+metric); err != nil {
		return fmt.Errorf("configure VPN IPv4 interface metric: %w", err)
	}
	if s.config.EnableIPv6 {
		if err := run("netsh", "interface", "ipv6", "set", "interface", "interface="+interfaceName, "metric="+metric); err != nil {
			return fmt.Errorf("configure VPN IPv6 interface metric: %w", err)
		}
	}
	return nil
}

func (s *Session) configureDNSLeakProtection() error {
	_ = cleanupDNSLeakFirewallRules()
	if !s.config.BlockDNSLeak {
		return nil
	}
	if err := addDNSLeakFirewallRule(dnsLeakRuleUDPLow, "UDP", "0.0.0.0-10.60.173.32"); err != nil {
		return err
	}
	if err := addDNSLeakFirewallRule(dnsLeakRuleUDPHigh, "UDP", "10.60.173.34-255.255.255.255"); err != nil {
		_ = cleanupDNSLeakFirewallRules()
		return err
	}
	if err := addDNSLeakFirewallRule(dnsLeakRuleTCPLow, "TCP", "0.0.0.0-10.60.173.32"); err != nil {
		_ = cleanupDNSLeakFirewallRules()
		return err
	}
	if err := addDNSLeakFirewallRule(dnsLeakRuleTCPHigh, "TCP", "10.60.173.34-255.255.255.255"); err != nil {
		_ = cleanupDNSLeakFirewallRules()
		return err
	}
	return nil
}

func addDNSLeakFirewallRule(name string, protocol string, localIP string) error {
	if err := run("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+name,
		"dir=out",
		"action=block",
		"protocol="+protocol,
		"localip="+localIP,
		"remoteport=53",
		"service=Dnscache",
		"profile=any",
		"enable=yes"); err != nil {
		return fmt.Errorf("add DNS leak firewall rule %s: %w", name, err)
	}
	return nil
}

func cleanupDNSLeakFirewallRules() error {
	var firstErr error
	for _, name := range []string{
		dnsLeakRuleUDPLow,
		dnsLeakRuleUDPHigh,
		dnsLeakRuleTCPLow,
		dnsLeakRuleTCPHigh,
		legacyDNSCacheUDP,
		legacyDNSCacheTCP,
		legacyDNSLeakUDP,
		legacyDNSLeakTCP,
	} {
		if err := removeDNSLeakFirewallRule(name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func removeDNSLeakFirewallRule(name string) error {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+name)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if isFirewallRuleMissingMessage(msg) {
			return nil
		}
		if msg != "" {
			return fmt.Errorf("remove DNS leak firewall rule %s: %w: %s", name, err, msg)
		}
		return fmt.Errorf("remove DNS leak firewall rule %s: %w", name, err)
	}
	return nil
}

func isFirewallRuleMissingMessage(message string) bool {
	clean := strings.ToLower(strings.TrimSpace(message))
	if clean == "" {
		return false
	}
	return strings.Contains(clean, "no rules match") ||
		strings.Contains(clean, "no rule matches") ||
		(strings.Contains(clean, "没有") && strings.Contains(clean, "规则")) ||
		(strings.Contains(clean, "找不到") && strings.Contains(clean, "规则"))
}

func (s *Session) configureDNS() error {
	v4Set := false
	v6Set := false
	for _, dns := range s.config.DNSServers {
		dns = strings.TrimSpace(dns)
		if dns == "" {
			continue
		}
		if strings.Contains(dns, ":") {
			if !s.config.EnableIPv6 {
				continue
			}
			if !v6Set {
				if err := run("netsh", "interface", "ipv6", "set", "dnsservers", "name="+interfaceName, "static", dns, "primary"); err != nil {
					return fmt.Errorf("configure VPN IPv6 DNS: %w", err)
				}
				v6Set = true
			} else if err := run("netsh", "interface", "ipv6", "add", "dnsservers", "name="+interfaceName, "address="+dns); err != nil {
				return fmt.Errorf("add VPN IPv6 DNS: %w", err)
			}
			continue
		}
		if !v4Set {
			if err := run("netsh", "interface", "ipv4", "set", "dnsservers", "name="+interfaceName, "static", dns, "primary", "validate=no"); err != nil {
				return fmt.Errorf("configure VPN DNS: %w", err)
			}
			v4Set = true
		} else if err := run("netsh", "interface", "ipv4", "add", "dnsservers", "name="+interfaceName, "address="+dns, "validate=no"); err != nil {
			return fmt.Errorf("add VPN DNS: %w", err)
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
		route, err := currentRouteTo(addr.String())
		if err != nil {
			return fmt.Errorf("find route for VPN bypass %s: %w", addr, err)
		}
		if addr.Is6() {
			if !s.config.EnableIPv6 {
				continue
			}
			args := []string{"interface", "ipv6", "add", "route", addr.String() + "/128", "interface=" + route.ifIndex, "metric=1", "store=active"}
			if route.nextHop != "" && route.nextHop != "::" {
				args = append(args, "nexthop="+route.nextHop)
			}
			if err := run("netsh", args...); err != nil {
				return fmt.Errorf("add VPN IPv6 bypass route %s: %w", addr, err)
			}
			continue
		}
		nextHop := route.nextHop
		if nextHop == "" {
			nextHop = "0.0.0.0"
		}
		if err := run("route", "add", addr.String(), "mask", "255.255.255.255", nextHop, "metric", "1", "IF", route.ifIndex); err != nil {
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
		if strings.Contains(cidr, ":") {
			if !s.config.EnableIPv6 {
				continue
			}
			if err := run("netsh", "interface", "ipv6", "add", "route", cidr, "interface="+interfaceName, "metric="+fmt.Sprint(s.config.RouteMetric), "publish=no"); err != nil {
				return fmt.Errorf("add VPN IPv6 route %s: %w", cidr, err)
			}
			continue
		}
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil || ip == nil {
			return fmt.Errorf("invalid VPN route %s", cidr)
		}
		if err := run("netsh", "interface", "ipv4", "add", "route", "prefix="+cidr, "interface="+interfaceName, "nexthop=0.0.0.0", "metric="+fmt.Sprint(s.config.RouteMetric), "store=active"); err != nil {
			return fmt.Errorf("add VPN route %s: %w", cidr, err)
		}
	}
	return nil
}

func (s *Session) cleanupRoutes() {
	for _, ip := range s.config.BypassIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || !addr.IsValid() {
			continue
		}
		if addr.Is6() {
			_ = run("netsh", "interface", "ipv6", "delete", "route", addr.String()+"/128")
			continue
		}
		_ = run("route", "delete", addr.String(), "mask", "255.255.255.255")
	}
	for _, cidr := range s.config.Routes {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if strings.Contains(cidr, ":") {
			_ = run("netsh", "interface", "ipv6", "delete", "route", cidr, "interface="+interfaceName)
			continue
		}
		if ip, _, err := net.ParseCIDR(cidr); err == nil && ip != nil {
			_ = run("netsh", "interface", "ipv4", "delete", "route", "prefix="+cidr, "interface="+interfaceName, "nexthop=0.0.0.0")
		}
	}
}

func findWintunDLL() (string, error) {
	const name = "wintun.dll"
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate gonc-gui.exe: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", fmt.Errorf("locate gonc-gui.exe: %w", err)
	}
	candidate := filepath.Join(filepath.Dir(exe), name)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("Wintun runtime %s was not found beside gonc-gui.exe; copy wintun.dll to the application directory", name)
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

func currentRouteTo(ip string) (routeTarget, error) {
	script := "$r = Find-NetRoute -RemoteIPAddress '" + strings.ReplaceAll(ip, "'", "''") + "' | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1; if ($null -eq $r) { exit 2 }; Write-Output ([string]$r.InterfaceIndex + ',' + [string]$r.NextHop)"
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return routeTarget{}, err
	}
	parts := strings.SplitN(strings.TrimSpace(string(output)), ",", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return routeTarget{}, fmt.Errorf("route not found")
	}
	route := routeTarget{ifIndex: strings.TrimSpace(parts[0])}
	if len(parts) > 1 {
		route.nextHop = strings.TrimSpace(parts[1])
	}
	return route, nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
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
