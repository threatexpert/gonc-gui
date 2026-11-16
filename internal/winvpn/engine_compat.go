//go:build windows || darwin || linux

package winvpn

import (
	"fmt"
	"gonc-gui/internal/vpnconfig"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

// v2.7.0 uses fatal/void lifecycle methods; the local upstream-based fork returns
// errors. Support both during local testing without committing a machine-local
// replace directive or ignoring the newer engine's startup failures.
func callEngineLifecycle(fn any) error {
	switch fn := fn.(type) {
	case func() error:
		return fn()
	case func():
		fn()
		return nil
	default:
		return fmt.Errorf("unsupported tun2socks lifecycle signature %T", fn)
	}
}

func startEngine() error { return callEngineLifecycle(engine.Start) }
func stopEngine() error  { return callEngineLifecycle(engine.Stop) }

func insertEngine(config vpnconfig.Config, device string) error {
	key := &engine.Key{Device: device, Proxy: config.SOCKS5Endpoint, LogLevel: config.LogLevel, MTU: config.MTU}
	if err := configureEnginePolicy(key, config); err != nil {
		return err
	}
	engine.Insert(key)
	return nil
}
