//go:build windows || darwin || linux

package winvpn

import (
	"errors"
	"testing"
)

func TestEngineLifecycleCompatibility(t *testing.T) {
	called := false
	if err := callEngineLifecycle(func() { called = true }); err != nil || !called {
		t.Fatal(called, err)
	}
	want := errors.New("engine startup failed")
	if err := callEngineLifecycle(func() error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := callEngineLifecycle(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
