//go:build !windows && !linux && !darwin

package winvpn

import (
	"fmt"

	"gonc-gui/internal/vpnconfig"
)

type Session struct{}

func CheckRuntime() error {
	return fmt.Errorf("Windows VPN is only supported on Windows")
}

func Start(config vpnconfig.Config) (*Session, error) {
	return nil, fmt.Errorf("Windows VPN is only supported on Windows")
}

func StartWithTrace(config vpnconfig.Config, trace func(string)) (*Session, error) {
	return Start(config)
}

func (s *Session) Stop() error { return nil }
