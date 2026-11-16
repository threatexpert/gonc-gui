//go:build windows

package winvpn

import (
	"errors"
	"fmt"
	"net/netip"
	"runtime"
	"strings"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

var dnsLeakRuleNames = []string{
	dnsLeakRuleUDPLow, dnsLeakRuleUDPHigh, dnsLeakRuleTCPLow, dnsLeakRuleTCPHigh,
	dnsLeakRuleUDPIPv6, dnsLeakRuleTCPIPv6,
	"Gonc VPN DNS Leak Protection Dnscache UDP 53 loopback DNS proxy allow",
	"Gonc VPN DNS Leak Protection Dnscache TCP 53 loopback DNS proxy allow",
}

func cleanupDNSLeakFirewallRules() error          { return removeFirewallRules(dnsLeakRuleNames) }
func removeDNSLeakFirewallRule(name string) error { return removeFirewallRules([]string{name}) }

// The policy and all objects derived from it stay on one initialized COM thread.
// COM firewall rules are supported on every Windows version we support.
func withFirewallRules(fn func(*ole.IDispatch) error) error {
	return withFirewallRulesTrace(fn, nil)
}

func withFirewallRulesTrace(fn func(*ole.IDispatch) error, trace func(string, time.Duration)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	step := time.Now()
	report := func(name string) {
		if trace != nil {
			trace(name, time.Since(step))
		}
		step = time.Now()
	}
	err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	if err == nil || comErrorCode(err) == 1 { // S_FALSE is successful initialization too.
		defer ole.CoUninitialize()
	} else if comErrorCode(err) != 0x80010106 { // RPC_E_CHANGED_MODE: already initialized, don't uninitialize it.
		return fmt.Errorf("initialize firewall COM: %w", err)
	}
	report("COM initialize")
	object, err := oleutil.CreateObject("HNetCfg.FwPolicy2")
	if err != nil {
		return fmt.Errorf("open firewall policy: %w", err)
	}
	defer object.Release()
	policy, err := object.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer policy.Release()
	report("create firewall policy")
	value, err := oleutil.GetProperty(policy, "Rules")
	if value != nil {
		defer value.Clear()
	}
	if err != nil {
		return err
	}
	if value == nil || value.ToIDispatch() == nil {
		return fmt.Errorf("firewall Rules collection is unavailable")
	}
	report("get rules collection")
	err = fn(value.ToIDispatch())
	report("rule operations")
	return err
}

func comErrorCode(err error) uint32 {
	var comErr *ole.OleError
	if errors.As(err, &comErr) {
		if uint32(comErr.Code()) == 0x80020009 { // DISP_E_EXCEPTION wraps the original HRESULT.
			if sub, ok := comErr.SubError().(interface{ SCODE() uint32 }); ok && sub.SCODE() != 0 {
				return sub.SCODE()
			}
		}
		return uint32(comErr.Code())
	}
	return 0
}

func firewallRuleExists(rules *ole.IDispatch, name string) (bool, error) {
	value, err := oleutil.CallMethod(rules, "Item", name)
	if value != nil {
		defer value.Clear()
	}
	if comErrorCode(err) == 0x80070002 {
		return false, nil
	} // HRESULT_FROM_WIN32(ERROR_FILE_NOT_FOUND)
	if err != nil {
		return false, err
	}
	if value == nil || value.ToIDispatch() == nil {
		return false, fmt.Errorf("firewall Item returned no rule")
	}
	return true, nil
}

func removeFirewallRules(names []string) error {
	return withFirewallRules(func(rules *ole.IDispatch) error {
		return removeFirewallRulesFrom(rules, names)
	})
}

func removeFirewallRulesFrom(rules *ole.IDispatch, names []string) error {
	return removeFirewallRulesWith(names,
		func(name string) (bool, error) { return firewallRuleExists(rules, name) },
		func(name string) error {
			value, err := oleutil.CallMethod(rules, "Remove", name)
			if value != nil {
				_ = value.Clear()
			}
			return err
		})
}

type firewallProperty struct {
	name  string
	value any
}

func dnsFirewallProperties(name, action, protocol, remoteIP string) ([]firewallProperty, error) {
	if name == "" || action != "block" {
		return nil, fmt.Errorf("invalid DNS leak rule name/action")
	}
	var proto int32
	switch protocol {
	case "TCP":
		proto = 6
	case "UDP":
		proto = 17
	default:
		return nil, fmt.Errorf("invalid DNS leak protocol %q", protocol)
	}
	if _, err := firewallRemoteRange(remoteIP); err != nil {
		return nil, err
	}
	return []firewallProperty{
		{"Name", name},
		// Leave ApplicationName unset (any application). Setting an empty BSTR
		// succeeds here but makes Rules.Add fail with E_INVALIDARG.
		{"ServiceName", "Dnscache"},
		{"Protocol", proto}, // Protocol MUST precede port properties.
		{"LocalPorts", "*"},
		{"RemotePorts", "53"},
		{"LocalAddresses", "*"},
		{"RemoteAddresses", remoteIP},
		{"Direction", int32(2)},         // NET_FW_RULE_DIR_OUT
		{"Action", int32(0)},            // NET_FW_ACTION_BLOCK
		{"Profiles", int32(0x7fffffff)}, // NET_FW_PROFILE2_ALL, same as profile=any
		{"InterfaceTypes", "All"},
		{"EdgeTraversal", false},
		{"Enabled", true},
	}, nil
}

func setFirewallProperties(properties []firewallProperty, put func(string, any) error) error {
	for _, p := range properties {
		if err := put(p.name, p.value); err != nil {
			return firewallCOMError("set firewall "+p.name, err)
		}
	}
	return nil
}

func newDNSFirewallRule(properties []firewallProperty) (*ole.IDispatch, error) {
	object, err := oleutil.CreateObject("HNetCfg.FWRule")
	if err != nil {
		return nil, err
	}
	defer object.Release()
	rule, err := object.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, err
	}
	err = setFirewallProperties(properties, func(name string, value any) error {
		result, err := oleutil.PutProperty(rule, name, value)
		if result != nil {
			_ = result.Clear()
		}
		return err
	})
	if err != nil {
		rule.Release()
		return nil, err
	}
	return rule, nil
}

