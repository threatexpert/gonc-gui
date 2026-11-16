//go:build (windows || darwin || linux) && !gonc_deferred_tcp

package winvpn

import (
	"github.com/xjasonlyu/tun2socks/v2/engine"
	"gonc-gui/internal/vpnconfig"
	"testing"
)

func TestUnsupportedBuildDoesNotSilentlyIgnoreRejection(t *testing.T) {
	if err := configureEnginePolicy(&engine.Key{}, vpnconfig.Config{RejectIPv6: true}); err == nil {
		t.Fatal("unsupported policy silently ignored")
	}
	if err := configureEnginePolicy(&engine.Key{}, vpnconfig.Config{}); err != nil {
		t.Fatal(err)
	}
}
