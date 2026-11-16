//go:build windows || darwin || linux

package winvpn

import (
	"time"

	"github.com/xjasonlyu/tun2socks/v2/engine"
	"gonc-gui/internal/vpnconfig"
)

// Desktop builds always use the pinned fork. Enable its optional policy here;
// library defaults remain unchanged and no special build tag is required.
func configureEnginePolicy(key *engine.Key, config vpnconfig.Config) error {
	key.TCPDeferredHandshake = true
	key.TCPSOCKSHandshakeTimeout = 30 * time.Second
	key.TCPFailureCacheTTL = 30 * time.Second
	key.TCPFailureCacheCapacity = 4096
	key.RejectIPv6 = config.RejectIPv6
	return nil
}
