package winvpn

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

// EgressGuard belongs to the elevated helper, not to a tunnel attempt.
// Removing capture routes must never remove this chain.
type EgressGuard struct {
	undo  []firewallCommand
	ready bool
}

func (g *EgressGuard) Ready() bool { return g.ready }

func InstallEgressGuard(mark uint32) (*EgressGuard, error) {
	if mark == 0 {
		return nil, errors.New("missing VPN egress mark")
	}
	name := fmt.Sprintf("GONC_OUT_%08x", mark)
	g := new(EgressGuard)
	var nftErr error
	if nft, err := exec.LookPath("nft"); err == nil {
		// One transaction covers both families, including a newly added interface.
		rules := fmt.Sprintf(`create table inet %s
add chain inet %s output { type filter hook output priority 10; policy accept; }
add rule inet %s output oifname { "lo", "%s" } return
add rule inet %s output meta mark 0x%x return
add rule inet %s output meta skuid 0 meta nfproto ipv4 udp sport 68 udp dport 67 return
add rule inet %s output meta skuid 0 meta nfproto ipv6 udp sport 546 udp dport 547 return
add rule inet %s output ip6 hoplimit 255 icmpv6 type { 133, 135, 136 } return
add rule inet %s output counter drop
`, name, name, name, interfaceName, name, mark, name, name, name, name)
		cmd := exec.Command(nft, "-f", "-")
		cmd.Stdin = strings.NewReader(rules)
		if out, err := cmd.CombinedOutput(); err == nil {
			g.undo = append(g.undo, firewallCommand{nft, []string{"delete", "table", "inet", name}})
			g.ready = true
			return g, nil
		} else {
			nftErr = fmt.Errorf("install nft egress guard: %w: %s", err, out)
		}
	}
	// No NAT is required. Build each chain completely before linking OUTPUT.
	// Cover IPv6 even if no interface currently has an IPv6 address. Only
	// skip that backend when the kernel does not implement the address family.
	tools := []string{"iptables", "ip6tables"}
	fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if errors.Is(err, unix.EAFNOSUPPORT) {
		tools = tools[:1]
	} else if err == nil {
		_ = unix.Close(fd)
	} else {
		return nil, errors.Join(nftErr, err)
	}
	for _, tool := range tools {
		if err := g.installXT(tool, name, mark); err != nil {
			return g, errors.Join(nftErr, err, g.Close())
		}
	}
	g.ready = true
	return g, nil
}

func (g *EgressGuard) installXT(tool, name string, mark uint32) error {
	command := func(args ...string) error { return run(tool, append([]string{"-w", "3"}, args...)...) }
	if err := command("-N", name); err != nil {
		return err
	}
	g.undo = append(g.undo, firewallCommand{tool, []string{"-w", "3", "-X", name}},
		firewallCommand{tool, []string{"-w", "3", "-F", name}})
	rules := [][]string{{"-o", "lo"}, {"-o", interfaceName}, {"-m", "mark", "--mark", fmt.Sprintf("0x%x", mark)}}
	if tool == "iptables" {
		rules = append(rules, []string{"-m", "owner", "--uid-owner", "0", "-p", "udp", "--sport", "68", "--dport", "67"})
	} else {
		rules = append(rules, []string{"-m", "owner", "--uid-owner", "0", "-p", "udp", "--sport", "546", "--dport", "547"})
		for _, kind := range []string{"133", "135", "136"} {
			rules = append(rules, []string{"-p", "ipv6-icmp", "--icmpv6-type", kind, "-m", "hl", "--hl-eq", "255"})
		}
	}
	for _, match := range rules {
		args := append([]string{"-A", name}, match...)
		if err := command(append(args, "-j", "RETURN")...); err != nil {
			return err
		}
	}
	if err := command("-A", name, "-j", "DROP"); err != nil {
		return err
	}
	if err := command("-I", "OUTPUT", "1", "-j", name); err != nil {
		return err
	}
	g.undo = append(g.undo, firewallCommand{tool, []string{"-w", "3", "-D", "OUTPUT", "-j", name}})
	return nil
}

func (g *EgressGuard) Close() error {
	if g == nil {
		return nil
	}
	g.ready = false
	return undoCommands(&g.undo)
}

func undoCommands(commands *[]firewallCommand) error {
	for len(*commands) > 0 {
		i := len(*commands) - 1
		cmd := (*commands)[i]
		if err := run(cmd.tool, cmd.args...); err != nil {
			return err
		}
		*commands = (*commands)[:i]
	}
	return nil
}
