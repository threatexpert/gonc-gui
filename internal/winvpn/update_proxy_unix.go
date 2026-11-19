//go:build darwin || linux

package winvpn

func (s *Session) updateDNSProxy(string) error { return nil }
