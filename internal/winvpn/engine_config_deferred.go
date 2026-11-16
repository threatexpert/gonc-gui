//go:build (windows || darwin || linux) && gonc_deferred_tcp

package winvpn

import (
	"time"

	"github.com/xjasonlyu/tun2socks/v2/engine"
	"gonc-gui/internal/vpnconfig"
)

// Internal policy for local-fork builds. Start uses the same public entry point
// in both modes; the library defaults remain unchanged. No UI switch is added.
func configureEnginePolicy(key *engine.Key, config vpnconfig.Config) error {
	key.TCPDeferredHandshake = true
	key.TCPSOCKSHandshakeTimeout = 30 * time.Second
	key.TCPFailureCacheTTL = 30 * time.Second
	key.TCPFailureCacheCapacity = 4096
	key.RejectIPv6 = config.RejectIPv6
	return nil
}
