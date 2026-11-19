package dnsproxy

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyUpdatePreservesDNSListenerAndReplacesAssociation(t *testing.T) {
	a, _, closeA := startTestSocks5UDPServer(t)
	defer closeA()
	b, countB, closeB := startTestSocks5UDPServer(t)
	defer closeB()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{udp: udp, socks5Endpoint: a, done: make(chan struct{}), pending: map[uint16]pendingRequest{}}
	defer s.Close()
	if !s.rebuildAssociation(nil) {
		t.Fatal("initial association failed")
	}
	old := s.currentAssociation()
	group := s.newPendingGroup(nil, 1, 0, time.Now(), time.Second)
	s.pending[1] = pendingRequest{group: group}
	if err := s.UpdateProxy(b); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.Done():
	default:
		t.Fatal("old association retained")
	}
	if !group.completed || len(s.pending) != 0 {
		t.Fatal("pending DNS was not invalidated")
	}
	if err := udp.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal("DNS listener closed", err)
	}
	if !s.rebuildAssociation(nil) || atomic.LoadInt32(countB) != 1 {
		t.Fatal("new association not using updated proxy")
	}
	current := s.currentAssociation()
	if err := s.UpdateProxy(b); err != nil || s.currentAssociation() != current {
		t.Fatal("same endpoint replaced association", err)
	}
	if err := s.UpdateProxy("http://127.0.0.1:1"); err == nil || s.currentAssociation() != current {
		t.Fatal("invalid update changed association")
	}
}

func TestProxyUpdateRejectsStaleReconnectResult(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{})
	release := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		close(accepted)
		<-release
		u, e := handleTestSocks5UDPAssociate(c)
		if e == nil {
			u.Close()
		}
	}()
	s := &Server{socks5Endpoint: "socks5://" + ln.Addr().String(), done: make(chan struct{}), pending: map[uint16]pendingRequest{}}
	defer s.Close()
	result := make(chan bool, 1)
	go func() { result <- s.rebuildAssociation(nil) }()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("old reconnect not started")
	}
	if err := s.UpdateProxy("socks5://127.0.0.1:12345"); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case got := <-result:
		if got || s.currentAssociation() != nil {
			t.Fatal("old reconnect overwrote updated endpoint")
		}
	case <-time.After(time.Second):
		t.Fatal("reconnect did not finish")
	}
	<-serverDone
}
