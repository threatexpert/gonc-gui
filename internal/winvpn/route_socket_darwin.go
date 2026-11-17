//go:build darwin

package winvpn

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/route"
	"golang.org/x/sys/unix"
)

var darwinRouteSequence atomic.Int32

// x/net/route.LinkAddr marshals an empty address as an 8-byte sockaddr.
// XNU ARP requires at least sizeof(sockaddr_dl), otherwise RTM_ADD succeeds
// but the route never gets LLINFO. Keep sdl_alen=0, with full struct padding.
func unresolvedDarwinGateway(index int) *route.DefaultAddr {
	raw := make([]byte, syscall.SizeofSockaddrDatalink)
	raw[0], raw[1] = byte(len(raw)), syscall.AF_LINK
	binary.NativeEndian.PutUint16(raw[2:4], uint16(index))
	return &route.DefaultAddr{Raw: raw}
}

// route(8) -interface copies the interface's own MAC into the gateway.
// For a physical on-link peer, use an unresolved AF_LINK gateway instead;
// ARP/ND must resolve the peer, not permanently map it to our own hardware.
func darwinLinkRouteRequest(op string, r ownedRoute, iface net.Interface) (*route.RouteMessage, error) {
	if !r.prefix.IsValid() || r.prefix.Bits() != r.prefix.Addr().BitLen() || r.gateway != "" || iface.Index <= 0 {
		return nil, fmt.Errorf("invalid on-link host route")
	}
	typ := syscall.RTM_ADD
	if op == "delete" {
		typ = syscall.RTM_DELETE
	} else if op != "add" {
		return nil, fmt.Errorf("unsupported route operation %q", op)
	}
	var dst route.Addr
	if r.prefix.Addr().Is4() {
		dst = &route.Inet4Addr{IP: r.prefix.Addr().As4()}
	} else {
		a := &route.Inet6Addr{IP: r.prefix.Addr().As16()}
		if r.prefix.Addr().IsLinkLocalUnicast() {
			a.ZoneID = iface.Index
		}
		dst = a
	}
	return &route.RouteMessage{
		Version: syscall.RTM_VERSION, Type: typ,
		Flags: syscall.RTF_UP | syscall.RTF_STATIC | syscall.RTF_HOST,
		Index: iface.Index, ID: uintptr(os.Getpid()), Seq: int(darwinRouteSequence.Add(1)),
		Addrs: []route.Addr{
			syscall.RTAX_DST:     dst,
			syscall.RTAX_GATEWAY: unresolvedDarwinGateway(iface.Index),
			syscall.RTAX_IFP:     &route.LinkAddr{Index: iface.Index, Name: iface.Name},
		},
	}, nil
}

func darwinRouteReply(request *route.RouteMessage, messages []route.Message) (routeMutation, bool) {
	for _, message := range messages {
		m, ok := message.(*route.RouteMessage)
		if !ok || m.ID != request.ID || m.Seq != request.Seq || m.Type != request.Type {
			continue
		}
		if m.Err != nil {
			return routeMutation{routeMutationFailed, m.Err}, true
		}
		return routeMutation{state: routeMutationSucceeded}, true
	}
	return routeMutation{}, false
}

func mutateDarwinLinkRoute(op string, r ownedRoute, iface net.Interface) routeMutation {
	request, err := darwinLinkRouteRequest(op, r, iface)
	if err != nil {
		return routeMutation{routeMutationFailed, err}
	}
	data, err := request.Marshal()
	if err != nil {
		return routeMutation{routeMutationFailed, err}
	}
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err != nil {
		return routeMutation{routeMutationFailed, err}
	}
	defer unix.Close(fd)
	if err = unix.SetNonblock(fd, true); err != nil {
		return routeMutation{routeMutationFailed, err}
	}
	n, err := unix.Write(fd, data)
	if err != nil {
		// A routing socket write returns the kernel operation error (e.g.
		// EEXIST); it must never claim ownership of a preexisting entry.
		return routeMutation{routeMutationFailed, err}
	}
	if n != len(data) {
		return routeMutation{routeMutationUnknown, fmt.Errorf("short routing socket write")}
	}
	deadline := time.Now().Add(3 * time.Second)
	buf := make([]byte, 64*1024)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, err = unix.Poll(poll, int(remaining.Milliseconds())+1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return routeMutation{routeMutationUnknown, err}
		}
		n, err = unix.Read(fd, buf)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			return routeMutation{routeMutationUnknown, err}
		}
		if n == 0 {
			return routeMutation{routeMutationUnknown, fmt.Errorf("routing socket closed before acknowledgement")}
		}
		messages, err := route.ParseRIB(route.RIBTypeRoute, buf[:n])
		if err != nil {
			return routeMutation{routeMutationUnknown, err}
		}
		if result, matched := darwinRouteReply(request, messages); matched {
			return result
		}
	}
	return routeMutation{routeMutationUnknown, fmt.Errorf("routing socket acknowledgement timed out")}
}
