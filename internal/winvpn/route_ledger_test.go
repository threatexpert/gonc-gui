package winvpn

import (
	"errors"
	"reflect"
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
	if got := routeArgs("delete", ownedRoute{cidr: "2001:db8::1/128", iface: "en0", gateway: "fe80::1%en0"}); got[len(got)-1] != "fe80::1%en0" {
		t.Fatal(got)
	}
}
