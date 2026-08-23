package dnsproxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEncodeDecodeSocksUDPIPv4(t *testing.T) {
	payload := []byte{0x12, 0x34, 0x01, 0x00}
	packet, err := encodeSocksUDP(socksAddr{ip: net.ParseIP("8.8.8.8"), port: 53}, payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeSocksUDP(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded payload mismatch: got %x want %x", decoded, payload)
	}
}

func TestParseUpstream(t *testing.T) {
	tests := []struct {
		value string
		host  string
		port  int
	}{
		{"8.8.8.8", "8.8.8.8", 53},
		{"1.1.1.1:5353", "1.1.1.1", 5353},
		{"2001:4860:4860::8888", "2001:4860:4860::8888", 53},
		{"[2001:4860:4860::8888]:5353", "2001:4860:4860::8888", 5353},
		{"dns.example:5353", "dns.example", 5353},
	}
	for _, tt := range tests {
		got, err := parseUpstream(tt.value)
		if err != nil {
			t.Fatalf("parseUpstream(%q): %v", tt.value, err)
		}
		if got.host != tt.host || got.port != tt.port {
			t.Fatalf("parseUpstream(%q) = %s:%d, want %s:%d", tt.value, got.host, got.port, tt.host, tt.port)
		}
	}
}

func TestUpstreamPlanSingleDNSStaysSingleAttempt(t *testing.T) {
	server := &Server{upstreams: make([]socksAddr, 1)}
	got := server.upstreamPlan(time.Now())
	want := []upstreamAttempt{{index: 0}}
	assertAttempts(t, got, want)
}

func TestUpstreamPlanUsesPrimaryThenFallbacks(t *testing.T) {
	server := &Server{upstreams: make([]socksAddr, 4)}
	got := server.upstreamPlan(time.Now())
	want := []upstreamAttempt{
		{index: 0},
		{index: 1, delay: 2 * time.Second},
		{index: 2, delay: 4 * time.Second},
		{index: 0, delay: 6 * time.Second},
		{index: 1, delay: 6 * time.Second},
		{index: 2, delay: 6 * time.Second},
		{index: 3, delay: 6 * time.Second},
		{index: 0, delay: 9 * time.Second},
		{index: 1, delay: 9 * time.Second},
		{index: 2, delay: 9 * time.Second},
		{index: 3, delay: 9 * time.Second},
	}
	assertAttempts(t, got, want)
}

func TestUpstreamPlanKeepsPrimaryBeforeFailureThreshold(t *testing.T) {
	now := time.Now()
	server := &Server{upstreams: make([]socksAddr, 3)}
	server.noteUpstreamResponse(0, 1, now, now.Add(3*time.Second))
	server.noteUpstreamResponse(0, 1, now.Add(4*time.Second), now.Add(7*time.Second))

	got := server.upstreamPlan(now.Add(8 * time.Second))
	if got[0].index != 0 {
		t.Fatalf("first upstream before threshold = %d, want 0", got[0].index)
	}
}

func TestUpstreamPlanUsesHealthyFallbackAfterFailureThreshold(t *testing.T) {
	now := time.Now()
	server := &Server{upstreams: make([]socksAddr, 3)}
	for i := 0; i < primaryFailureThreshold; i++ {
		started := now.Add(time.Duration(i) * 4 * time.Second)
		server.noteUpstreamResponse(0, 1, started, started.Add(3*time.Second))
	}

	got := server.upstreamPlan(now.Add(13 * time.Second))
	want := []upstreamAttempt{
		{index: 1},
		{index: 2, delay: 2 * time.Second},
		{index: 0, delay: 4 * time.Second},
		{index: 0, delay: 6 * time.Second},
		{index: 1, delay: 6 * time.Second},
		{index: 2, delay: 6 * time.Second},
		{index: 0, delay: 9 * time.Second},
		{index: 1, delay: 9 * time.Second},
		{index: 2, delay: 9 * time.Second},
	}
	assertAttempts(t, got, want)
}

func TestUpstreamPlanProbesPrimaryAndRestoresIt(t *testing.T) {
	now := time.Now()
	server := &Server{upstreams: make([]socksAddr, 3)}
	var lastFailure time.Time
	for i := 0; i < primaryFailureThreshold; i++ {
		started := now.Add(time.Duration(i) * 4 * time.Second)
		lastFailure = started.Add(3 * time.Second)
		server.noteUpstreamResponse(0, 1, started, lastFailure)
	}

	got := server.upstreamPlan(lastFailure.Add(primaryRetryInterval + time.Second))
	if got[0].index != 0 {
		t.Fatalf("first probe upstream = %d, want 0", got[0].index)
	}

	started := lastFailure.Add(primaryRetryInterval + time.Second)
	server.noteUpstreamResponse(0, 0, started, started.Add(time.Second))
	got = server.upstreamPlan(started.Add(2 * time.Second))
	if got[0].index != 0 {
		t.Fatalf("first upstream after restore = %d, want 0", got[0].index)
	}
}

func assertAttempts(t *testing.T, got []upstreamAttempt, want []upstreamAttempt) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("attempt count = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestMonitorAssociationRebuildsAfterControlClose(t *testing.T) {
	endpoint, count, cleanup := startTestSocks5UDPServer(t)
	defer cleanup()

	server := &Server{
		socks5Endpoint: endpoint,
		done:           make(chan struct{}),
		pending:        map[uint16]pendingRequest{},
	}
	assoc, err := newUDPAssociation(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server.setAssociation(assoc)
	server.wg.Add(1)
	go server.monitorAssociation()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(count) >= 2 {
			close(server.done)
			if current := server.currentAssociation(); current != nil {
				_ = current.Close()
			}
			server.wg.Wait()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(server.done)
	if current := server.currentAssociation(); current != nil {
		_ = current.Close()
	}
	server.wg.Wait()
	t.Fatalf("association was not rebuilt, count=%d", atomic.LoadInt32(count))
}

func startTestSocks5UDPServer(t *testing.T) (string, *int32, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var count int32
	var mu sync.Mutex
	var conns []net.Conn
	var udpConns []*net.UDPConn
	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			go func() {
				id := atomic.AddInt32(&count, 1)
				udpConn, err := handleTestSocks5UDPAssociate(conn)
				if err != nil {
					_ = conn.Close()
					return
				}
				mu.Lock()
				udpConns = append(udpConns, udpConn)
				mu.Unlock()
				if id == 1 {
					time.Sleep(80 * time.Millisecond)
					_ = conn.Close()
					_ = udpConn.Close()
					return
				}
				select {
				case <-done:
				case <-time.After(5 * time.Second):
				}
			}()
		}
	}()

	cleanup := func() {
		close(done)
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
		for _, conn := range udpConns {
			_ = conn.Close()
		}
	}
	return "socks5://" + ln.Addr().String(), &count, cleanup
}

func handleTestSocks5UDPAssociate(conn net.Conn) (*net.UDPConn, error) {
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return nil, err
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return nil, err
	}

	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return nil, err
	}
	if head[1] != 0x03 {
		return nil, io.ErrUnexpectedEOF
	}
	if err := discardSocksAddr(conn, head[3]); err != nil {
		return nil, err
	}
	var port [2]byte
	if _, err := io.ReadFull(conn, port[:]); err != nil {
		return nil, err
	}

	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	reply := []byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(reply[8:], uint16(udpConn.LocalAddr().(*net.UDPAddr).Port))
	if _, err := conn.Write(reply); err != nil {
		_ = udpConn.Close()
		return nil, err
	}
	return udpConn, nil
}

func discardSocksAddr(r io.Reader, atyp byte) error {
	switch atyp {
	case 0x01:
		_, err := io.CopyN(io.Discard, r, net.IPv4len)
		return err
	case 0x03:
		var size [1]byte
		if _, err := io.ReadFull(r, size[:]); err != nil {
			return err
		}
		_, err := io.CopyN(io.Discard, r, int64(size[0]))
		return err
	case 0x04:
		_, err := io.CopyN(io.Discard, r, net.IPv6len)
		return err
	default:
		return io.ErrUnexpectedEOF
	}
}
