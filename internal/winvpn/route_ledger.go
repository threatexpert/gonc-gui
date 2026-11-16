package winvpn

import (
	"errors"
	"fmt"
	"net/netip"
)

type ownedRoute struct {
	prefix         netip.Prefix
	iface, gateway string
	index          int // Original interface identity, not a reused utun name.
}

// The ledger never treats the requested configuration as proof of ownership.
// It is independent of OS calls so failure/rollback paths can be tested locally.
type routeLedger struct {
	owned     []ownedRoute
	uncertain []ownedRoute // Never delete without evidence of a successful add.
	present   func(ownedRoute) (bool, error)
	command   func(string, ownedRoute) routeMutation
	trace     func(string)
	diagnose  func(ownedRoute) string
}

func (l *routeLedger) add(r ownedRoute) error {
	if !r.prefix.IsValid() {
		return fmt.Errorf("invalid route prefix")
	}
	installed, err := l.present(r)
	if err != nil {
		return fmt.Errorf("inspect route %s: %w", r.prefix, err)
	}
	if installed {
		if l.trace != nil {
			l.trace(fmt.Sprintf("VPN route %s: reuse static route via %s on %s", r.prefix, r.gateway, r.iface))
		}
		return nil
	}
	result := l.command("add", r)
	installed, verifyErr := l.present(r)
	// Retain candidates even if verification failed: cleanup must query them
	// again instead of forgetting possibly-created routes after a partial start.
	// A failed add (including EEXIST) must never claim someone else's route,
	// even if a matching entry appeared between the two queries.
	if result.state == routeMutationSucceeded {
		l.owned = append(l.owned, r)
	}
	if result.state == routeMutationUnknown {
		l.uncertain = append(l.uncertain, r)
		l.logDiagnostics(r)
		return errors.Join(fmt.Errorf("route %s on %s: add outcome uncertain; ownership not claimed (present=%t): %w", r.prefix, r.iface, installed, result.failure()), verifyErr)
	}
	if result.state != routeMutationSucceeded {
		return result.failure()
	}
	if verifyErr != nil {
		l.logDiagnostics(r)
		return fmt.Errorf("verify route %s: %w", r.prefix, verifyErr)
	}
	if !installed {
		l.logDiagnostics(r)
		return fmt.Errorf("route %s was not installed on %s; VPN is not ready", r.prefix, r.iface)
	}
	if l.trace != nil {
		l.trace(fmt.Sprintf("VPN route %s: created static route via %s on %s", r.prefix, r.gateway, r.iface))
	}
	return nil
}

func (l *routeLedger) logDiagnostics(r ownedRoute) {
	if l.trace != nil && l.diagnose != nil {
		l.trace(fmt.Sprintf("VPN route %s diagnostics: %s", r.prefix, l.diagnose(r)))
	}
}

func (l *routeLedger) cleanup() error {
	var failures []error
	var retry []ownedRoute
	for i := len(l.owned) - 1; i >= 0; i-- {
		r := l.owned[i]
		installed, err := l.present(r)
		if err == nil && installed {
			result := l.command("delete", r)
			// For removal, verified absence is enough, even if the command's
			// output changed or another actor removed the route concurrently.
			installed, err = l.present(r)
			if err != nil || installed {
				err = errors.Join(err, result.failure())
				if err == nil {
					err = fmt.Errorf("route still installed")
				}
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("cleanup %s on %s: %w", r.prefix, r.iface, err))
			retry = append([]ownedRoute{r}, retry...)
		}
	}
	l.owned = retry
	var uncertain []ownedRoute
	for _, r := range l.uncertain {
		installed, err := l.present(r)
		if err != nil || installed {
			uncertain = append(uncertain, r)
			failures = append(failures, errors.Join(fmt.Errorf("route %s on %s has uncertain ownership; left untouched", r.prefix, r.iface), err))
		}
	}
	l.uncertain = uncertain
	return errors.Join(failures...)
}
