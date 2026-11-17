//go:build windows || darwin || linux

package winvpn

import (
	"gonc-gui/internal/vpnconfig"
	"testing"
)

func TestEngineIPv6RejectionClearsOnRecovery(t *testing.T) {
	for _, reject := range []bool{true, false, true} {
		key := engineConfig(vpnconfig.Config{SOCKS5Endpoint: "socks5://127.0.0.1:1080", MTU: 1400, LogLevel: "warn", RejectIPv6: reject}, "tun://gonc")
		if key.RejectIPv6 != reject || key.DeferredTCP == nil {
			t.Fatalf("wrong engine policy: %+v", key)
		}
		if key.Device != "tun://gonc" || key.Proxy != "socks5://127.0.0.1:1080" || key.MTU != 1400 || key.LogLevel != "warn" {
			t.Fatalf("lost engine settings: %+v", key)
		}
		if key.DeferredTCP.ProxyHandshakeTimeout != 0 || key.DeferredTCP.FailureCacheTTL != 0 || key.DeferredTCP.FailureCacheCapacity != 0 {
			t.Fatalf("desktop should use shared defaults: %+v", key)
		}
	}
}
