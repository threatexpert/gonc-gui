package winvpn

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"

	"gonc-gui/internal/dnsproxy"
	"gonc-gui/internal/vpnconfig"
)

type linuxDNSCapture struct {
	listeners []*dnsproxy.Server
	undo      []firewallCommand
}
type firewallCommand struct {
	tool string
	args []string
}

func (s *Session) installDNSCapture(servers []string) error {
	if err := s.cleanupDNSCaptures(); err != nil {
		return err
	}
	next := new(linuxDNSCapture)
	if err := next.start(s.config, servers); err != nil {
		s.retired = append(s.retired, next)
		return errors.Join(err, s.cleanupDNSCaptures())
	}
	if s.capture != nil {
		s.retired = append(s.retired, s.capture)
	}
	s.capture = next
	s.config.DNSServers = append([]string(nil), servers...)
	s.appliedDNS = next.listeners[0].UpstreamServers()
	return s.cleanupDNSCaptures()
}
func (s *Session) cleanupDNSCaptures() error {
	var failures error
	pending := s.retired[:0]
	for _, capture := range s.retired {
		if err := capture.close(); err != nil {
			pending = append(pending, capture)
			failures = errors.Join(failures, err)
		}
	}
	s.retired = pending
	return failures
}
func (s *Session) updateDNSProxy(endpoint string) error {
	if s.capture == nil {
		return nil
	}
	for i, proxy := range s.capture.listeners {
		if err := proxy.UpdateProxy(endpoint); err != nil {
			for _, updated := range s.capture.listeners[:i] {
				_ = updated.UpdateProxy(s.config.SOCKS5Endpoint)
			}
			return err
		}
	}
	return nil
}

type dnsFamily struct{ tool, ip string }

func checkIPTables(families []dnsFamily) error {
	for _, f := range families {
		if _, err := exec.LookPath(f.tool); err != nil {
			return fmt.Errorf("iptables fallback requires %s: %w", f.tool, err)
		}
	}
	return nil
}

func captureFamilies() ([]dnsFamily, error) {
	families := []dnsFamily{{"iptables", "127.0.0.1"}}
	// IPv6 LAN DNS may be reachable even when the VPN only routes IPv4.
	if data, err := os.ReadFile("/proc/net/if_inet6"); err == nil && len(data) > 0 {
		families = append(families, dnsFamily{"ip6tables", "::1"})
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := exec.LookPath("nft"); err != nil {
		if fallbackErr := checkIPTables(families); fallbackErr != nil {
			return nil, errors.Join(fmt.Errorf("Linux DNS leak protection: %w", err), fallbackErr)
		}
	}
	return families, nil
}
func (c *linuxDNSCapture) start(cfg vpnconfig.Config, servers []string) error {
	if cfg.DisconnectProtection && (cfg.LinuxDNSMark == 0 || cfg.LinuxDNSDevice == "" || cfg.LinuxDNSBypass == "") {
		return errors.New("missing reconnect DNS socket binding")
	}
	families, err := captureFamilies()
	if err != nil {
		return err
	}
	var id [6]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	chain := fmt.Sprintf("GONC_DNS_%x", id)
	for _, family := range families {
		proxy, err := dnsproxy.StartLoopback(family.ip, cfg.SOCKS5Endpoint, servers)
		if err != nil {
			return err
		}
		c.listeners = append(c.listeners, proxy)
	}
	ports := make([]int, len(c.listeners))
	for i, listener := range c.listeners {
		ports[i] = listener.Port()
	}
	return c.installRules(families, chain, ports, cfg)
}

func (c *linuxDNSCapture) installRules(families []dnsFamily, chain string, ports []int, cfg vpnconfig.Config) error {
	var nftErr error
	if nft, err := exec.LookPath("nft"); err == nil {
		if nftErr = c.installNFT(nft, families, chain, ports, cfg); nftErr == nil {
			return nil
		}
		// The nft batch is atomic: a rejected transaction installed no rules.
	}
	if err := checkIPTables(families); err != nil {
		return errors.Join(nftErr, err)
	}
	for i, family := range families {
		if err := c.installFamilyRules(family.tool, chain, ports[i], cfg); err != nil {
			return errors.Join(nftErr, err)
		}
	}
	return nil
}
func (c *linuxDNSCapture) installFamilyRules(tool, chain string, listenerPort int, cfg vpnconfig.Config) error {
	host, port, _ := net.SplitHostPort(cfg.LinuxDNSBypass)
	v4 := tool == "iptables"
	for _, table := range []string{"filter", "nat"} {
		if err := c.rule(tool, table, "-N", chain); err != nil {
			return err
		}
		for _, proto := range []string{"udp", "tcp"} {
			if cfg.LinuxDNSMark != 0 && (net.ParseIP(host).To4() != nil) == v4 {
				// RETURN from our chain continues processing the user's firewall.
				if err := c.rule(tool, table, "-A", chain, "-p", proto, "--dport", port, "-d", host, "-o", cfg.LinuxDNSDevice, "-m", "mark", "--mark", fmt.Sprintf("0x%x", cfg.LinuxDNSMark), "-j", "RETURN"); err != nil {
					return err
				}
			}
			args := []string{chain, "-p", proto, "--dport", "53"}
			if table == "nat" {
				args = append(args, "-j", "REDIRECT", "--to-ports", strconv.Itoa(listenerPort))
			} else {
				// Local redirects reach loopback. Existing conntrack flows that
				// retain their old physical destination must still be stopped.
				args = append(args, "!", "-o", "lo", "-j", "REJECT")
			}
			if err := c.rule(tool, table, "-A", args...); err != nil {
				return err
			}
		}
		if err := c.rule(tool, table, "-I", "OUTPUT", "-j", chain); err != nil {
			return err
		}
	}
	return nil
}

func (c *linuxDNSCapture) rule(tool, table, op string, args ...string) error {
	cmd := append([]string{"-w", "3", "-t", table, op}, args...)
	if err := run(tool, cmd...); err != nil {
		return err
	}
	inverse := "-D"
	if op == "-N" {
		inverse = "-X"
	}
	undo := append([]string{"-w", "3", "-t", table, inverse}, args...)
	c.undo = append(c.undo, firewallCommand{tool, undo})
	return nil
}
func (c *linuxDNSCapture) close() error {
	for len(c.undo) > 0 {
		i := len(c.undo) - 1
		cmd := c.undo[i]
		if err := run(cmd.tool, cmd.args...); err != nil {
			return err
		}
		c.undo = c.undo[:i]
	}
	var err error
	for _, proxy := range c.listeners {
		err = errors.Join(err, proxy.Close())
	}
	c.listeners = nil
	return err
}
