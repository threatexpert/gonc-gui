//go:build !linux || android

package vpnhelper

import (
	"gonc-gui/internal/winvpn"
	"net"
)

func verifyElevatedPeer(net.Conn) error            { return nil }
func registerSocketBinding(*Client, string) func() { return nil }
func prepareBindingService(_ net.Conn, b *winvpn.BypassSession) (bypassSession, string, error) {
	return b, "", nil
}
