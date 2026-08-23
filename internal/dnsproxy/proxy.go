package dnsproxy

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultDNSPort          = 53
	udpBufferSize           = 64 * 1024
	associationTimeout      = 8 * time.Second
	listenRetryTimeout      = 5 * time.Second
	listenRetryInterval     = 100 * time.Millisecond
	requestTimeout          = 8 * time.Second
	multiRequestTimeout     = 10 * time.Second
	pendingSweepInterval    = 3 * time.Second
	primaryResponseWindow   = 2 * time.Second
	primaryFailureThreshold = 3
	primaryRetryInterval    = 30 * time.Second
)

type Server struct {
	udp            *net.UDPConn
	socks5Endpoint string
	upstreams      []socksAddr

	done chan struct{}
	wg   sync.WaitGroup
	once sync.Once

	assocMu       sync.RWMutex
	assoc         *udpAssociation
	reconnect     sync.Mutex
	mu            sync.Mutex
	pending       map[uint16]pendingRequest
	activeUp      int
	probeAt       time.Time
	primaryMisses int
}

type pendingRequest struct {
	group    *pendingGroup
	upstream int
}

type pendingGroup struct {
	client        *net.UDPAddr
	id            uint16
	started       time.Time
	expire        time.Time
	firstUpstream int
	proxyIDs      []uint16
	completed     bool
}

type upstreamAttempt struct {
	index int
	delay time.Duration
}

type socksAddr struct {
	host string
	ip   net.IP
	port int
}

func Start(bindIP string, socks5Endpoint string, upstreams []string) (*Server, error) {
	return StartWithTrace(bindIP, socks5Endpoint, upstreams, nil)
}

func StartWithTrace(bindIP string, socks5Endpoint string, upstreams []string, trace func(string)) (*Server, error) {
	total := time.Now()
	defer func() {
		traceDNSProxyStep(trace, "total", total)
	}()
	step := time.Now()
	servers, err := parseUpstreams(upstreams)
	if err != nil {
		return nil, err
	}
	traceDNSProxyStep(trace, "parse upstreams", step)
	bind := net.JoinHostPort(strings.TrimSpace(bindIP), strconv.Itoa(defaultDNSPort))
	step = time.Now()
	udp, err := listenDNSUDPWithTrace(bind, trace)
	if err != nil {
		return nil, err
	}
	traceDNSProxyStep(trace, "listen DNS UDP", step)
	server := &Server{
		udp:            udp,
		socks5Endpoint: socks5Endpoint,
		upstreams:      servers,
		done:           make(chan struct{}),
		pending:        map[uint16]pendingRequest{},
	}
	step = time.Now()
	assoc, err := newUDPAssociationWithTrace(socks5Endpoint, trace)
	if err != nil {
		udp.Close()
		return nil, err
	}
	traceDNSProxyStep(trace, "create SOCKS5 UDP association", step)
	server.setAssociation(assoc)
	step = time.Now()
	server.wg.Add(4)
	go server.serveClients()
	go server.serveResponses()
	go server.monitorAssociation()
	go server.sweepPending()
	traceDNSProxyStep(trace, "start workers", step)
	return server, nil
}

func traceDNSProxyStep(trace func(string), name string, started time.Time) {
	if trace == nil {
		return
	}
	trace(fmt.Sprintf("DNS proxy step: %s completed in %s", name, time.Since(started).Round(time.Millisecond)))
}

func listenDNSUDP(bind string) (*net.UDPConn, error) {
	return listenDNSUDPWithTrace(bind, nil)
}

