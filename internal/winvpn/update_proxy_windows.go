package winvpn

func (s *Session) updateDNSProxy(endpoint string) error {
	if s.dnsProxy != nil {
		return s.dnsProxy.UpdateProxy(endpoint)
	}
	return nil
}
