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

	"gonc-gui/internal/dnsproxy"
	"gonc-gui/internal/vpnconfig"

	"github.com/xjasonlyu/tun2socks/v2/engine"
	"golang.org/x/sys/windows"
)

const (
	interfaceName      = "Gonc"
	interfaceIPv4      = "10.60.173.33"
	interfaceMask      = "255.255.255.0"
	dnsProxyIPv4       = "127.60.173.33"
	defaultMTU         = 1400
	defaultRouteMetric = 1
	dnsLeakRuleUDPLow  = "Gonc VPN DNS Leak Protection Dnscache UDP 53 non-VPN local low"
	dnsLeakRuleUDPHigh = "Gonc VPN DNS Leak Protection Dnscache UDP 53 non-VPN local high"
	dnsLeakRuleTCPLow  = "Gonc VPN DNS Leak Protection Dnscache TCP 53 non-VPN local low"
	dnsLeakRuleTCPHigh = "Gonc VPN DNS Leak Protection Dnscache TCP 53 non-VPN local high"
	dnsLeakRuleUDPIPv6 = "Gonc VPN DNS Leak Protection Dnscache UDP 53 IPv6"
	dnsLeakRuleTCPIPv6 = "Gonc VPN DNS Leak Protection Dnscache TCP 53 IPv6"
)

type Session struct {
	mu       sync.Mutex
	config   vpnconfig.Config
	dnsProxy *dnsproxy.Server
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
	return StartWithTrace(config, nil)
}

func StartWithTrace(config vpnconfig.Config, trace func(string)) (*Session, error) {
	started := time.Now()
	defer func() {
		traceVPNStep(trace, "total", started)
	}()
	if strings.TrimSpace(config.SOCKS5Endpoint) == "" {
		return nil, fmt.Errorf("SOCKS5 endpoint is required")
	}
	step := time.Now()
	if err := CheckRuntime(); err != nil {
		return nil, err
	}
	traceVPNStep(trace, "check Wintun runtime", step)
	if config.MTU <= 0 {
		config.MTU = defaultMTU
	}
	config.RouteMetric = normalizeRouteMetric(config.RouteMetric)
	if config.LogLevel == "" {
		config.LogLevel = "warn"
	}
	session := &Session{config: config}
	step = time.Now()
	if err := session.configureDNSLeakProtection(trace); err != nil {
		return nil, err
	}
	traceVPNStep(trace, "configure DNS leak protection", step)
	step = time.Now()
	engine.Insert(&engine.Key{
		Device:   "tun://" + interfaceName,
		Proxy:    config.SOCKS5Endpoint,
		LogLevel: config.LogLevel,
		MTU:      config.MTU,
	})
	traceVPNStep(trace, "insert tun2socks engine", step)
	step = time.Now()
	engine.Start()
	traceVPNStep(trace, "start tun2socks engine", step)
	if err := session.configure(trace); err != nil {
		_ = session.Stop()
		return nil, err
	}
	return session, nil
}

func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRoutes()
	if s.dnsProxy != nil {
		_ = s.dnsProxy.Close()
		s.dnsProxy = nil
	}
	err := cleanupDNSLeakFirewallRules()
	engine.Stop()
	return err
}

func (s *Session) configure(trace func(string)) error {
	step := time.Now()
	if err := waitInterface(interfaceName, 8*time.Second); err != nil {
		return err
	}
	traceVPNStep(trace, "wait interface", step)
	step = time.Now()
	if err := run("netsh", "interface", "ipv4", "set", "address", "name="+interfaceName, "static", interfaceIPv4, interfaceMask); err != nil {
		return fmt.Errorf("configure VPN IPv4 address: %w", err)
	}
	traceVPNStep(trace, "configure IPv4 address", step)
	if s.config.EnableIPv6 {
		step = time.Now()
		if err := s.configureIPv6Address(); err != nil {
			return err
		}
		traceVPNStep(trace, "configure IPv6 address", step)
	}
	step = time.Now()
	if err := s.configureInterfaceMetric(); err != nil {
		return err
	}
	traceVPNStep(trace, "configure interface metric", step)
	step = time.Now()
	dnsProxyReady := true
	if err := s.startDNSProxy(trace); err != nil {
		if s.config.BlockDNSLeak {
			return err
		}
		dnsProxyReady = false
		traceVPNMessage(trace, "System VPN warning: DNS proxy unavailable; falling back to direct DNS servers: "+err.Error())
	}
	traceVPNStep(trace, "start DNS proxy", step)
	step = time.Now()
	if err := s.configureDNS(dnsProxyReady); err != nil {
		return err
	}
	traceVPNStep(trace, "configure DNS", step)
	step = time.Now()
	traceVPNMessage(trace, "System VPN step: configure bypass routes skipped on Windows")
	step = time.Now()
	if err := s.configureRoutes(); err != nil {
		return err
	}
	traceVPNStep(trace, "configure VPN routes", step)
	return nil
}