func listenDNSUDPWithTrace(bind string, trace func(string)) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", bind)
	if err != nil {
		return nil, fmt.Errorf("resolve DNS proxy bind address %s: %w", bind, err)
	}
	network := "udp"
	if addr.IP != nil && addr.IP.To4() != nil {
		network = "udp4"
	}
	deadline := time.Now().Add(listenRetryTimeout)
	var lastErr error
	retries := 0
	for {
		udp, err := net.ListenUDP(network, addr)
		if err == nil {
			if retries > 0 {
				traceDNSProxyMessage(trace, fmt.Sprintf("DNS proxy step: listen DNS UDP succeeded after %d retries; last error: %v", retries, lastErr))
			}
			return udp, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		retries++
		time.Sleep(listenRetryInterval)
	}
	return nil, fmt.Errorf("listen DNS proxy on %s using %s: %w", bind, network, lastErr)
}

func traceDNSProxyMessage(trace func(string), message string) {
	if trace == nil {
		return
	}
	trace(message)
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.once.Do(func() {
		close(s.done)
		if s.udp != nil {
			err = errors.Join(err, s.udp.Close())
		}
		if assoc := s.currentAssociation(); assoc != nil {
			err = errors.Join(err, assoc.Close())
		}
		s.wg.Wait()
	})
	return err
}

func (s *Server) serveClients() {
	defer s.wg.Done()
	buf := make([]byte, udpBufferSize)
	for {
		n, client, err := s.udp.ReadFromUDP(buf)
		if err != nil {
			if s.isDone() {
				return
			}
			continue
		}
		if n < 2 || client == nil {
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		if len(s.upstreams) == 1 {
			s.forwardSingle(client, packet)
			continue
		}
		go s.forwardWithFallback(client, packet)
	}
}

func (s *Server) serveResponses() {
	defer s.wg.Done()
	buf := make([]byte, udpBufferSize)
	for {
		assoc := s.ensureAssociation()
		if assoc == nil {
			if s.sleepOrDone(100 * time.Millisecond) {
				return
			}
			continue
		}
		n, err := assoc.Read(buf)
		if err != nil {
			if s.isDone() {
				return
			}
			s.rebuildAssociation(assoc)
			continue
		}
		payload, err := decodeSocksUDP(buf[:n])
		if err != nil || len(payload) < 2 {
			continue
		}
		proxyID := binary.BigEndian.Uint16(payload[:2])
		req, ok := s.take(proxyID, time.Now())
		if !ok {
			continue
		}
		response := append([]byte(nil), payload...)
		binary.BigEndian.PutUint16(response[:2], req.id)
		_, _ = s.udp.WriteToUDP(response, req.client)
	}
}

func (s *Server) monitorAssociation() {
	defer s.wg.Done()
	for {
		assoc := s.ensureAssociation()
		if assoc == nil {
			if s.sleepOrDone(100 * time.Millisecond) {
				return
			}
			continue
		}
		select {
		case <-s.done:
			return
		case <-assoc.Done():
			s.rebuildAssociation(assoc)
		}
	}
}

func (s *Server) sweepPending() {
	defer s.wg.Done()
	ticker := time.NewTicker(pendingSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			expired := false
			s.mu.Lock()
			for _, req := range s.pending {
				group := req.group
				if group != nil && !group.completed && now.After(group.expire) {
					group.completed = true
					for _, id := range group.proxyIDs {
						delete(s.pending, id)
					}
					expired = true
				}
			}
			s.mu.Unlock()
			if expired {
				go s.rebuildAssociation(s.currentAssociation())
			}
		}
	}
}

func (s *Server) currentAssociation() *udpAssociation {
	s.assocMu.RLock()
	defer s.assocMu.RUnlock()
	return s.assoc
}

func (s *Server) setAssociation(assoc *udpAssociation) {
	s.assocMu.Lock()
	s.assoc = assoc
	s.assocMu.Unlock()
}

func (s *Server) ensureAssociation() *udpAssociation {
	if assoc := s.currentAssociation(); assoc != nil {
		return assoc
	}
	if !s.rebuildAssociation(nil) {
		return nil
	}
	return s.currentAssociation()
}

