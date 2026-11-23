//go:build windows || darwin || linux

package winvpn

import "gonc-gui/internal/tunengine"

func (s *Session) UpdateProxy(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if endpoint == s.config.SOCKS5Endpoint {
		return nil
	}
	if err := tunengine.UpdateProxy(endpoint); err != nil {
		return err
	}
	if err := s.updateDNSProxy(endpoint); err != nil {
		_ = tunengine.UpdateProxy(s.config.SOCKS5Endpoint)
		return err
	}
	s.config.SOCKS5Endpoint = endpoint
	return nil
}
