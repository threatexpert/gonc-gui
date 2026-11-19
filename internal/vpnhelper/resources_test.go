package vpnhelper

import (
	"errors"
	"testing"
)

type failedNetwork struct {
	failures int
	stops    int
}

func (n *failedNetwork) Stop() error {
	n.stops++
	if n.stops <= n.failures {
		return errors.New("route delete failed")
	}
	return nil
}
func (*failedNetwork) DNSServers() []string     { return nil }
func (*failedNetwork) UpdateDNS([]string) error { return nil }
func (*failedNetwork) UpdateProxy(string) error { return nil }

type failedBypass struct {
	failures int
	closes   int
}

func (b *failedBypass) Close() error {
	b.closes++
	if b.closes <= b.failures {
		return errors.New("scoped route delete failed")
	}
	return nil
}
func (*failedBypass) Refresh() (string, uint64, error) { return "", 0, nil }

func TestCleanupAttemptsBothResourcesAndRetainsFailures(t *testing.T) {
	n, b := &failedNetwork{failures: 1}, &failedBypass{failures: 2}
	r := helperResources{active: n, bypass: b}
	if err := r.cleanup(); err == nil || n.stops != 1 || b.closes != 1 || r.active == nil || r.bypass == nil {
		t.Fatal("cleanup hid failure", err)
	}
	if err := r.cleanup(); err == nil || r.active != nil || r.bypass == nil {
		t.Fatal("retry state lost", err)
	}
	if err := r.cleanup(); err != nil || r.bypass != nil || n.stops != 2 || b.closes != 3 {
		t.Fatal("retry failed", err)
	}
}