func (s *Server) rebuildAssociation(trigger *udpAssociation) bool {
	if s.isDone() {
		return false
	}
	s.reconnect.Lock()
	defer s.reconnect.Unlock()
	if s.isDone() {
		return false
	}
	current := s.currentAssociation()
	if trigger != nil && current != trigger {
		return true
	}
	if trigger == nil && current != nil {
		return true
	}
	if current != nil {
		_ = current.Close()
		s.setAssociation(nil)
	}
	s.clearPending()
	next, err := newUDPAssociation(s.socks5Endpoint)
	if err != nil {
		return false
	}
	if s.isDone() {
		_ = next.Close()
		return false
	}
	s.setAssociation(next)
	return true
}

func (s *Server) clearPending() {
	s.mu.Lock()
	for id, req := range s.pending {
		if req.group != nil {
			req.group.completed = true
		}
		delete(s.pending, id)
	}
	s.mu.Unlock()
}

func (s *Server) forwardSingle(client *net.UDPAddr, packet []byte) {
	assoc := s.ensureAssociation()
	if assoc == nil {
		return
	}
	now := time.Now()
	group := s.newPendingGroup(client, binary.BigEndian.Uint16(packet[:2]), 0, now, requestTimeout)
	s.sendAttempt(assoc, group, 0, packet)
}

func (s *Server) forwardWithFallback(client *net.UDPAddr, packet []byte) {
	started := time.Now()
	attempts := s.upstreamPlan(started)
	if len(attempts) == 0 {
		return
	}
	group := s.newPendingGroup(client, binary.BigEndian.Uint16(packet[:2]), attempts[0].index, started, multiRequestTimeout)
	for _, attempt := range attempts {
		if wait := time.Until(started.Add(attempt.delay)); wait > 0 {
			if s.sleepOrDone(wait) {
				return
			}
		}
		if !s.groupOpen(group) {
			return
		}
		assoc := s.ensureAssociation()
		if assoc == nil {
			continue
		}
		s.sendAttempt(assoc, group, attempt.index, packet)
	}
}

func (s *Server) sendAttempt(assoc *udpAssociation, group *pendingGroup, upstreamIndex int, packet []byte) {
	proxyID, ok := s.remember(group, upstreamIndex)
	if !ok {
		return
	}
	attempt := append([]byte(nil), packet...)
	binary.BigEndian.PutUint16(attempt[:2], proxyID)
	if err := assoc.WriteTo(s.upstreams[upstreamIndex], attempt); err != nil {
		s.forget(proxyID)
		s.rebuildAssociation(assoc)
	}
}

func (s *Server) upstreamPlan(now time.Time) []upstreamAttempt {
	total := len(s.upstreams)
	if total == 0 {
		return nil
	}
	first := 0
	if total > 1 {
		s.mu.Lock()
		if s.activeUp > 0 && now.Before(s.probeAt) {
			first = s.activeUp
		}
		s.mu.Unlock()
	}
	if total == 1 {
		return []upstreamAttempt{{index: first}}
	}
	second := (first + 1) % total
	third := (first + 2) % total
	attempts := []upstreamAttempt{{index: first}}
	if second != first {
		attempts = append(attempts, upstreamAttempt{index: second, delay: 2 * time.Second})
	}
	if total > 2 && third != first && third != second {
		attempts = append(attempts, upstreamAttempt{index: third, delay: 4 * time.Second})
	}
	for _, delay := range []time.Duration{6 * time.Second, 9 * time.Second} {
		for index := range s.upstreams {
			attempts = append(attempts, upstreamAttempt{index: index, delay: delay})
		}
	}
	return attempts
}

func (s *Server) newPendingGroup(client *net.UDPAddr, originalID uint16, firstUpstream int, started time.Time, timeout time.Duration) *pendingGroup {
	return &pendingGroup{
		client:        client,
		id:            originalID,
		started:       started,
		expire:        started.Add(timeout),
		firstUpstream: firstUpstream,
	}
}

func (s *Server) groupOpen(group *pendingGroup) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !group.completed
}