func traceVPNStep(trace func(string), name string, started time.Time) {
	if trace == nil {
		return
	}
	trace(fmt.Sprintf("System VPN step: %s completed in %s", name, time.Since(started).Round(time.Millisecond)))
}

func traceVPNMessage(trace func(string), message string) {
	if trace == nil {
		return
	}
	trace(message)
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

func (s *Session) configureIPv6Address() error {
	_ = run("netsh", "interface", "ipv6", "delete", "address", "interface="+interfaceName, "address=fd00::2")
	_ = run("netsh", "interface", "ipv6", "delete", "address", "interface="+interfaceName, "address=fd60:173:33::2")
	if err := run("netsh", "interface", "ipv6", "add", "address", "interface="+interfaceName, "address=fd60:173:33::2/128"); err != nil {
		return fmt.Errorf("configure VPN IPv6 address: %w", err)
	}
	return nil
}

func (s *Session) configureDNSLeakProtection(trace func(string)) error {
	if !s.config.BlockDNSLeak {
		step := time.Now()
		err := cleanupDNSLeakFirewallRules()
		traceVPNStep(trace, "cleanup DNS leak protection rules", step)
		return err
	}
	rules := []struct {
		name     string
		action   string
		protocol string
		remoteIP string
	}{
		{dnsLeakRuleUDPLow, "block", "UDP", "0.0.0.0-126.255.255.255"},
		{dnsLeakRuleUDPHigh, "block", "UDP", "128.0.0.0-255.255.255.255"},
		{dnsLeakRuleTCPLow, "block", "TCP", "0.0.0.0-126.255.255.255"},
		{dnsLeakRuleTCPHigh, "block", "TCP", "128.0.0.0-255.255.255.255"},
		{dnsLeakRuleUDPIPv6, "block", "UDP", "::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		{dnsLeakRuleTCPIPv6, "block", "TCP", "::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
	}

	type ruleResult struct {
		index    int
		action   string
		protocol string
		remoteIP string
		elapsed  time.Duration
		err      error
	}
	results := make(chan ruleResult, len(rules))
	for index, rule := range rules {
		index, rule := index, rule
		go func() {
			step := time.Now()
			err := ensureDNSLeakFirewallRule(rule.name, rule.action, rule.protocol, rule.remoteIP)
			results <- ruleResult{
				index:    index,
				action:   rule.action,
				protocol: rule.protocol,
				remoteIP: rule.remoteIP,
				elapsed:  time.Since(step),
				err:      err,
			}
		}()
	}
	ordered := make([]ruleResult, len(rules))
	var firstErr error
	for range rules {
		result := <-results
		ordered[result.index] = result
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}
	}
	for _, result := range ordered {
		traceVPNMessage(trace, fmt.Sprintf(
			"System VPN step: ensure DNS leak rule %s %s remote %s completed in %s",
			result.action,
			result.protocol,
			result.remoteIP,
			result.elapsed.Round(time.Millisecond),
		))
	}
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func ensureDNSLeakFirewallRule(name string, action string, protocol string, remoteIP string) error {
	_ = removeDNSLeakFirewallRule(name)
	return addDNSLeakFirewallRule(name, action, protocol, remoteIP)
}

func addDNSLeakFirewallRule(name string, action string, protocol string, remoteIP string) error {
	if err := run("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+name,
		"dir=out",
		"action="+action,
		"protocol="+protocol,
		"remoteip="+remoteIP,
		"remoteport=53",
		"service=Dnscache",
		"profile=any",
		"enable=yes"); err != nil {
		return fmt.Errorf("add DNS leak firewall rule %s: %w", name, err)
	}
	return nil
}

func cleanupDNSLeakFirewallRules() error {
	for _, name := range []string{
		dnsLeakRuleUDPLow,
		dnsLeakRuleUDPHigh,
		dnsLeakRuleTCPLow,
		dnsLeakRuleTCPHigh,
		dnsLeakRuleUDPIPv6,
		dnsLeakRuleTCPIPv6,
		"Gonc VPN DNS Leak Protection Dnscache UDP 53 loopback DNS proxy allow",
		"Gonc VPN DNS Leak Protection Dnscache TCP 53 loopback DNS proxy allow",
	} {
		_ = removeDNSLeakFirewallRule(name)
	}
	return nil
}

func removeDNSLeakFirewallRule(name string) error {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+name)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
	return nil
}

func (s *Session) configureDNS(useProxy bool) error {
	if useProxy {
		if err := run("netsh", "interface", "ipv4", "set", "dnsservers", "name="+interfaceName, "static", dnsProxyIPv4, "primary", "validate=no"); err != nil {
			return fmt.Errorf("configure VPN DNS proxy: %w", err)
		}
		_ = run("netsh", "interface", "ipv6", "delete", "dnsservers", "name="+interfaceName, "all")
		return nil
	}
	ipv4, ipv6 := directDNSServers(s.config.DNSServers, s.config.EnableIPv6)
	if err := configureWindowsDNSServers("ipv4", ipv4); err != nil {
		return fmt.Errorf("configure VPN direct IPv4 DNS: %w", err)
	}
	if s.config.EnableIPv6 && len(ipv6) > 0 {
		if err := configureWindowsDNSServers("ipv6", ipv6); err != nil {
			return fmt.Errorf("configure VPN direct IPv6 DNS: %w", err)
		}
	} else {
		_ = run("netsh", "interface", "ipv6", "delete", "dnsservers", "name="+interfaceName, "all")
	}
	return nil
}

func (s *Session) startDNSProxy(trace func(string)) error {
	proxy, err := dnsproxy.StartWithTrace(dnsProxyIPv4, s.config.SOCKS5Endpoint, s.config.DNSServers, trace)
	if err != nil {
		return fmt.Errorf("start DNS proxy: %w", err)
	}
	s.dnsProxy = proxy
	return nil
}

func configureWindowsDNSServers(family string, servers []string) error {
	if len(servers) == 0 {
		return run("netsh", "interface", family, "delete", "dnsservers", "name="+interfaceName, "all")
	}
	if err := run("netsh", "interface", family, "set", "dnsservers", "name="+interfaceName, "static", servers[0], "primary", "validate=no"); err != nil {
		return err
	}
	for index, server := range servers[1:] {
		if err := run("netsh", "interface", family, "add", "dnsservers", "name="+interfaceName, "address="+server, "index="+fmt.Sprint(index+2), "validate=no"); err != nil {
			return err
		}
	}
	return nil
}

func directDNSServers(values []string, enableIPv6 bool) ([]string, []string) {
	var ipv4 []string
	var ipv6 []string
	seen := map[string]struct{}{}
	for _, value := range values {
		addr, ok := directDNSServer(value)
		if !ok {
			continue
		}
		if _, exists := seen[addr.String()]; exists {
			continue
		}
		seen[addr.String()] = struct{}{}
		if addr.Is4() {
			ipv4 = append(ipv4, addr.String())
			continue
		}
		if enableIPv6 {
			ipv6 = append(ipv6, addr.String())
		}
	}
	if len(ipv4) == 0 {
		ipv4 = append(ipv4, "8.8.8.8")
	}
	if enableIPv6 && len(ipv6) == 0 {
		ipv6 = append(ipv6, "2001:4860:4860::8888")
	}
	return ipv4, ipv6
}

func directDNSServer(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.IsValid() || addr.IsUnspecified() {
		return netip.Addr{}, false
	}
	return addr, true
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
