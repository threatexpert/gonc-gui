package winvpn

import (
	"errors"
	"net/netip"
	"testing"
)

func TestDarwinRouteMutationStates(t *testing.T) {
	r := ownedRoute{prefix: netip.MustParsePrefix("2001:db8::abcd/128"), iface: "utun4"}
	processErr := errors.New("interrupted")
	for _, tc := range []struct {
		name, output string
		err          error
		want         routeMutationState
	}{
		{"success", "add host 2001:db8::abcd: gateway utun4", nil, routeMutationSucceeded},
		{"semantic IPv6", "  add\thost  2001:0DB8:0:0:0:0:0:ABCD:   gateway\tutun4\r\n", nil, routeMutationSucceeded},
		{"exists", "add host 2001:db8::abcd: gateway utun4: File exists", nil, routeMutationFailed},
		{"unreachable", "add host 2001:db8::abcd: gateway utun4: Network is unreachable", nil, routeMutationFailed},
		{"empty", "", nil, routeMutationUnknown},
		{"new format", "route added successfully", nil, routeMutationUnknown},
		{"wrong address", "add host 2001:db8::abce: gateway utun4", nil, routeMutationUnknown},
		{"wrong interface", "add host 2001:db8::abcd: gateway utun5", nil, routeMutationUnknown},
		{"wrong operation", "delete host 2001:db8::abcd: gateway utun4", nil, routeMutationUnknown},
		{"extra diagnostic", "add host 2001:db8::abcd: gateway utun4\nroute: error", nil, routeMutationUnknown},
		{"unknown suffix", "add host 2001:db8::abcd: gateway utun4: unfamiliar response", nil, routeMutationUnknown},
		{"process interrupted after mutation", "add host 2001:db8::abcd: gateway utun4", processErr, routeMutationUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDarwinRouteMutation("add", r, tc.output, tc.err)
			if got.state != tc.want || (got.failure() == nil) != (tc.want == routeMutationSucceeded) {
				t.Fatalf("got %+v, want %v", got, tc.want)
			}
			if tc.err != nil && !errors.Is(got.failure(), tc.err) {
				t.Fatal("lost process error", got.failure())
			}
		})
	}
}

func TestDarwinRouteMutationSemanticGatewayAndNetwork(t *testing.T) {
	r := ownedRoute{prefix: netip.MustParsePrefix("2001:db8::/32"), iface: "en0", gateway: "fe80::1%en0"}
	for _, op := range []string{"add", "delete"} {
		result := parseDarwinRouteMutation(op, r, op+" net 2001:0DB8:0:0::/32: gateway fe80:0:0:0:0:0:0:1%en0", nil)
		if result.state != routeMutationSucceeded {
			t.Fatal(result)
		}
		for _, output := range []string{
			op + " net 2001:db8::/33: gateway fe80::1%en0",
			op + " net 2001:db8::/32: gateway fe80::1%en1",
			op + " net 2001:db8::/32: gateway fe80::2%en0",
		} {
			if got := parseDarwinRouteMutation(op, r, output, nil); got.state == routeMutationSucceeded {
				t.Fatal("accepted wrong route", output)
			}
		}
	}
}