func (s *Server) remember(group *pendingGroup, upstreamIndex int) (uint16, bool) {
	for i := 0; i < 32; i++ {
		id, err := randomUint16()
		if err != nil {
			return 0, false
		}
		s.mu.Lock()
		if group.completed {
			s.mu.Unlock()
			return 0, false
		}
		if _, exists := s.pending[id]; !exists {
			s.pending[id] = pendingRequest{
				group:    group,
				upstream: upstreamIndex,
			}
			group.proxyIDs = append(group.proxyIDs, id)
			s.mu.Unlock()
			return id, true
		}
		s.mu.Unlock()
	}
	return 0, false
}

func (s *Server) take(id uint16, now time.Time) (pendingGroup, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.pending[id]
	if !ok || req.group == nil || req.group.completed {
		return pendingGroup{}, false
	}
	group := req.group
	group.completed = true
	for _, proxyID := range group.proxyIDs {
		delete(s.pending, proxyID)
	}
	if len(s.upstreams) > 1 {
		s.noteUpstreamResponseLocked(group.firstUpstream, req.upstream, group.started, now)
	}
	return *group, true
}

func (s *Server) noteUpstreamResponseLocked(first int, winner int, started time.Time, now time.Time) {
	if first == 0 {
		primaryOnTime := !now.After(started.Add(primaryResponseWindow))
		if winner == 0 && primaryOnTime {
			s.activeUp = 0
			s.probeAt = time.Time{}
			s.primaryMisses = 0
			return
		}
		s.primaryMisses++
		if winner != 0 && s.primaryMisses >= primaryFailureThreshold {
			s.activeUp = winner
			s.probeAt = now.Add(primaryRetryInterval)
			return
		}
		if s.activeUp > 0 {
			s.probeAt = now.Add(primaryRetryInterval)
		}
		return
	}
	if winner == 0 {
		s.activeUp = 0
		s.probeAt = time.Time{}
		s.primaryMisses = 0
		return
	}
	if winner != first {
		s.activeUp = winner
		s.probeAt = now.Add(primaryRetryInterval)
	}
}

func (s *Server) noteUpstreamResponse(first int, winner int, started time.Time, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteUpstreamResponseLocked(first, winner, started, now)
}

func (s *Server) forget(id uint16) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *Server) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Server) sleepOrDone(duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-s.done:
		return true
	case <-timer.C:
		return false
	}
}

type udpAssociation struct {
	tcp      net.Conn
	udp      *net.UDPConn
	relay    *net.UDPAddr
	done     chan struct{}
	closeOne sync.Once
}

func newUDPAssociation(endpoint string) (*udpAssociation, error) {
	return newUDPAssociationWithTrace(endpoint, nil)
}

func newUDPAssociationWithTrace(endpoint string, trace func(string)) (*udpAssociation, error) {
	step := time.Now()
	addr, err := parseSocksEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	traceDNSProxyStep(trace, "parse SOCKS5 endpoint", step)
	ctx, cancel := context.WithTimeout(context.Background(), associationTimeout)
	defer cancel()
	var dialer net.Dialer
	step = time.Now()
	tcp, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect SOCKS5 endpoint %s: %w", addr, err)
	}
	traceDNSProxyStep(trace, "connect SOCKS5 TCP", step)
	step = time.Now()
	assoc, err := handshakeUDPAssociate(tcp, addr)
	if err != nil {
		tcp.Close()
		return nil, err
	}
	traceDNSProxyStep(trace, "SOCKS5 UDP associate handshake", step)
	step = time.Now()
	udp, err := net.ListenUDP("udp", nil)
	if err != nil {
		tcp.Close()
		return nil, fmt.Errorf("open SOCKS5 UDP socket: %w", err)
	}
	traceDNSProxyStep(trace, "open SOCKS5 UDP socket", step)
	association := &udpAssociation{
		tcp:   tcp,
		udp:   udp,
		relay: assoc,
		done:  make(chan struct{}),
	}
	go association.monitorControl()
	return association, nil
}

