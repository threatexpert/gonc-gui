//go:build windows || darwin || linux

package winvpn

import (
	"gonc-gui/internal/tunengine"
	"gonc-gui/internal/vpnconfig"
)

func engineConfig(config vpnconfig.Config, device string) tunengine.Config {
	return tunengine.Config{
		Device: device, Proxy: config.SOCKS5Endpoint, MTU: config.MTU, LogLevel: config.LogLevel,
		DeferredTCP: &tunengine.DeferredTCPConfig{}, RejectIPv6: config.RejectIPv6,
	}
}

func startEngine(config vpnconfig.Config, device string) error {
	return tunengine.Start(engineConfig(config, device))
}

func stopEngine() error { return tunengine.Stop() }
