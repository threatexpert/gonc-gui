package winvpn

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type ownedRoute struct {
	cidr, iface, gateway string
	index                int // Original interface identity, not a reused utun name.
}

// The ledger never treats the requested configuration as proof of ownership.
// It is independent of OS calls so failure/rollback paths can be tested locally.
type routeLedger struct {
	owned    []ownedRoute
	present  func(ownedRoute) (bool, error)
	command  func(string, ownedRoute) error
	trace    func(string)
	diagnose func(ownedRoute) string
}

func (l *routeLedger) add(r ownedRoute) error {
	installed, err := l.present(r)
	if err != nil {
		return fmt.Errorf("inspect route %s: %w", r.cidr, err)
	}
	if installed {
		if l.trace != nil {
			l.trace(fmt.Sprintf("VPN route %s: reuse static route via %s on %s", r.cidr, r.gateway, r.iface))
		}
		return nil
	}
	commandErr := l.command("add", r)
	installed, verifyErr := l.present(r)
	// Retain candidates even if verification failed: cleanup must query them
	// again instead of forgetting possibly-created routes after a partial start.
	// A failed add (including EEXIST) must never claim someone else's route,
	// even if a matching entry appeared between the two queries.
	if commandErr == nil {
		l.owned = append(l.owned, r)
	}
	if commandErr != nil {
		return commandErr
	}
	if verifyErr != nil {
		return fmt.Errorf("verify route %s: %w", r.cidr, verifyErr)
	}
	if !installed {
		detail := ""
		if l.diagnose != nil {
			detail = "; " + l.diagnose(r)
		}
		return fmt.Errorf("route %s was not installed on %s; VPN is not ready%s", r.cidr, r.iface, detail)
	}
	if l.trace != nil {
		l.trace(fmt.Sprintf("VPN route %s: created static route via %s on %s", r.cidr, r.gateway, r.iface))
	}
	return nil
}

func (l *routeLedger) cleanup() error {
	var failures []error
	var retry []ownedRoute
	for i := len(l.owned) - 1; i >= 0; i-- {
		r := l.owned[i]
		installed, err := l.present(r)
		if err == nil && installed {
			err = l.command("delete", r)
			if err == nil {
				installed, err = l.present(r)
				if err == nil && installed {
					err = fmt.Errorf("route still installed")
				}
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("cleanup %s on %s: %w", r.cidr, r.iface, err))
			retry = append([]ownedRoute{r}, retry...)
		}
	}
	l.owned = retry
	return errors.Join(failures...)
}

func routeArgs(op string, r ownedRoute) []string {
	family := "-inet"
	if strings.Contains(r.cidr, ":") {
		family = "-inet6"
	}
	kind, destination := routeDestination(r.cidr)
	args := []string{"-n", op, family, kind, destination}
	if r.gateway != "" {
		// -ifp selects the outgoing interface without making the route scoped
		// (-ifscope would not protect ordinary, unbound tunnel sockets).
		return append(args, r.gateway, "-ifp", r.iface)
	}
	return append(args, "-interface", r.iface)
}

func routeDestination(cidr string) (kind, destination string) {
	if p, err := netip.ParsePrefix(cidr); err == nil && p.Bits() == p.Addr().BitLen() {
		return "-host", p.Addr().String()
	}
	return "-net", cidr
}

// route(8) may exit zero after a kernel error. Recognize its successful mutation
// output, including IPv4 CIDR suffix removal, but never ignore an error suffix.
// This is NOT proof of route installation: the ledger also checks the exact
// prefix, interface and gateway in the kernel RIB before reporting success.
func checkDarwinRouteOutput(op string, r ownedRoute, output string, commandErr error) error {
	kind, destination := routeDestination(r.cidr)
	gateway := r.gateway
	if gateway == "" {
		gateway = r.iface
	}
	want := fmt.Sprintf("%s %s %s: gateway %s", op, strings.TrimPrefix(kind, "-"), destination, gateway)
	if commandErr != nil {
		return fmt.Errorf("route %s: %w; output: %s", strings.Join(routeArgs(op, r), " "), commandErr, strings.TrimSpace(output))
	}
	actual := strings.TrimSpace(output)
	if actual == want {
		return nil
	}
	// Apple's IPv4 getaddr() replaces '/' with NUL while parsing a network,
	// and does not restore it before printing dest. IPv6 restores the slash.
	if p, err := netip.ParsePrefix(destination); err == nil && p.Addr().Is4() && kind == "-net" {
		want = fmt.Sprintf("%s net %s: gateway %s", op, p.Addr(), gateway)
	}
	if actual != want {
		return fmt.Errorf("route %s: unexpected result: %s", strings.Join(routeArgs(op, r), " "), strings.TrimSpace(output))
	}
	return nil
}