func verifyFirewallProperties(properties []firewallProperty, get func(string) (any, error)) error {
	for _, p := range properties {
		actual, err := get(p.name)
		if err != nil {
			return fmt.Errorf("read firewall %s: %w", p.name, err)
		}
		equal := actual == p.value
		if p.name == "RemoteAddresses" {
			text, ok := actual.(string)
			if ok {
				got, err := firewallRemoteRange(text)
				want, _ := firewallRemoteRange(p.value.(string))
				equal = err == nil && got == want
			}
		}
		if !equal {
			return fmt.Errorf("firewall %s mismatch: got %v, want %v", p.name, actual, p.value)
		}
	}
	return nil
}

// Windows may canonicalize a full IPv6 range into ::/0. Compare addresses,
// not their formatting, while rejecting wider/different capture ranges.
func firewallRemoteRange(value string) ([2]netip.Addr, error) {
	var result [2]netip.Addr
	if first, last, ok := strings.Cut(value, "-"); ok {
		a, e1 := netip.ParseAddr(first)
		b, e2 := netip.ParseAddr(last)
		if e1 == nil && e2 == nil && a.BitLen() == b.BitLen() && a.Compare(b) <= 0 {
			return [2]netip.Addr{a, b}, nil
		}
	} else if p, err := netip.ParsePrefix(value); err == nil {
		p = p.Masked()
		bytes := p.Addr().AsSlice()
		for bit := p.Bits(); bit < len(bytes)*8; bit++ {
			bytes[bit/8] |= 1 << (7 - bit%8)
		}
		last, _ := netip.AddrFromSlice(bytes)
		return [2]netip.Addr{p.Addr(), last}, nil
	}
	return result, fmt.Errorf("invalid DNS leak address range %q", value)
}

func getFirewallProperty(rule *ole.IDispatch, name string) (any, error) {
	value, err := oleutil.GetProperty(rule, name)
	if value != nil {
		defer value.Clear()
	}
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("empty firewall property %s", name)
	}
	return value.Value(), nil
}

func firewallCOMError(stage string, err error) error {
	if err == nil {
		return nil
	}
	// IDispatch can report a generic DISP_E_EXCEPTION message while EXCEPINFO
	// holds the useful HRESULT (for example E_INVALIDARG or E_ACCESSDENIED).
	if code := comErrorCode(err); code != 0 {
		return fmt.Errorf("%s (HRESULT 0x%08X): %w", stage, code, err)
	}
	return fmt.Errorf("%s: %w", stage, err)
}

func addFirewallRule(rules, rule *ole.IDispatch) error {
	value, err := oleutil.CallMethod(rules, "Add", rule)
	if value != nil {
		_ = value.Clear()
	}
	return firewallCOMError("add firewall rule", err)
}

func verifyDNSFirewallRule(rule *ole.IDispatch, properties []firewallProperty) error {
	get := func(name string) (any, error) { return getFirewallProperty(rule, name) }
	// Verify the default without writing an empty ApplicationName into COM.
	if err := verifyFirewallProperties([]firewallProperty{{"ApplicationName", ""}}, get); err != nil {
		return err
	}
	return verifyFirewallProperties(properties, get)
}

func ensureDNSLeakFirewallRule(name, action, protocol, remoteIP string) error {
	properties, err := dnsFirewallProperties(name, action, protocol, remoteIP)
	if err != nil {
		return err
	}
	return withFirewallRules(func(rules *ole.IDispatch) error {
		return ensureFirewallRuleWith(
			func() error { return removeFirewallRulesFrom(rules, []string{name}) },
			func() error {
				rule, err := newDNSFirewallRule(properties)
				if err != nil {
					return err
				}
				defer rule.Release()
				return addFirewallRule(rules, rule)
			},
			func() error {
				value, err := oleutil.CallMethod(rules, "Item", name)
				if value != nil {
					defer value.Clear()
				}
				if err != nil {
					return err
				}
				if value == nil || value.ToIDispatch() == nil {
					return fmt.Errorf("added firewall rule not found: %s", name)
				}
				return verifyDNSFirewallRule(value.ToIDispatch(), properties)
			})
	})
}

func ensureFirewallRuleWith(remove, add, verify func() error) error {
	if err := remove(); err != nil {
		return err
	}
	if err := add(); err != nil {
		return errors.Join(err, remove())
	}
	if err := verify(); err != nil {
		return errors.Join(err, remove())
	}
	return nil
}

func removeFirewallRulesWith(names []string, exists func(string) (bool, error), remove func(string) error) error {
	var failures []error
	for _, name := range names {
		// Recheck after every removal: old releases may have left duplicate names.
		// Bound retries if another process keeps recreating a rule.
		for attempt := 0; ; attempt++ {
			present, err := exists(name)
			if err != nil {
				failures = append(failures, fmt.Errorf("inspect DNS leak rule %s: %w", name, err))
				break
			}
			if !present {
				break
			}
			if attempt == 128 {
				failures = append(failures, fmt.Errorf("DNS leak rule still present after cleanup: %s", name))
				break
			}
			if err := remove(name); err != nil {
				failures = append(failures, fmt.Errorf("remove DNS leak rule %s: %w", name, err))
				break
			}
		}
	}
	return errors.Join(failures...)
}
