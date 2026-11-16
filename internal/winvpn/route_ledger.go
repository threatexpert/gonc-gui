package winvpn

import (
	"errors"
	"fmt"
	"strings"
)

type ownedRoute struct {
	cidr, iface, gateway string
	index                int // Original interface identity, not a reused utun name.
}

// The ledger never treats the requested configuration as proof of ownership.
// It is independent of OS calls so failure/rollback paths can be tested locally.
type routeLedger struct {
	owned   []ownedRoute
	present func(ownedRoute) (bool, error)
	command func(string, ownedRoute) error
}

func (l *routeLedger) add(r ownedRoute) error {
	installed, err := l.present(r)
	if err != nil {
		return fmt.Errorf("inspect route %s: %w", r.cidr, err)
	}
	if installed {
		return nil
	}
	commandErr := l.command("add", r)
	installed, verifyErr := l.present(r)
	// Retain candidates even if verification failed: cleanup must query them
	// again instead of forgetting possibly-created routes after a partial start.
	if commandErr == nil || installed {
		l.owned = append(l.owned, r)
	}
	if commandErr != nil {
		return commandErr
	}
	if verifyErr != nil {
		return fmt.Errorf("verify route %s: %w", r.cidr, verifyErr)
	}
	if !installed {
		return fmt.Errorf("route %s was not installed on %s; VPN is not ready", r.cidr, r.iface)
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
	args := []string{"-n", op, family, "-net", r.cidr}
	if r.gateway != "" {
		return append(args, r.gateway)
	}
	return append(args, "-interface", r.iface)
}
