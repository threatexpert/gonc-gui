package winvpn

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRouteLedgerNeverDeletesPreexistingRoutes(t *testing.T) {
	r := ownedRoute{cidr: "::/0", iface: "en0", gateway: "fe80::1%en0"}
	l := routeLedger{present: func(ownedRoute) (bool, error) { return true, nil }, command: func(string, ownedRoute) error { t.Fatal("modified existing route"); return nil }}
	if err := l.add(r); err != nil {
		t.Fatal(err)
	}
	if err := l.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestRouteLedgerDetectsZeroExitCodeFailure(t *testing.T) {
	commands := 0
	l := routeLedger{present: func(ownedRoute) (bool, error) { return false, nil }, command: func(op string, _ ownedRoute) error {
		commands++
		if op != "add" {
			t.Fatal("deleted a route that was not installed")
		}
		return nil // macOS route(8) can report EEXIST with exit code zero.
	}}
	if err := l.add(ownedRoute{cidr: "::/1", iface: "utun4"}); err == nil {
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
	native := ownedRoute{cidr: "::/0", iface: "en0"}
	first := ownedRoute{cidr: "::/1", iface: "utun4"}
	second := ownedRoute{cidr: "8000::/1", iface: "utun4"}
	present := map[ownedRoute]bool{native: true}
	var deleted []ownedRoute
	l := routeLedger{present: func(r ownedRoute) (bool, error) { return present[r], nil }, command: func(op string, r ownedRoute) error {
		if op == "add" {
			if r == second {
				return errors.New("conflict")
			}
			present[r] = true
		} else {
			deleted = append(deleted, r)
			delete(present, r)
		}
		return nil
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
	}, command: func(op string, _ ownedRoute) error {
		present = op == "add"
		queryFailed = op == "add"
		return nil
	}}
	if err := l.add(ownedRoute{cidr: "::/1", iface: "utun4"}); err == nil {
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
	if got := routeArgs("delete", ownedRoute{cidr: "::/1", iface: "utun4"}); !reflect.DeepEqual(got, []string{"-n", "delete", "-inet6", "-net", "::/1", "-interface", "utun4"}) {
		t.Fatal(got)
	}
	if got := routeArgs("delete", ownedRoute{cidr: "2001:db8::1/128", iface: "en0", gateway: "fe80::1%en0"}); !reflect.DeepEqual(got, []string{"-n", "delete", "-inet6", "-host", "2001:db8::1", "fe80::1%en0", "-ifp", "en0"}) {
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
			r := ownedRoute{cidr: tc.cidr, iface: "utun4"}
			want := []string{"-n", op, tc.family, tc.kind, tc.dst, "-interface", "utun4"}
			if got := routeArgs(op, r); !reflect.DeepEqual(got, want) {
				t.Fatalf("%v != %v", got, want)
			}
		}
	}
}

func TestDarwinRouteCommandResults(t *testing.T) {
	r := ownedRoute{cidr: "2001:4860:4860::8888/128", iface: "utun4"}
	for _, op := range []string{"add", "delete"} {
		output := op + " host 2001:4860:4860::8888: gateway utun4"
		if err := checkDarwinRouteOutput(op, r, output+"\n", nil); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{": File exists", ": not in table", ": Network is unreachable"} {
			if err := checkDarwinRouteOutput(op, r, output+suffix, nil); err == nil {
				t.Fatal("zero-exit failure accepted", suffix)
			}
		}
		failure := errors.New("exit 1")
		if err := checkDarwinRouteOutput(op, r, output, failure); !errors.Is(err, failure) {
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
				r := ownedRoute{cidr: tc.cidr, gateway: tc.gateway, iface: tc.iface}
				nextHop := tc.gateway
				if nextHop == "" {
					nextHop = tc.iface
				}
				output := op + " " + tc.printed + ": gateway " + nextHop
				if err := checkDarwinRouteOutput(op, r, output+"\n", nil); err != nil {
					t.Fatal(err)
				}
				for _, suffix := range []string{": File exists", ": not in table", ": Network is unreachable", "\nroute: error"} {
					if err := checkDarwinRouteOutput(op, r, output+suffix, nil); err == nil {
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
		if err := checkDarwinRouteOutput("add", ownedRoute{cidr: "0.0.0.0/1", iface: "utun4"}, output, nil); err == nil {
			t.Fatal("accepted mismatched output", output)
		}
	}
}

func TestDarwinIPv4OutputStillRequiresRIBVerificationAndRollback(t *testing.T) {
	for _, installed := range []bool{true, false} {
		r := ownedRoute{cidr: "0.0.0.0/1", iface: "utun4"}
		present := false
		var commands []string
		l := routeLedger{present: func(ownedRoute) (bool, error) { return present, nil }, command: func(op string, _ ownedRoute) error {
			commands = append(commands, op)
			present = op == "add" && installed
			return checkDarwinRouteOutput(op, r, op+" net 0.0.0.0: gateway utun4\n", nil)
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
	l := routeLedger{present: func(ownedRoute) (bool, error) { queries++; return queries > 1, nil }, command: func(op string, _ ownedRoute) error {
		if op != "add" {
			t.Fatal("removed another actor's route")
		}
		return errors.New("File exists")
	}}
	if err := l.add(ownedRoute{cidr: "203.0.113.1/32", iface: "en0"}); err == nil {
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
	r := ownedRoute{cidr: "2001:4860:4860::8888/128", iface: "utun4"}
	mutated := false
	l := routeLedger{
		present: func(ownedRoute) (bool, error) { return false, nil },
		command: func(string, ownedRoute) error { mutated = true; return nil },
		diagnose: func(got ownedRoute) string {
			if !mutated || got != r {
				t.Fatal("diagnostics did not capture the failed mutation")
			}
			return "RIB: index=20 flags=0xC05"
		},
	}
	err := l.add(r)
	if err == nil || !strings.Contains(err.Error(), "RIB: index=20 flags=0xC05") {
		t.Fatal(err)
	}
}
