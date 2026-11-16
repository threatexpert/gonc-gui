package winvpn

import (
	"fmt"
	"net/netip"
	"strings"
)

type routeMutationState uint8

const (
	routeMutationUnknown routeMutationState = iota
	routeMutationSucceeded
	routeMutationFailed
)

// A command acknowledgement is separate from the observed kernel state.
// Only an acknowledged add gives the ledger evidence of route ownership.
type routeMutation struct {
	state routeMutationState
	err   error
}

func (r routeMutation) failure() error {
	if r.state == routeMutationSucceeded {
		return nil
	}
	if r.err != nil {
		return r.err
	}
	return fmt.Errorf("route mutation was not acknowledged (state=%d)", r.state)
}

func routeArgs(op string, r ownedRoute) []string {
	family := "-inet"
	if r.prefix.Addr().Is6() {
		family = "-inet6"
	}
	kind, destination := routeDestination(r.prefix)
	args := []string{"-n", op, family, kind, destination}
	if r.gateway != "" {
		// -ifp selects the outgoing interface without making the route scoped.
		return append(args, r.gateway, "-ifp", r.iface)
	}
	return append(args, "-interface", r.iface)
}

func routeDestination(p netip.Prefix) (kind, destination string) {
	if p.Bits() == p.Addr().BitLen() {
		return "-host", p.Addr().String()
	}
	return "-net", p.String()
}

// route(8) can exit zero on a kernel error. Decode its acknowledgement by
// fields, not an exact sentence. Extra diagnostics must not become success.
// Unknown output is NOT evidence that the mutation failed or that we own it.
func parseDarwinRouteMutation(op string, r ownedRoute, output string, commandErr error) routeMutation {
	actual := strings.TrimSpace(output)
	if commandErr != nil {
		// An interrupted process may have mutated the kernel before exiting.
		// A process error alone is not proof of a rejected route operation.
		return routeMutation{routeMutationUnknown, fmt.Errorf("route %s: %w; output: %s", strings.Join(routeArgs(op, r), " "), commandErr, actual)}
	}
	fields := strings.Fields(actual)
	kind, _ := routeDestination(r.prefix)
	// Success is: add|delete host|net DEST: gateway NEXT_HOP.
	// IPv6 ':' characters are part of addresses; remove only the final
	// punctuation, never split the whole line on ':'.
	if len(fields) >= 5 && fields[0] == op && fields[1] == strings.TrimPrefix(kind, "-") &&
		strings.HasSuffix(fields[2], ":") && fields[3] == "gateway" &&
		sameRouteDestination(strings.TrimSuffix(fields[2], ":"), r.prefix) {
		gateway := fields[4]
		if len(fields) > 5 {
			gateway = strings.TrimSuffix(gateway, ":")
		}
		matches := gateway == r.iface
		if r.gateway != "" {
			matches = sameDarwinGateway(gateway, r.gateway, r.iface)
		}
		if matches {
			if len(fields) == 5 {
				return routeMutation{state: routeMutationSucceeded}
			}
			// Apple's failure summary appends ': strerror(errno)'. Recognize
			// known failures, but leave unfamiliar suffixes as uncertain.
			if strings.HasSuffix(fields[4], ":") {
				switch strings.Join(fields[5:], " ") {
				case "File exists", "not in table", "Network is unreachable", "No such process", "Permission denied", "Operation not permitted", "Invalid argument":
					return routeMutation{routeMutationFailed, fmt.Errorf("route %s rejected: %s", op, actual)}
				}
			}
		}
	}
	return routeMutation{routeMutationUnknown, fmt.Errorf("route %s: unrecognized acknowledgement: %s", op, actual)}
}

func sameRouteDestination(printed string, want netip.Prefix) bool {
	if !want.IsValid() {
		return false
	}
	if got, err := netip.ParsePrefix(printed); err == nil {
		return got.Masked() == want.Masked()
	}
	got, err := netip.ParseAddr(printed)
	if err != nil || got != want.Addr() {
		return false
	}
	// IPv4 route(8) removes a network's CIDR suffix when printing it.
	// Hosts (including IPv6) are always printed as a plain address.
	return want.Addr().Is4() || want.Bits() == want.Addr().BitLen()
}
