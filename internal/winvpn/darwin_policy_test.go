package winvpn

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestStableDarwinRoute(t *testing.T) {
	host := darwinRouteUp | darwinRouteStatic | darwinRouteHost | darwinRouteGateway
	if !stableDarwinRoute(host, true) {
		t.Fatal("static unscoped host rejected")
	}
	for _, flag := range []int{darwinRouteWasCloned, darwinRouteIfScope, darwinRouteDynamic, darwinRouteReject, darwinRouteBlackhole, darwinRouteLLInfo, darwinRouteCloning, darwinRouteProtoCloning} {
		if stableDarwinRoute(host|flag, true) {
			t.Fatalf("unsafe host flags accepted: %x", flag)
		}
	}
	if stableDarwinRoute(host&^darwinRouteStatic, true) || stableDarwinRoute(host&^darwinRouteUp, true) || stableDarwinRoute(host, false) {
		t.Fatal("unstable/wrong route kind accepted")
	}
	if !stableDarwinRoute(darwinRouteUp|darwinRouteStatic|darwinRouteProtoCloning, false) {
		t.Fatal("static capture network rejected")
	}
}

func TestParseDarwinRouteLookup(t *testing.T) {
	for _, tc := range []struct {
		name, gateway, flags, want string
		bad                        bool
	}{
		{"ipv4", "192.168.50.1", "UP,GATEWAY,HOST,DONE,WASCLONED,IFSCOPE", "192.168.50.1", false},
		{"ipv6", "fe80::1%en0", "UP,GATEWAY,DONE", "fe80::1%en0", false},
		{"ipv6 implicit zone", "fe80::1", "UP,GATEWAY,DONE", "fe80::1%en0", false},
		{"on link", "link#11", "UP,HOST,DONE", "", false},
		{"neighbour", "7c:10:c9:4d:f9:a8", "UP,HOST,LLINFO,WASCLONED", "", false},
		{"reject", "192.168.50.1", "UP,GATEWAY,REJECT", "", true},
		{"blackhole", "192.168.50.1", "UP,GATEWAY,BLACKHOLE", "", true},
		{"down", "192.168.50.1", "GATEWAY", "", true},
		{"bad gateway", "link#11", "UP,GATEWAY", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDarwinRouteLookup(fmt.Sprintf("route to: 203.0.113.7\n gateway: %s\n interface: en0\n flags: <%s>\n", tc.gateway, tc.flags))
			if tc.bad {
				if err == nil {
					t.Fatal("accepted unusable route")
				}
				return
			}
			if err != nil || got.iface != "en0" || got.gateway != tc.want {
				t.Fatal(got, err)
			}
		})
	}
	if _, err := parseDarwinRouteLookup("route: writing to routing socket: not in table"); err == nil {
		t.Fatal("accepted missing route")
	}
}

func TestDarwinBypassPlanningAndReconnect(t *testing.T) {
	lookup := func(ip string) (darwinRouteLookup, error) { return darwinRouteLookup{"en0", "192.168.50.1"}, nil }
	routes, err := planDarwinBypass([]string{"203.0.113.7", "203.0.113.7"}, "utun4", lookup)
	if err != nil || len(routes) != 1 || routes[0].cidr != "203.0.113.7/32" {
		t.Fatal(routes, err)
	}
	if err := verifyDarwinBypass(routes, "utun4", lookup, nil); err != nil {
		t.Fatal(err)
	}
	// A new session resolves the changed peer/network afresh, not an old exit.
	lookup6 := func(string) (darwinRouteLookup, error) { return darwinRouteLookup{"en1", "fe80::1%en1"}, nil }
	next, err := planDarwinBypass([]string{"2001:db8::7"}, "utun5", lookup6)
	if err != nil || next[0].iface != "en1" || next[0].cidr != "2001:db8::7/128" {
		t.Fatal(next, err)
	}
	if err := verifyDarwinBypass(next, "utun5", lookup6, nil); err != nil {
		t.Fatal(err)
	}
	for _, ips := range [][]string{nil, {"bad"}, {"0.0.0.0"}, {"127.0.0.1"}, {"ff02::1"}} {
		if _, err := planDarwinBypass(ips, "utun4", lookup); err == nil {
			t.Fatal("unsafe peers accepted", ips)
		}
	}
	recursive := func(string) (darwinRouteLookup, error) { return darwinRouteLookup{"utun4", ""}, nil }
	if _, err := planDarwinBypass([]string{"203.0.113.7"}, "utun4", recursive); err == nil {
		t.Fatal("recursive native exit accepted")
	}
	failure := errors.New("route query failed")
	failed := func(string) (darwinRouteLookup, error) { return darwinRouteLookup{}, failure }
	if _, err := planDarwinBypass([]string{"203.0.113.7"}, "utun4", failed); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := verifyDarwinBypass(routes, "utun4", failed, nil); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, actual := range []darwinRouteLookup{{"utun4", ""}, {"en1", "192.168.50.1"}, {"en0", "192.168.50.2"}} {
		if err := verifyDarwinBypass(routes, "utun4", func(string) (darwinRouteLookup, error) { return actual, nil }, nil); err == nil {
			t.Fatal("changed exit accepted", actual)
		}
	}
}

