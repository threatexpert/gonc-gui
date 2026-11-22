package winvpn

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"
)

// Protected sessions install ordinary main-table routes while connected. Record
// the exact routes we added so reconnect can withdraw them without touching DNS,
// the TUN device, or a pre-existing LAN/host route.
type protectedRoute struct {
	prefix netip.Prefix
	exit   defaultRoute
	metric int
}

func (r protectedRoute) family() string {
	if r.prefix.Addr().Is6() {
		return "-6"
	}
	return "-4"
}
func (r protectedRoute) change(op string) error {
	args := appendRouteGateway([]string{r.family(), "route", op, r.prefix.String()}, r.exit)
	return run("ip", append(args, "metric", strconv.Itoa(r.metric))...)
}

// Addresses and routes can disappear with the physical device during a Wi-Fi
// change. Treat an already removed route as cleaned up, retaining real errors.
func (r protectedRoute) exists() (bool, error) {
	out, err := exec.Command("ip", "-j", r.family(), "route", "show", "exact", r.prefix.String()).Output()
	if err != nil {
		return false, err
	}
	var routes []struct {
		Dev, Gateway string
		Metric       int
	}
	if err := json.Unmarshal(out, &routes); err != nil {
		return false, err
	}
	for _, entry := range routes {
		if entry.Dev == r.exit.dev && entry.Gateway == r.exit.gateway && entry.Metric == r.metric {
			return true, nil
		}
	}
	return false, nil
}
func (s *Session) addProtectedRoute(r protectedRoute) error {
	if err := r.change("add"); err != nil {
		if exists, checkErr := r.exists(); checkErr == nil && exists {
			return nil
		}
		return fmt.Errorf("install VPN route %s: %w", r.prefix, err)
	}
	s.protectedRoutes = append(s.protectedRoutes, r)
	return nil
}
func (s *Session) ResumeProtectedRoutes(peers []string, seal func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routingActive && slices.Equal(peers, s.config.BypassIPs) {
		return nil
	}
	if err := seal(); err != nil {
		return err
	}
	return s.resumeProtectedRoutes(peers)
}
func (s *Session) resumeProtectedRoutes(peers []string) error {
	if len(peers) == 0 {
		return errors.New("missing tunnel peer for protected routes")
	}
	if err := s.suspendProtectedRoutes(); err != nil {
		return err
	}
	var routes []protectedRoute
	hosts := map[netip.Prefix]bool{}
	for _, peer := range peers {
		ip, err := netip.ParseAddr(peer)
		if err != nil {
			return err
		}
		if ip.Is6() && !s.config.EnableIPv6 {
			continue
		}
		p := netip.PrefixFrom(ip, ip.BitLen())
		if hosts[p] {
			continue
		}
		exit, err := currentRouteTo(peer)
		if err != nil {
			return err
		}
		hosts[p] = true
		routes = append(routes, protectedRoute{p, exit, 1})
	}
	for _, cidr := range s.config.Routes {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			return err
		}
		if !hosts[p] {
			routes = append(routes, protectedRoute{p, defaultRoute{dev: interfaceName}, 5})
		}
	}
	for _, r := range routes {
		if err := s.addProtectedRoute(r); err != nil {
			return errors.Join(err, s.suspendProtectedRoutes())
		}
	}
	s.config.BypassIPs = slices.Clone(peers)
	s.routingActive = true
	return nil
}
func (s *Session) SuspendProtectedRoutes() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suspendProtectedRoutes()
}
func (s *Session) suspendProtectedRoutes() error {
	s.routingActive = false
	for len(s.protectedRoutes) > 0 {
		i := len(s.protectedRoutes) - 1
		r := s.protectedRoutes[i]
		if err := r.change("del"); err != nil {
			if exists, checkErr := r.exists(); checkErr != nil || exists {
				return errors.Join(err, checkErr)
			}
		}
		s.protectedRoutes = s.protectedRoutes[:i]
	}
	return nil
}
