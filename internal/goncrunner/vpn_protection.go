package goncrunner

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/threatexpert/gonc/v2/netx"
)

type vpnProtection struct {
	captured atomic.Bool
	released chan struct{}
	once     sync.Once
}

func newVPNProtection() *vpnProtection { return &vpnProtection{released: make(chan struct{})} }
func (p *vpnProtection) release()      { p.once.Do(func() { close(p.released) }) }

type ProtectionPlan struct {
	DNS             string
	Local           string
	Overrides       []string
	requestedSource string
}

// PlanProtection validates the final values before elevation or network changes.
// Advanced arguments take precedence; the app must confirm any such override.
func PlanProtection(req Request) (ProtectionPlan, error) {
	plan := ProtectionPlan{DNS: req.ReconnectDNS}
	args := splitExtraArgs(req.ExtraArgs)
	dnsOverride, localOverride := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name != "dns" && name != "local" {
			continue
		}
		if !hasValue {
			i++
			if i >= len(args) {
				return plan, fmt.Errorf("-%s requires a value", name)
			}
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return plan, fmt.Errorf("-%s cannot be empty with disconnect protection", name)
		}
		if name == "dns" {
			plan.DNS = value
			dnsOverride = true
		} else {
			plan.Local = value
			localOverride = true
		}
	}
	var err error
	plan.DNS, err = reconnectDNSServer(plan.DNS)
	if err != nil {
		return plan, err
	}
	binding, err := netx.ParseLocalBinding(plan.Local)
	if err != nil {
		return plan, err
	}
	plan.requestedSource = binding.Source()
	if binding.Endpoint != "" {
		host, port, e := net.SplitHostPort(plan.Local)
		ip, ie := netip.ParseAddr(host)
		p, pe := strconv.Atoi(port)
		if e != nil || ie != nil || pe != nil || p < 0 || p > 65535 || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsLoopback() {
			return plan, fmt.Errorf("protected -local requires a local interface IP and port, for example 192.168.1.2:0")
		}
		plan.Local = net.JoinHostPort(ip.String(), strconv.Itoa(p))
		dnsHost, _, _ := net.SplitHostPort(plan.DNS)
		if ip.Is4() != (net.ParseIP(dnsHost).To4() != nil) {
			return plan, fmt.Errorf("-local and reconnect DNS must use the same IP family")
		}
	}
	if localOverride {
		plan.Overrides = append(plan.Overrides, "-local "+plan.Local)
	}
	if dnsOverride {
		plan.Overrides = append(plan.Overrides, "-dns "+plan.DNS)
	}
	return plan, nil
}

func reconnectDNSServer(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "8.8.8.8"
	}
	if ip, err := netip.ParseAddr(strings.Trim(value, "[]")); err == nil && ip.Zone() == "" && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLoopback() {
		return net.JoinHostPort(ip.String(), "53"), nil
	}
	host, port, err := net.SplitHostPort(value)
	ip, ipErr := netip.ParseAddr(host)
	p, portErr := strconv.Atoi(port)
	if err != nil || ipErr != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() || portErr != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("reconnect DNS must be a reachable DNS server IP with an optional port (not a hostname or loopback address)")
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(p)), nil
}

func (p ProtectionPlan) localArgument(selected string) string {
	if p.Local != "" {
		return p.Local
	}
	return selected
}

// goncembed accepts adjacent quoted segments, without backslash escapes.
func quoteLocalArg(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `"'"'"`) + `"`
}
