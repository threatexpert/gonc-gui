//go:build windows

package winvpn

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

func TestFirewallCleanupExactNames(t *testing.T) {
	rules := map[string]int{dnsLeakRuleUDPLow: 3, dnsLeakRuleTCPIPv6: 1, "user custom rule": 1}
	var removed []string
	err := removeFirewallRulesWith(dnsLeakRuleNames, func(name string) (bool, error) { return rules[name] > 0, nil }, func(name string) error {
		removed = append(removed, name)
		rules[name]--
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 4 || rules["user custom rule"] != 1 {
		t.Fatal(rules, removed)
	}
	err = removeFirewallRulesWith(dnsLeakRuleNames, func(name string) (bool, error) { return rules[name] > 0, nil }, func(string) error { t.Fatal("absent rules should not be removed"); return nil })
	if err != nil {
		t.Fatal(err)
	}
}

func TestDNSFirewallPropertyMapping(t *testing.T) {
	for _, protocol := range []string{"TCP", "UDP"} {
		properties, err := dnsFirewallProperties("test rule", "block", protocol, "0.0.0.0-126.255.255.255")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]any{}
		protocolSeen := false
		err = setFirewallProperties(properties, func(name string, value any) error {
			if name == "Protocol" {
				protocolSeen = true
			}
			if (name == "LocalPorts" || name == "RemotePorts") && !protocolSeen {
				t.Fatal("ports set before protocol")
			}
			got[name] = value
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		proto := int32(6)
		if protocol == "UDP" {
			proto = 17
		}
		want := map[string]any{
			"Name": "test rule", "ServiceName": "Dnscache", "Protocol": proto,
			"LocalPorts": "*", "RemotePorts": "53", "LocalAddresses": "*", "RemoteAddresses": "0.0.0.0-126.255.255.255",
			"Direction": int32(2), "Action": int32(0), "Profiles": int32(0x7fffffff), "InterfaceTypes": "All", "EdgeTraversal": false, "Enabled": true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mapping differs from netsh semantics: %v", got)
		}
		failure := errors.New("property setter failed")
		calls := 0
		err = setFirewallProperties(properties, func(string, any) error { calls++; return failure })
		if !errors.Is(err, failure) || calls != 1 {
			t.Fatal(err, calls)
		}
	}
	for _, tc := range [][4]string{{"", "block", "TCP", "::/0"}, {"name", "allow", "TCP", "::/0"}, {"name", "block", "ANY", "::/0"}, {"name", "block", "UDP", "*"}} {
		if _, err := dnsFirewallProperties(tc[0], tc[1], tc[2], tc[3]); err == nil {
			t.Fatal("invalid rule accepted", tc)
		}
	}
}

func TestDNSFirewallCreationFailures(t *testing.T) {
	for _, phase := range []string{"success", "remove", "add", "verify"} {
		t.Run(phase, func(t *testing.T) {
			failure := errors.New(phase)
			var calls []string
			err := ensureFirewallRuleWith(func() error {
				calls = append(calls, "remove")
				if phase == "remove" {
					return failure
				}
				return nil
			},
				func() error {
					calls = append(calls, "add")
					if phase == "add" {
						return failure
					}
					return nil
				},
				func() error {
					calls = append(calls, "verify")
					if phase == "verify" {
						return failure
					}
					return nil
				})
			var want []string
			switch phase {
			case "success":
				want = []string{"remove", "add", "verify"}
				if err != nil {
					t.Fatal(err)
				}
			case "remove":
				want = []string{"remove"}
			case "add":
				want = []string{"remove", "add", "remove"}
			case "verify":
				want = []string{"remove", "add", "verify", "remove"}
			}
			if phase != "success" && !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatal(calls)
			}
		})
	}
	createErr, cleanupErr := errors.New("add"), errors.New("rollback")
	removes := 0
	err := ensureFirewallRuleWith(func() error {
		removes++
		if removes == 2 {
			return cleanupErr
		}
		return nil
	}, func() error { return createErr }, func() error { t.Fatal("verify after failed add"); return nil })
	if !errors.Is(err, createErr) || !errors.Is(err, cleanupErr) {
		t.Fatal("lost rollback error", err)
	}
}

func TestDNSFirewallVerification(t *testing.T) {
	properties, err := dnsFirewallProperties("test", "block", "UDP", "::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	for _, p := range properties {
		values[p.name] = p.value
	}
	values["RemoteAddresses"] = "::/0"
	get := func(name string) (any, error) { return values[name], nil }
	if err := verifyFirewallProperties(properties, get); err != nil {
		t.Fatal(err)
	}
	for _, p := range properties {
		old := values[p.name]
		values[p.name] = "incorrect"
		if err := verifyFirewallProperties(properties, get); err == nil {
			t.Fatalf("ignored mismatched %s", p.name)
		}
		values[p.name] = old
	}
	values["RemoteAddresses"] = "::/1"
	if err := verifyFirewallProperties(properties, get); err == nil {
		t.Fatal("accepted incomplete IPv6 capture")
	}
	failure := errors.New("cannot read")
	if err := verifyFirewallProperties(properties, func(string) (any, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestDNSFirewallDetachedCOMRule(t *testing.T) {
	// This creates only unattached in-memory rules. It never calls Rules.Add,
	// so it validates real COM property setters/getters without changing policy.
	err := withFirewallRules(func(_ *ole.IDispatch) error {
		for _, remoteIP := range []string{"0.0.0.0-126.255.255.255", "128.0.0.0-255.255.255.255", "::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
			for _, protocol := range []string{"TCP", "UDP"} {
				properties, err := dnsFirewallProperties("Gonc detached test", "block", protocol, remoteIP)
				if err != nil {
					return err
				}
				started := time.Now()
				rule, err := newDNSFirewallRule(properties)
				if err != nil {
					return err
				}
				t.Logf("create/configure detached %s %s: %s", protocol, remoteIP, time.Since(started))
				err = verifyDNSFirewallRule(rule, properties)
				rule.Release()
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFirewallCleanupErrors(t *testing.T) {
	failure := errors.New("access denied")
	for _, inspectFail := range []bool{false, true} {
		var checked []string
		err := removeFirewallRulesWith([]string{"one", "two"}, func(name string) (bool, error) {
			checked = append(checked, name)
			if inspectFail {
				return false, failure
			}
			return true, nil
		}, func(string) error {
			if inspectFail {
				t.Fatal("remove after inspection failed")
			}
			return failure
		})
		if !errors.Is(err, failure) || !reflect.DeepEqual(checked, []string{"one", "two"}) {
			t.Fatal(err, checked)
		}
	}
	removed := 0
	err := removeFirewallRulesWith([]string{"stuck"}, func(string) (bool, error) { return true, nil }, func(string) error { removed++; return nil })
	if err == nil || removed != 128 {
		t.Fatal("unbounded cleanup or false success", removed, err)
	}
}

func TestCOMErrorCodes(t *testing.T) {
	for _, code := range []uint32{1, 0x80070002, 0x80070005, 0x80010106} {
		if got := comErrorCode(fmt.Errorf("wrapped: %w", ole.NewError(uintptr(code)))); got != code {
			t.Fatalf("%x != %x", got, code)
		}
	}
	if comErrorCode(nil) != 0 || comErrorCode(errors.New("other")) != 0 {
		t.Fatal("not a COM error")
	}
}

func TestFirewallCOMErrorContext(t *testing.T) {
	cause := ole.NewError(0x80070057)
	err := firewallCOMError("add firewall rule", cause)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "add firewall rule (HRESULT 0x80070057)") {
		t.Fatal(err)
	}
	if firewallCOMError("add", nil) != nil {
		t.Fatal("nil error must stay nil")
	}
}

func TestDNSFirewallAddValidationWithoutElevation(t *testing.T) {
	// Add validates rule parameters before checking permissions. A non-admin
	// attempt must reach E_ACCESSDENIED, not E_INVALIDARG. Rules are disabled
	// as an extra precaution; do not run this write-boundary test as admin.
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("requires non-elevated process")
	}
	err := withFirewallRules(func(rules *ole.IDispatch) error {
		for _, remote := range []string{"0.0.0.0-126.255.255.255", "128.0.0.0-255.255.255.255", "::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
			for _, protocol := range []string{"TCP", "UDP"} {
				name := fmt.Sprintf("Gonc disabled validation test %d", time.Now().UnixNano())
				properties, err := dnsFirewallProperties(name, "block", protocol, remote)
				if err != nil {
					return err
				}
				for i := range properties {
					if properties[i].name == "Enabled" {
						properties[i].value = false
					}
				}
				rule, err := newDNSFirewallRule(properties)
				if err != nil {
					return err
				}
				defer rule.Release()
				err = addFirewallRule(rules, rule)
				if err == nil {
					return errors.Join(fmt.Errorf("unexpected permission to add test rule"), removeFirewallRulesFrom(rules, []string{name}))
				}
				if comErrorCode(err) != 0x80070005 {
					return fmt.Errorf("%s %s: %w", protocol, remote, err)
				}
				// Negative control reproduces the original bug, including the
				// EXCEPINFO HRESULT hidden by the generic IDispatch error text.
				value, err := oleutil.PutProperty(rule, "ApplicationName", "")
				if value != nil {
					_ = value.Clear()
				}
				if err != nil {
					return err
				}
				err = addFirewallRule(rules, rule)
				if err == nil {
					return errors.Join(fmt.Errorf("unexpected permission to add invalid test rule"), removeFirewallRulesFrom(rules, []string{name}))
				}
				if comErrorCode(err) != 0x80070057 {
					return fmt.Errorf("expected empty-application E_INVALIDARG: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFirewallCOMReadOnlyTiming(t *testing.T) {
	// The first pass in a fresh test process is cold; subsequent passes rebuild
	// the objects. No Remove calls are made, even if Gonc is currently running.
	for pass := 0; pass < 5; pass++ {
		started := time.Now()
		err := withFirewallRulesTrace(func(rules *ole.IDispatch) error {
			for _, name := range dnsLeakRuleNames {
				if _, err := firewallRuleExists(rules, name); err != nil {
					return err
				}
			}
			// Measure a repeated query batch reusing the same COM objects.
			warm := time.Now()
			for _, name := range dnsLeakRuleNames {
				if _, err := firewallRuleExists(rules, name); err != nil {
					return err
				}
			}
			t.Logf("pass %d reused-object query (8 names): %s", pass, time.Since(warm))
			return nil
		}, func(stage string, elapsed time.Duration) { t.Logf("pass %d %s: %s", pass, stage, elapsed) })
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("pass %d total including release: %s", pass, time.Since(started))
	}
}
