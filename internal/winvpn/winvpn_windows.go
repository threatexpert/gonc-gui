//go:build windows

package winvpn

import (
	"fmt"
	"net"
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
	interfaceName = "Gonc"
	defaultMTU    = 1400
)

type Session struct {
	mu     sync.Mutex
	config vpnconfig.Config
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
	engine.Stop()
	return nil
}

func (s *Session) configure() error {
	if err := waitInterface(interfaceName, 8*time.Second); err != nil {
		return err
	}
	if err := run("netsh", "interface", "ipv4", "set", "address", "name="+interfaceName, "static", "10.0.0.2", "255.255.255.0"); err != nil {
		return fmt.Errorf("configure VPN IPv4 address: %w", err)
	}
	if s.config.EnableIPv6 {
		_ = run("netsh", "interface", "ipv6", "delete", "address", "interface="+interfaceName, "address=fd00::2")
		if err := run("netsh", "interface", "ipv6", "add", "address", "interface="+interfaceName, "address=fd00::2/128"); err != nil {
			return fmt.Errorf("configure VPN IPv6 address: %w", err)
		}
	}
	if err := s.configureDNS(); err != nil {
		return err
	}
	if err := s.configureRoutes(); err != nil {
		return err
	}
	return nil
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
			if err := run("netsh", "interface", "ipv6", "add", "route", cidr, "interface="+interfaceName, "publish=no"); err != nil {
				return fmt.Errorf("add VPN IPv6 route %s: %w", cidr, err)
			}
			continue
		}
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil || ip == nil {
			return fmt.Errorf("invalid VPN route %s", cidr)
		}
		if err := run("netsh", "interface", "ipv4", "add", "route", "prefix="+cidr, "interface="+interfaceName, "nexthop=0.0.0.0", "metric=5", "store=active"); err != nil {
			return fmt.Errorf("add VPN route %s: %w", cidr, err)
		}
	}
	return nil
}

func (s *Session) cleanupRoutes() {
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