func (a *udpAssociation) Close() error {
	if a == nil {
		return nil
	}
	var err error
	a.closeOne.Do(func() {
		if a.tcp != nil {
			err = errors.Join(err, a.tcp.Close())
		}
		if a.udp != nil {
			err = errors.Join(err, a.udp.Close())
		}
		close(a.done)
	})
	return err
}

func (a *udpAssociation) Done() <-chan struct{} {
	return a.done
}

func (a *udpAssociation) monitorControl() {
	var buf [1]byte
	_, _ = a.tcp.Read(buf[:])
	_ = a.Close()
}

func (a *udpAssociation) WriteTo(dst socksAddr, payload []byte) error {
	packet, err := encodeSocksUDP(dst, payload)
	if err != nil {
		return err
	}
	_, err = a.udp.WriteToUDP(packet, a.relay)
	return err
}

func (a *udpAssociation) Read(buf []byte) (int, error) {
	n, _, err := a.udp.ReadFromUDP(buf)
	return n, err
}

func parseSocksEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("SOCKS5 endpoint is required")
	}
	u, err := url.Parse(endpoint)
	if err == nil && u.Scheme != "" {
		if u.Scheme != "socks5" {
			return "", fmt.Errorf("unsupported DNS proxy endpoint scheme %q", u.Scheme)
		}
		if u.Host == "" {
			return "", fmt.Errorf("SOCKS5 endpoint host is required")
		}
		return u.Host, nil
	}
	if _, _, err := net.SplitHostPort(endpoint); err != nil {
		return "", fmt.Errorf("invalid SOCKS5 endpoint %q: %w", endpoint, err)
	}
	return endpoint, nil
}

func handshakeUDPAssociate(conn net.Conn, socksAddr string) (*net.UDPAddr, error) {
	if err := conn.SetDeadline(time.Now().Add(associationTimeout)); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return nil, fmt.Errorf("write SOCKS5 greeting: %w", err)
	}
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return nil, fmt.Errorf("read SOCKS5 greeting: %w", err)
	}
	if greeting[0] != 0x05 || greeting[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 endpoint rejected no-auth method")
	}
	if _, err := conn.Write([]byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, fmt.Errorf("write SOCKS5 UDP associate request: %w", err)
	}
	relay, err := readSocksReply(conn, socksAddr)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return relay, nil
}

func readSocksReply(conn net.Conn, socksAddr string) (*net.UDPAddr, error) {
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return nil, fmt.Errorf("read SOCKS5 reply: %w", err)
	}
	if head[0] != 0x05 {
		return nil, fmt.Errorf("invalid SOCKS5 reply version %d", head[0])
	}
	if head[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 UDP associate failed with reply code %d", head[1])
	}
	host, err := readSocksAddress(conn, head[3])
	if err != nil {
		return nil, err
	}
	var portBytes [2]byte
	if _, err := io.ReadFull(conn, portBytes[:]); err != nil {
		return nil, fmt.Errorf("read SOCKS5 UDP relay port: %w", err)
	}
	port := int(binary.BigEndian.Uint16(portBytes[:]))
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() {
		socksHost, _, err := net.SplitHostPort(socksAddr)
		if err != nil {
			return nil, err
		}
		ips, err := net.LookupIP(socksHost)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("resolve SOCKS5 UDP relay host %s: %w", socksHost, err)
		}
		ip = ips[0]
	}
	return &net.UDPAddr{IP: ip, Port: port}, nil
}

