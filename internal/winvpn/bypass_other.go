//go:build !darwin

package winvpn

import (
	"fmt"
	"net"
	"runtime"
	"time"
)

func selectBypassExit(dns, requested string, previous bypassSelection) (bypassSelection, error) {
	host, _, _ := net.SplitHostPort(dns)
	ipv4 := net.ParseIP(host).To4() != nil
	// Once TUN captures traffic, global route selection can point at TUN.
	// Re-read the physical interface addresses instead of reusing the old IP.
	device := previous.Name
	if runtime.GOOS == "linux" {
		if requested == "" {
			return bypassSelection{}, nil
		} // Use system routing for the tunnel and its bootstrap DNS.
		// Protected capture rules have been suspended before a reconnect.
		device = ""
	}
	if requested != "" && net.ParseIP(requested) == nil {
		device = requested
		requested = ""
	}
	if device != "" {
		iface, err := net.InterfaceByName(device)
		if err != nil {
			return bypassSelection{}, err
		}
		ip, err := interfaceSource(*iface, ipv4, previous.IP, requested)
		return bypassSelection{Name: iface.Name, Index: iface.Index, IP: ip}, err
	}
	local := requested
	if local == "" {
		conn, err := net.DialTimeout("udp", dns, 3*time.Second)
		if err != nil {
			return bypassSelection{}, err
		}
		local = conn.LocalAddr().(*net.UDPAddr).IP.String()
		_ = conn.Close()
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return bypassSelection{}, err
	}
	for _, iface := range interfaces {
		ip, err := interfaceSource(iface, ipv4, "", local)
		if err == nil {
			return bypassSelection{Name: iface.Name, Index: iface.Index, IP: ip}, nil
		}
	}
	return bypassSelection{}, fmt.Errorf("cannot identify physical interface for source %s", local)
}
