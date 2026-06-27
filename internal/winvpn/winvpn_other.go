//go:build !windows

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

func (s *Session) Stop() error { return nil }