func readSocksAddress(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case 0x01:
		var ip [4]byte
		if _, err := io.ReadFull(r, ip[:]); err != nil {
			return "", fmt.Errorf("read SOCKS5 IPv4 address: %w", err)
		}
		return net.IP(ip[:]).String(), nil
	case 0x03:
		var size [1]byte
		if _, err := io.ReadFull(r, size[:]); err != nil {
			return "", fmt.Errorf("read SOCKS5 domain length: %w", err)
		}
		name := make([]byte, int(size[0]))
		if _, err := io.ReadFull(r, name); err != nil {
			return "", fmt.Errorf("read SOCKS5 domain: %w", err)
		}
		return string(name), nil
	case 0x04:
		var ip [16]byte
		if _, err := io.ReadFull(r, ip[:]); err != nil {
			return "", fmt.Errorf("read SOCKS5 IPv6 address: %w", err)
		}
		return net.IP(ip[:]).String(), nil
	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type %d", atyp)
	}
}

func encodeSocksUDP(dst socksAddr, payload []byte) ([]byte, error) {
	packet := make([]byte, 0, len(payload)+32)
	packet = append(packet, 0, 0, 0)
	switch {
	case dst.ip != nil && dst.ip.To4() != nil:
		packet = append(packet, 0x01)
		packet = append(packet, dst.ip.To4()...)
	case dst.ip != nil && dst.ip.To16() != nil:
		packet = append(packet, 0x04)
		packet = append(packet, dst.ip.To16()...)
	case dst.host != "":
		if len(dst.host) > 255 {
			return nil, fmt.Errorf("SOCKS5 domain is too long: %s", dst.host)
		}
		packet = append(packet, 0x03, byte(len(dst.host)))
		packet = append(packet, dst.host...)
	default:
		return nil, fmt.Errorf("invalid upstream DNS address")
	}
	packet = binary.BigEndian.AppendUint16(packet, uint16(dst.port))
	packet = append(packet, payload...)
	return packet, nil
}

func decodeSocksUDP(packet []byte) ([]byte, error) {
	if len(packet) < 4 {
		return nil, fmt.Errorf("short SOCKS5 UDP packet")
	}
	if packet[0] != 0 || packet[1] != 0 || packet[2] != 0 {
		return nil, fmt.Errorf("unsupported SOCKS5 UDP header")
	}
	offset := 4
	switch packet[3] {
	case 0x01:
		offset += net.IPv4len
	case 0x03:
		if len(packet) < offset+1 {
			return nil, fmt.Errorf("short SOCKS5 UDP domain")
		}
		offset += 1 + int(packet[offset])
	case 0x04:
		offset += net.IPv6len
	default:
		return nil, fmt.Errorf("unsupported SOCKS5 UDP address type %d", packet[3])
	}
	offset += 2
	if len(packet) < offset {
		return nil, fmt.Errorf("short SOCKS5 UDP payload")
	}
	return packet[offset:], nil
}

func parseUpstreams(values []string) ([]socksAddr, error) {
	var upstreams []socksAddr
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		upstream, err := parseUpstream(value)
		if err != nil {
			return nil, err
		}
		upstreams = append(upstreams, upstream)
	}
	if len(upstreams) == 0 {
		upstream, _ := parseUpstream("8.8.8.8")
		upstreams = append(upstreams, upstream)
	}
	return upstreams, nil
}

func parseUpstream(value string) (socksAddr, error) {
	host := value
	port := defaultDNSPort
	if strings.HasPrefix(value, "[") || strings.Count(value, ":") == 1 {
		if h, p, err := net.SplitHostPort(value); err == nil {
			host = h
			parsedPort, err := strconv.Atoi(p)
			if err != nil || parsedPort <= 0 || parsedPort > 65535 {
				return socksAddr{}, fmt.Errorf("invalid DNS upstream port %q", p)
			}
			port = parsedPort
		}
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip != nil {
		return socksAddr{host: host, ip: ip, port: port}, nil
	}
	if host == "" {
		return socksAddr{}, fmt.Errorf("invalid DNS upstream %q", value)
	}
	return socksAddr{host: host, port: port}, nil
}

func randomUint16() (uint16, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<16))
	if err != nil {
		return 0, err
	}
	return uint16(n.Int64()), nil
}