func TestDarwinGatewayAndDNSComparison(t *testing.T) {
	if !sameDarwinGateway("fe80:0:0:0:0:0:0:1", "fe80::1%en0", "en0") || sameDarwinGateway("fe80::1%en1", "fe80::1%en0", "en0") || sameDarwinGateway("bad", "bad", "en0") {
		t.Fatal("gateway comparison")
	}
	for _, tc := range []struct {
		a, b []string
		want bool
	}{
		{nil, []string{}, true},
		{[]string{"2001:4860:4860::8888"}, []string{"2001:4860:4860:0:0:0:0:8888"}, true},
		{[]string{"2001:DB8::ABCD", "8.8.8.8"}, []string{"2001:db8::abcd", "8.8.8.8"}, true},
		{[]string{"8.8.8.8", "1.1.1.1"}, []string{"1.1.1.1", "8.8.8.8"}, false},
		{[]string{"2001:db8::1"}, []string{"2001:db8::2"}, false},
		{[]string{"8.8.8.8"}, nil, false},
		{[]string{"bad"}, []string{"bad"}, false},
	} {
		if got := sameDarwinDNS(tc.a, tc.b); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestDarwinConfigurationStagesAndRollback(t *testing.T) {
	for _, failAt := range []string{"", "bypass", "capture", "verify1", "dns", "verify2"} {
		t.Run("fail="+failAt, func(t *testing.T) {
			var calls []string
			failure := errors.New("failure")
			owned := map[ownedRoute]bool{}
			r := ownedRoute{cidr: "203.0.113.7/32", iface: "en0", gateway: "192.168.50.1"}
			ledger := routeLedger{present: func(r ownedRoute) (bool, error) { return owned[r], nil }, command: func(op string, r ownedRoute) error {
				if op == "add" {
					owned[r] = true
				} else {
					delete(owned, r)
				}
				return nil
			}}
			step := func(name string) error {
				calls = append(calls, name)
				if name == failAt {
					return failure
				}
				return nil
			}
			checks := 0
			err := applyDarwinNetwork(func() error {
				if err := step("bypass"); err != nil {
					return err
				}
				return ledger.add(r)
			}, func() error { return step("capture") }, func() error { return step("dns") }, func() error { checks++; return step(fmt.Sprintf("verify%d", checks)) })
			if failAt == "" {
				if err != nil || !reflect.DeepEqual(calls, []string{"bypass", "capture", "verify1", "dns", "verify2"}) {
					t.Fatal(calls, err)
				}
			} else {
				if !errors.Is(err, failure) || calls[len(calls)-1] != failAt {
					t.Fatal(calls, err)
				}
				if strings.Contains(failAt, "verify") && len(owned) == 0 {
					t.Fatal("test did not install bypass before verification")
				}
			}
			// Start's error path and normal Stop use the same ownership cleanup.
			if err := ledger.cleanup(); err != nil || len(owned) != 0 {
				t.Fatal(owned, err)
			}
		})
	}
}

func TestDarwinCloneDoesNotSuppressStaticBypass(t *testing.T) {
	r := ownedRoute{cidr: "203.0.113.7/32", iface: "en0", gateway: "192.168.50.1"}
	flags := darwinRouteUp | darwinRouteHost | darwinRouteWasCloned | darwinRouteIfScope
	adds := 0
	l := routeLedger{present: func(ownedRoute) (bool, error) { return stableDarwinRoute(flags, true), nil }, command: func(op string, _ ownedRoute) error {
		if op == "add" {
			adds++
			flags = darwinRouteUp | darwinRouteHost | darwinRouteStatic
		} else {
			flags = 0
		}
		return nil
	}}
	if err := l.add(r); err != nil || adds != 1 || len(l.owned) != 1 {
		t.Fatal(adds, l.owned, err)
	}
	if err := l.add(r); err != nil || adds != 1 {
		t.Fatal("static route should be reused", err)
	}
}
