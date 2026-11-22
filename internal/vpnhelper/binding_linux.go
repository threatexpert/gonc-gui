//go:build linux && !android

package vpnhelper

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/threatexpert/gonc/v2/netx"
	"golang.org/x/sys/unix"
	"gonc-gui/internal/vpnconfig"
	"gonc-gui/internal/winvpn"
)

func verifyElevatedPeer(c net.Conn) error {
	peer, err := unixPeer(c)
	if err != nil {
		return err
	}
	if peer.Uid != 0 {
		return fmt.Errorf("VPN helper must run as root")
	}
	return nil
}
func (c *Client) TunnelNetwork(local, dns string) (*netx.TunnelNetwork, error) {
	c.bindingMu.Lock()
	defer c.bindingMu.Unlock()
	address, control := c.bindAddress, c.conn
	if address == "" || control == nil {
		return nil, errors.New("tunnel socket helper unavailable")
	}
	return netx.NewTunnelNetwork(dns, local, func(raw syscall.RawConn) error {
		return requestSocketBinding(control, address, raw, request{Local: local})
	})
}

func unixPeer(c net.Conn) (*unix.Ucred, error) {
	s, ok := c.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("socket binding requires a Unix control channel")
	}
	raw, err := s.SyscallConn()
	if err != nil {
		return nil, err
	}
	var peer *unix.Ucred
	var sockErr error
	err = raw.Control(func(fd uintptr) { peer, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	return peer, errors.Join(err, sockErr)
}

// A separate seqpacket channel lets new tunnel sockets be configured while the
// control channel is busy starting VPN routes or updating the SOCKS endpoint.
func requestSocketBinding(control net.Conn, address string, raw syscall.RawConn, req request) error {
	expected, err := unixPeer(control)
	if err != nil {
		return err
	}
	c, err := net.DialTimeout("unixpacket", address, 3*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	peer, err := unixPeer(c)
	if err != nil {
		return err
	}
	if peer.Uid != 0 || *peer != *expected {
		return fmt.Errorf("socket binder is not the elevated VPN helper")
	}
	conn := c.(*net.UnixConn)
	if err := conn.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	var sendErr error
	err = raw.Control(func(fd uintptr) { _, _, sendErr = conn.WriteMsgUnix(data, unix.UnixRights(int(fd)), nil) })
	if err != nil || sendErr != nil {
		return errors.Join(err, sendErr)
	}
	_, err = readResponse(json.NewDecoder(conn), nil)
	return err
}

type bindingService struct {
	*winvpn.BypassSession
	ln     *net.UnixListener
	parent unix.Ucred
	mark   uint32
	guard  *winvpn.EgressGuard
	done   chan struct{}
}

func prepareBindingService(control net.Conn, session *winvpn.BypassSession) (bypassSession, string, error) {
	peer, err := unixPeer(control)
	if err != nil {
		return nil, "", err
	}
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", err
	}
	name, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	ln, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: "@gonc-bind-" + name[:32], Net: "unixpacket"})
	if err != nil {
		return nil, "", err
	}
	b := &bindingService{BypassSession: session, ln: ln, parent: *peer, mark: binary.BigEndian.Uint32(random[:]) | 1, done: make(chan struct{})}
	go b.serve()
	return b, ln.Addr().String(), nil
}
func (b *bindingService) configureDNSBypass(config *vpnconfig.Config) error {
	_, _, dns, err := b.CurrentExit()
	if err != nil {
		return err
	}
	config.LinuxDNSBypass = dns
	return nil
}
func (b *bindingService) Close() error {
	_ = b.ln.Close()
	<-b.done
	return errors.Join(b.BypassSession.Close(), b.guard.Close())
}

func (b *bindingService) egressMark() uint32 { return b.mark }

func (b *bindingService) sealOutput() error {
	if b.guard != nil {
		if b.guard.Ready() {
			return nil
		}
		if err := b.guard.Close(); err != nil {
			return err
		}
	}
	guard, err := winvpn.InstallEgressGuard(b.mark)
	b.guard = guard // retain partial setup for teardown
	return err
}

func (b *bindingService) openOutput() error {
	if err := b.guard.Close(); err != nil {
		return err
	}
	b.guard = nil
	return nil
}

func (c *Client) SealOutput() error { _, err := c.call(request{Op: OpSeal}); return err }

func (c *Client) ResumeProtectedRoutes(peers []string) error {
	_, err := c.call(request{Op: OpResume, Config: vpnconfig.Config{BypassIPs: peers}})
	return err
}
func (b *bindingService) serve() {
	defer close(b.done)
	for {
		conn, err := b.ln.AcceptUnix()
		if err != nil {
			return
		}
		if peer, err := unixPeer(conn); err == nil && *peer == b.parent {
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			resp := response{OK: true}
			if err := b.bindRequest(conn); err != nil {
				resp = response{Error: err.Error()}
			}
			_ = json.NewEncoder(conn).Encode(resp)
		}
		conn.Close()
	}
}

func (b *bindingService) bindRequest(c *net.UnixConn) error {
	data, oob := make([]byte, 1024), make([]byte, unix.CmsgSpace(4))
	n, on, flags, _, err := c.ReadMsgUnix(data, oob)
	if err != nil {
		return err
	}
	msgs, err := unix.ParseSocketControlMessage(oob[:on])
	if err != nil {
		return err
	}
	var fds []int
	for i := range msgs {
		got, err := unix.ParseUnixRights(&msgs[i])
		if err != nil {
			return err
		}
		for _, fd := range got {
			defer unix.Close(fd)
		}
		fds = append(fds, got...)
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || len(fds) != 1 {
		return fmt.Errorf("expected exactly one socket")
	}
	var req request
	if err := json.Unmarshal(data[:n], &req); err != nil {
		return err
	}
	dev, local, dns, err := b.CurrentExit()
	if err != nil {
		return err
	}
	if req.Local != local || (req.Address != "" && req.Address != dns) {
		return fmt.Errorf("socket does not match the prepared VPN bypass")
	}
	fd := fds[0]
	addr, err := unix.Getsockname(fd)
	if err != nil {
		return err
	}
	switch a := addr.(type) {
	case *unix.SockaddrInet4:
		if a.Port != 0 {
			return fmt.Errorf("socket already bound")
		}
	case *unix.SockaddrInet6:
		if a.Port != 0 {
			return fmt.Errorf("socket already bound")
		}
	default:
		return fmt.Errorf("expected an Internet socket")
	}
	proto, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if err != nil {
		return err
	}
	kind, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil {
		return err
	}
	if !(proto == unix.IPPROTO_TCP && kind == unix.SOCK_STREAM || proto == unix.IPPROTO_UDP && kind == unix.SOCK_DGRAM) {
		return fmt.Errorf("expected TCP or UDP socket")
	}
	if b.ExplicitSource() {
		if err := unix.BindToDevice(fd, dev); err != nil {
			return fmt.Errorf("bind socket to %s: %w", dev, err)
		}
	}
	return unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, int(b.mark))
}
