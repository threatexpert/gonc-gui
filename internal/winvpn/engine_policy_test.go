//go:build windows || darwin || linux

package winvpn

import (
	"github.com/xjasonlyu/tun2socks/v2/engine"
	"gonc-gui/internal/vpnconfig"
	"testing"
	"time"
)

func TestEngineIPv6RejectionClearsOnRecovery(t *testing.T) {
	key := &engine.Key{}
	for _, reject := range []bool{true, false, true} {
		if err := configureEnginePolicy(key, vpnconfig.Config{RejectIPv6: reject}); err != nil {
			t.Fatal(err)
		}
		if key.RejectIPv6 != reject || !key.TCPDeferredHandshake {
			t.Fatalf("wrong engine policy: %+v", key)
		}
		if key.TCPSOCKSHandshakeTimeout != 30*time.Second {
			t.Fatalf("wrong timeout policy: %+v", key)
		}
		if key.TCPFailureCacheTTL != 30*time.Second || key.TCPFailureCacheCapacity != 4096 {
			t.Fatalf("wrong failure cache policy: %+v", key)
		}
	}
}
