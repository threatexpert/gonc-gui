//go:build !linux || android

package vpnhelper

import (
	"github.com/threatexpert/gonc/v2/netx"
	"gonc-gui/internal/winvpn"
	"net"
)

func verifyElevatedPeer(net.Conn) error { return nil }

// These platforms retain the original capture routes throughout reconnect.
func (*Client) SealOutput() error                                         { return nil }
func (*Client) ResumeProtectedRoutes([]string) error                      { return nil }
func (*Client) TunnelNetwork(string, string) (*netx.TunnelNetwork, error) { return nil, nil }
func prepareBindingService(_ net.Conn, b *winvpn.BypassSession) (bypassSession, string, error) {
	return b, "", nil
}
