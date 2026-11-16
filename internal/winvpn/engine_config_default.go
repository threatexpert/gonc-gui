//go:build (windows || darwin || linux) && !gonc_deferred_tcp

package winvpn

import (
	"errors"

	"github.com/xjasonlyu/tun2socks/v2/engine"
	"gonc-gui/internal/vpnconfig"
)

// Published-dependency builds do not have the fork's optional configuration.
func configureEnginePolicy(_ *engine.Key, config vpnconfig.Config) error {
	if config.RejectIPv6 {
		return errors.New("this build does not support local IPv6 rejection; rebuild with the tun2socks fork and gonc_deferred_tcp tag")
	}
	return nil
}
