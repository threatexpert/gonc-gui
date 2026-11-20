package winvpn

import (
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"

	"gonc-gui/internal/vpnconfig"
)

// Each address family gets its own table so IPv4-only hosts and kernels
// predating inet-family NAT can use the same transaction.
func (c *linuxDNSCapture) installNFT(nft string, families []dnsFamily, table string, ports []int, cfg vpnconfig.Config) error {
	var bypass netip.AddrPort
	var deviceMatch string
	if cfg.LinuxDNSMark != 0 {
		addr, err := netip.ParseAddrPort(cfg.LinuxDNSBypass)
		if err != nil {
			return fmt.Errorf("invalid reconnect DNS: %w", err)
		}
		iface, err := net.InterfaceByName(cfg.LinuxDNSDevice)
		if err != nil {
			return fmt.Errorf("find reconnect DNS interface: %w", err)
		}
		bypass = addr
		// Follow a recreated device of the same name. Use its numeric index
		// when the name contains nft syntax, including wildcard characters.
		if strings.Trim(iface.Name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.:-") == "" {
			deviceMatch = fmt.Sprintf("oifname %q", iface.Name)
		} else {
			deviceMatch = fmt.Sprintf("meta oif %d", iface.Index)
		}
	}
	var script strings.Builder
	var cleanup []firewallCommand
	for i, family := range families {
		af := "ip"
		ipv4 := family.tool == "iptables"
		if !ipv4 {
			af = "ip6"
		}
		target := af + " " + table
		fmt.Fprintf(&script, "create table %s\n", target)
		fmt.Fprintf(&script, "add chain %s capture { type nat hook output priority -101; policy accept; }\n", target)
		fmt.Fprintf(&script, "add chain %s guard { type filter hook output priority 0; policy accept; }\n", target)
		for _, proto := range []string{"udp", "tcp"} {
			if bypass.IsValid() && bypass.Addr().Unmap().Is4() == ipv4 {
				for _, chain := range []string{"capture", "guard"} {
					fmt.Fprintf(&script, "add rule %s %s meta mark 0x%x %s %s daddr %s %s dport %d return\n", target, chain, cfg.LinuxDNSMark, deviceMatch, af, bypass.Addr().Unmap(), proto, bypass.Port())
				}
			}
			fmt.Fprintf(&script, "add rule %s capture %s dport 53 redirect to :%d\n", target, proto, ports[i])
			fmt.Fprintf(&script, "add rule %s guard oifname != \"lo\" %s dport 53 reject\n", target, proto)
		}
		cleanup = append(cleanup, firewallCommand{nft, []string{"delete", "table", af, table}})
	}
	cmd := exec.Command(nft, "-f", "-")
	cmd.Stdin = strings.NewReader(script.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("install nft DNS rules: %w: %s", err, strings.TrimSpace(string(output)))
	}
	c.undo = append(c.undo, cleanup...)
	return nil
}
