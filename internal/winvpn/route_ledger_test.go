package winvpn

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestRouteLedgerNeverDeletesPreexistingRoutes(t *testing.T) {
	r := ownedRoute{prefix: netip.MustParsePrefix("::/0"), iface: "en0", gateway: "fe80::1%en0"}
	l := routeLedger{present: func(ownedRoute) (bool, error) { return true, nil }, command: func(string, ownedRoute) routeMutation { t.Fatal("modified existing route"); return routeMutation{} }}
	if err := l.add(r); err != nil {
		t.Fatal(err)
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestRouteLedgerRollsBackUnusableOwnedRoute(t *testing.T) {
	r := ownedRoute{prefix: netip.MustParsePrefix("172.16.88.141/32"), iface: "en0"}
	present := false
	var commands []string
	l := routeLedger{
		present: func(ownedRoute) (bool, error) { return present, nil },
		ready:   func(ownedRoute) (bool, error) { return false, nil },
		command: func(op string, _ ownedRoute) routeMutation {
			commands = append(commands, op)
			present = op == "add"
			return routeMutation{state: routeMutationSucceeded}
		},
	}
	if err := l.add(r); err == nil {
		t.Fatal("unusable route accepted")
	}
	if err := l.cleanup(); err != nil || present || len(l.owned) != 0 {
		t.Fatal("unusable route leaked", err)
	}
	if !reflect.DeepEqual(commands, []string{"add", "delete"}) {
		t.Fatal(commands)
	}
}

func TestRouteLedgerLeavesUnusablePreexistingRoute(t *testing.T) {
	l := routeLedger{
		present: func(ownedRoute) (bool, error) { return true, nil },
		ready:   func(ownedRoute) (bool, error) { return false, nil },
		command: func(string, ownedRoute) routeMutation { t.Fatal("modified foreign route"); return routeMutation{} },
	}
	if err := l.add(ownedRoute{prefix: netip.MustParsePrefix("172.16.88.141/32"), iface: "en0"}); err == nil {
		t.Fatal("invalid preexisting route reused")
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestRouteLedgerDetectsZeroExitCodeFailure(t *testing.T) {
	commands := 0
	l := routeLedger{present: func(ownedRoute) (bool, error) { return false, nil }, command: func(op string, _ ownedRoute) routeMutation {
		commands++
		if op != "add" {
			t.Fatal("deleted a route that was not installed")
		}
		return routeMutation{state: routeMutationSucceeded} // Acknowledgement alone is not readiness.
	}}
	if err := l.add(ownedRoute{prefix: netip.MustParsePrefix("::/1"), iface: "utun4"}); err == nil {
		t.Fatal("reported ready")
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
	if commands != 1 {
		t.Fatal(commands)
	}
}

func TestRouteLedgerPartialRollbackAndIdempotence(t *testing.T) {
	native := ownedRoute{prefix: netip.MustParsePrefix("::/0"), iface: "en0"}
	first := ownedRoute{prefix: netip.MustParsePrefix("::/1"), iface: "utun4"}
	second := ownedRoute{prefix: netip.MustParsePrefix("8000::/1"), iface: "utun4"}
	present := map[ownedRoute]bool{native: true}
	var deleted []ownedRoute
	l := routeLedger{present: func(r ownedRoute) (bool, error) { return present[r], nil }, command: func(op string, r ownedRoute) routeMutation {
		if op == "add" {
			if r == second {
				return routeMutation{routeMutationFailed, errors.New("conflict")}
			}
			present[r] = true
		} else {
			deleted = append(deleted, r)
			delete(present, r)
		}
		return routeMutation{state: routeMutationSucceeded}
	}}
	if err := l.add(first); err != nil {
		t.Fatal(err)
	}
	if err := l.add(second); err == nil {
		t.Fatal("missing failure")
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
	if !present[native] || !reflect.DeepEqual(deleted, []ownedRoute{first}) {
		t.Fatal(present, deleted)
	}
}

func TestRouteLedgerRetainsCandidatesAfterVerificationError(t *testing.T) {
	present, queryFailed := false, false
	l := routeLedger{present: func(ownedRoute) (bool, error) {
		if queryFailed {
			return false, errors.New("RIB temporarily unavailable")
		}
		return present, nil
	}, command: func(op string, _ ownedRoute) routeMutation {
		present = op == "add"
		queryFailed = op == "add"
		return routeMutation{state: routeMutationSucceeded}
	}}
	if err := l.add(ownedRoute{prefix: netip.MustParsePrefix("::/1"), iface: "utun4"}); err == nil {
		t.Fatal("unverified success")
	}
	if err := l.cleanup(); err == nil {
		t.Fatal("cleanup error hidden")
	}
	queryFailed = false
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
	if present || len(l.owned) != 0 {
		t.Fatal("forgot partially installed route")
	}
}

func TestDarwinDeleteArgumentsIncludeOwnership(t *testing.T) {
	if got := routeArgs("delete", ownedRoute{prefix: netip.MustParsePrefix("::/1"), iface: "utun4"}); !reflect.DeepEqual(got, []string{"-n", "delete", "-inet6", "-net", "::/1", "-interface", "utun4"}) {
		t.Fatal(got)
	}
	if got := routeArgs("delete", ownedRoute{prefix: netip.MustParsePrefix("2001:db8::1/128"), iface: "en0", gateway: "fe80::1%en0"}); !reflect.DeepEqual(got, []string{"-n", "delete", "-inet6", "-host", "2001:db8::1", "fe80::1%en0", "-ifp", "en0"}) {
		t.Fatal(got)
	}
}

func TestDarwinHostRouteArguments(t *testing.T) {
	for _, tc := range []struct{ cidr, family, kind, dst string }{
		{"8.8.8.8/32", "-inet", "-host", "8.8.8.8"},
		{"2001:4860:4860::8888/128", "-inet6", "-host", "2001:4860:4860::8888"},
		{"0.0.0.0/1", "-inet", "-net", "0.0.0.0/1"},
		{"::/1", "-inet6", "-net", "::/1"},
	} {
		for _, op := range []string{"add", "delete"} {
			r := ownedRoute{prefix: netip.MustParsePrefix(tc.cidr), iface: "utun4"}
			want := []string{"-n", op, tc.family, tc.kind, tc.dst, "-interface", "utun4"}
			if got := routeArgs(op, r); !reflect.DeepEqual(got, want) {
				t.Fatalf("%v != %v", got, want)
			}
		}
	}
}

func TestDarwinRouteCommandResults(t *testing.T) {
	r := ownedRoute{prefix: netip.MustParsePrefix("2001:4860:4860::8888/128"), iface: "utun4"}
	for _, op := range []string{"add", "delete"} {
		output := op + " host 2001:4860:4860::8888: gateway utun4"
		if err := parseDarwinRouteMutation(op, r, output+"\n", nil).failure(); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{": File exists", ": not in table", ": Network is unreachable"} {
			if err := parseDarwinRouteMutation(op, r, output+suffix, nil).failure(); err == nil {
				t.Fatal("zero-exit failure accepted", suffix)
			}
		}
		failure := errors.New("exit 1")
		if err := parseDarwinRouteMutation(op, r, output, failure).failure(); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
}

func TestDarwinRouteOutputAddressFormatting(t *testing.T) {
	// IPv4 getaddr() in Apple's route(8) splits CIDR at '/' in place.
	// Its success message therefore prints the address without the prefix.
	// These cases include the actual v1.2.36 failure reported on macOS.
	for _, tc := range []struct{ cidr, printed, gateway, iface string }{
		{"0.0.0.0/1", "net 0.0.0.0", "", "utun4"},
		{"128.0.0.0/1", "net 128.0.0.0", "", "utun4"},
		{"10.0.0.0/8", "net 10.0.0.0", "", "utun4"},
		{"111.230.148.111/32", "host 111.230.148.111", "192.168.50.1", "en0"},
		{"101.33.204.3/32", "host 101.33.204.3", "192.168.50.1", "en0"},
		{"8.8.8.8/32", "host 8.8.8.8", "", "utun4"},
		{"::/1", "net ::/1", "", "utun4"},
		{"8000::/1", "net 8000::/1", "", "utun4"},
		{"2001:4860:4860::8888/128", "host 2001:4860:4860::8888", "", "utun4"},
	} {
		for _, op := range []string{"add", "delete"} {
			t.Run(op+"/"+tc.cidr, func(t *testing.T) {
				r := ownedRoute{prefix: netip.MustParsePrefix(tc.cidr), gateway: tc.gateway, iface: tc.iface}
				nextHop := tc.gateway
				if nextHop == "" {
					nextHop = tc.iface
				}
				output := op + " " + tc.printed + ": gateway " + nextHop
				if err := parseDarwinRouteMutation(op, r, output+"\n", nil).failure(); err != nil {
					t.Fatal(err)
				}
				for _, suffix := range []string{": File exists", ": not in table", ": Network is unreachable", "\nroute: error"} {
					if err := parseDarwinRouteMutation(op, r, output+suffix, nil).failure(); err == nil {
						t.Fatal("accepted error suffix", suffix)
					}
				}
			})
		}
	}
	for _, output := range []string{
		"add net 128.0.0.0: gateway utun4", // wrong destination
		"add net 0.0.0.0/2: gateway utun4", // explicit wrong prefix
		"add net 0.0.0.0: gateway en0",     // wrong interface
		"add host 0.0.0.0: gateway utun4",  // wrong kind
	} {
		if err := parseDarwinRouteMutation("add", ownedRoute{prefix: netip.MustParsePrefix("0.0.0.0/1"), iface: "utun4"}, output, nil).failure(); err == nil {
			t.Fatal("accepted mismatched output", output)
		}
	}
}

func TestDarwinIPv4OutputStillRequiresRIBVerificationAndRollback(t *testing.T) {
	for _, installed := range []bool{true, false} {
		r := ownedRoute{prefix: netip.MustParsePrefix("0.0.0.0/1"), iface: "utun4"}
		present := false
		var commands []string
		l := routeLedger{present: func(ownedRoute) (bool, error) { return present, nil }, command: func(op string, _ ownedRoute) routeMutation {
			commands = append(commands, op)
			present = op == "add" && installed
			return parseDarwinRouteMutation(op, r, op+" net 0.0.0.0: gateway utun4\n", nil)
		}}
		err := l.add(r)
		if (err == nil) != installed {
			t.Fatalf("installed=%t: %v", installed, err)
		}
		if len(l.owned) != 1 {
			t.Fatal("successful mutation not tracked for rollback")
		}
		if err := l.cleanup(); err != nil || present || len(l.owned) != 0 {
			t.Fatal("rollback failed", err)
		}
		if installed && !reflect.DeepEqual(commands, []string{"add", "delete"}) {
			t.Fatal(commands)
		}
	}
}

func TestRouteLedgerFailedAddDoesNotClaimConcurrentRoute(t *testing.T) {
	queries := 0
	l := routeLedger{present: func(ownedRoute) (bool, error) { queries++; return queries > 1, nil }, command: func(op string, _ ownedRoute) routeMutation {
		if op != "add" {
			t.Fatal("removed another actor's route")
		}
		return routeMutation{routeMutationFailed, errors.New("File exists")}
	}}
	if err := l.add(ownedRoute{prefix: netip.MustParsePrefix("203.0.113.1/32"), iface: "en0"}); err == nil {
		t.Fatal("failed add accepted")
	}
	if len(l.owned) != 0 {
		t.Fatal("claimed route after failed add")
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestRouteLedgerCapturesDiagnosticsBeforeRollback(t *testing.T) {
	r := ownedRoute{prefix: netip.MustParsePrefix("2001:4860:4860::8888/128"), iface: "utun4"}
	mutated := false
	var logs []string
	l := routeLedger{
		trace:   func(message string) { logs = append(logs, message) },
		present: func(ownedRoute) (bool, error) { return false, nil },
		command: func(string, ownedRoute) routeMutation {
			mutated = true
			return routeMutation{state: routeMutationSucceeded}
		},
		diagnose: func(got ownedRoute) string {
			if !mutated || got != r {
				t.Fatal("diagnostics did not capture the failed mutation")
			}
			return "RIB: index=20 flags=0xC05"
		},
	}
	err := l.add(r)
	if err == nil || strings.Contains(err.Error(), "RIB:") || len(logs) != 1 || !strings.Contains(logs[0], "RIB: index=20 flags=0xC05") {
		t.Fatal(err, logs)
	}
}

func TestRouteLedgerUncertainAddNeverClaimsOrDeletes(t *testing.T) {
	for _, state := range []string{"present", "absent", "query failure"} {
		t.Run(state, func(t *testing.T) {
			r := ownedRoute{prefix: netip.MustParsePrefix("203.0.113.7/32"), iface: "en0", gateway: "192.168.50.1"}
			mutated, present, queryFails := false, state == "present", state == "query failure"
			l := routeLedger{
				present: func(ownedRoute) (bool, error) {
					if !mutated {
						return false, nil
					}
					if queryFails {
						return false, errors.New("query unavailable")
					}
					return present, nil
				},
				command: func(op string, r ownedRoute) routeMutation {
					if op != "add" {
						t.Fatal("deleted route of uncertain ownership")
					}
					mutated = true
					return parseDarwinRouteMutation(op, r, "unfamiliar route output", nil)
				},
			}
			if err := l.add(r); err == nil || !strings.Contains(err.Error(), "ownership not claimed") {
				t.Fatal("uncertain add reported ready", err)
			}
			if len(l.owned) != 0 || len(l.uncertain) != 1 {
				t.Fatal("incorrect ownership bookkeeping", l)
			}
			err := l.cleanup()
			if (err != nil) != (present || queryFails) {
				t.Fatal("cleanup hid uncertainty", err)
			}
			if (len(l.uncertain) != 0) != (present || queryFails) {
				t.Fatal("lost unresolved route")
			}
			// Once the route is verifiably gone, the unresolved record can be
			// discarded without issuing a destructive command.
			present, queryFails = false, false
			if err := l.cleanup(); err != nil || len(l.uncertain) != 0 {
				t.Fatal(err, l.uncertain)
			}
		})
	}
}

func TestRouteLedgerDeleteUsesVerifiedAbsence(t *testing.T) {
	for _, outcome := range []routeMutationState{routeMutationSucceeded, routeMutationFailed, routeMutationUnknown} {
		for _, remaining := range []bool{false, true} {
			for _, queryFails := range []bool{false, true} {
				r := ownedRoute{prefix: netip.MustParsePrefix("2001:db8::1/128"), iface: "utun4"}
				deleted := false
				l := routeLedger{
					owned: []ownedRoute{r},
					present: func(ownedRoute) (bool, error) {
						if !deleted {
							return true, nil
						}
						if queryFails {
							return false, errors.New("RIB unavailable")
						}
						return remaining, nil
					},
					command: func(op string, _ ownedRoute) routeMutation {
						if op != "delete" {
							t.Fatal(op)
						}
						deleted = true
						return routeMutation{state: outcome}
					},
				}
				err := l.cleanup()
				wantFailure := remaining || queryFails
				if (err != nil) != wantFailure || (len(l.owned) != 0) != wantFailure {
					t.Fatalf("outcome=%v remaining=%t queryFails=%t: err=%v retained=%v", outcome, remaining, queryFails, err, l.owned)
				}
			}
		}
	}
}

func TestRouteLedgerKnownRejectionDoesNotClaimConcurrentRoute(t *testing.T) {
	r := ownedRoute{prefix: netip.MustParsePrefix("203.0.113.7/32"), iface: "en0", gateway: "192.168.50.1"}
	queries := 0
	l := routeLedger{
		present: func(ownedRoute) (bool, error) { queries++; return queries > 1, nil },
		command: func(op string, r ownedRoute) routeMutation {
			if op != "add" {
				t.Fatal("deleted concurrent route")
			}
			return parseDarwinRouteMutation(op, r, "add host 203.0.113.7: gateway 192.168.50.1: File exists", nil)
		},
	}
	if err := l.add(r); err == nil || len(l.owned) != 0 || len(l.uncertain) != 0 {
		t.Fatal(err, l)
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
}
