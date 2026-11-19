package winvpn

import (
	"errors"
	"reflect"
	"testing"
)

func TestBypassRefreshRemovesOldRouteBeforePreparingNewAddress(t *testing.T) {
	old := bypassSelection{Name: "en0", Index: 11, IP: "192.168.50.220", Gateway: "192.168.50.1"}
	next := old
	var calls []string
	offline := false
	b := &BypassSession{
		selectExit: func(string, string, bypassSelection) (bypassSelection, error) {
			if offline {
				return bypassSelection{}, errors.New("Wi-Fi down")
			}
			return next, nil
		},
		prepare: func(s bypassSelection) (*bypassRoute, error) {
			calls = append(calls, "add "+s.IP)
			return &bypassRoute{ready: func() (bool, error) { return true, nil }, close: func() error { calls = append(calls, "delete "+s.IP); return nil }}, nil
		},
	}
	if _, _, err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	offline = true
	if _, _, err := b.Refresh(); err == nil {
		t.Fatal("offline accepted")
	}
	next.IP = "192.168.50.40"
	offline = false
	ip, rev, err := b.Refresh()
	if err != nil || ip != next.IP || rev != 2 {
		t.Fatalf("%s %d %v", ip, rev, err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"add " + old.IP, "delete " + old.IP, "add " + next.IP, "delete " + next.IP}) {
		t.Fatal(calls)
	}
}

func TestBypassCleanupFailureRetainsOwnershipAndBlocksReplacement(t *testing.T) {
	selected := bypassSelection{Name: "en0", Index: 11, IP: "192.168.50.2", Gateway: "192.168.50.1"}
	failed := true
	deleted := 0
	prepared := 0
	b := &BypassSession{selection: selected, route: &bypassRoute{ready: func() (bool, error) { return false, nil }, close: func() error {
		deleted++
		if failed {
			return errors.New("delete denied")
		}
		return nil
	}},
		selectExit: func(string, string, bypassSelection) (bypassSelection, error) { return selected, nil },
		prepare: func(bypassSelection) (*bypassRoute, error) {
			prepared++
			return &bypassRoute{ready: func() (bool, error) { return true, nil }}, nil
		}}
	if _, _, err := b.Refresh(); err == nil || prepared != 0 || b.route == nil {
		t.Fatal("failed cleanup was forgotten", err)
	}
	if err := b.Close(); err == nil {
		t.Fatal("close hid cleanup error")
	}
	failed = false
	if err := b.Close(); err != nil || b.route != nil || deleted != 3 {
		t.Fatal(err, deleted)
	}
}

func TestBypassGatewayChangeWithSameIPRebuildsOwnedRoute(t *testing.T) {
	a := bypassSelection{Name: "en0", Index: 11, IP: "192.168.50.40", Gateway: "192.168.50.1"}
	n := a
	n.Gateway = "192.168.50.254"
	cleaned := false
	b := &BypassSession{selection: a, LocalIP: a.IP, revision: 1, route: &bypassRoute{close: func() error { cleaned = true; return nil }},
		selectExit: func(string, string, bypassSelection) (bypassSelection, error) { return n, nil },
		prepare: func(s bypassSelection) (*bypassRoute, error) {
			if !cleaned || s != n {
				t.Fatal("prepared before cleanup")
			}
			return &bypassRoute{ready: func() (bool, error) { return true, nil }}, nil
		}}
	ip, rev, err := b.Refresh()
	if err != nil || ip != a.IP || rev != 2 {
		t.Fatal(ip, rev, err)
	}
}

func TestReusedBypassIsNotDeletedOnClose(t *testing.T) {
	b := &BypassSession{route: &bypassRoute{ready: func() (bool, error) { return true, nil }}}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}
