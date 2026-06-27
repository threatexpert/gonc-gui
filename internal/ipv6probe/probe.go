package ipv6probe

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const DefaultTimeout = 5 * time.Second

type Result struct {
	Available bool
	Detail    string
}

type Probe struct {
	Host string
	Port int
	Kind string
}

var defaultProbes = []Probe{
	{Host: "2001:4860:4860::8888", Port: 53, Kind: "DNS"},
	{Host: "2620:fe::fe", Port: 53, Kind: "DNS"},
	{Host: "2400:3200::1", Port: 53, Kind: "DNS"},
	{Host: "2400:3200:baba::1", Port: 443, Kind: "TLS"},
}

func Check(ctx context.Context, socks5Endpoint string, timeout time.Duration) Result {
	proxy, err := parseSOCKS5Endpoint(socks5Endpoint)
	if err != nil {
		return Result{Detail: "invalid SOCKS5 endpoint: " + err.Error()}
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results := make(chan Result, len(defaultProbes))
	var wg sync.WaitGroup
	for _, probe := range defaultProbes {
		probe := probe
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runProbe(ctx, proxy, probe); err != nil {
				results <- Result{Detail: probe.Label() + ": " + err.Error()}
				return
			}
			results <- Result{Available: true, Detail: probe.Label()}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var failures []string
	for {
		select {
		case <-ctx.Done():
			if len(failures) == 0 {
				return Result{Detail: "timeout"}
			}
			return Result{Detail: joinFailures(failures)}
		case result, ok := <-results:
			if !ok {
				if len(failures) == 0 {
					return Result{Detail: "timeout"}
				}
				return Result{Detail: joinFailures(failures)}
			}
			if result.Available {
				return result
			}
			failures = append(failures, result.Detail)
		}
	}
}

func (p Probe) Label() string {
	return fmt.Sprintf("[%s]:%d %s", p.Host, p.Port, p.Kind)
}

type socks5Endpoint struct {
	Host string
	Port string
}

func parseSOCKS5Endpoint(endpoint string) (socks5Endpoint, error) {
	clean := strings.TrimSpace(endpoint)
	if strings.HasPrefix(clean, "socks5://") {
		u, err := url.Parse(clean)
		if err != nil {
			return socks5Endpoint{}, err
		}
		if u.Hostname() == "" || u.Port() == "" {
			return socks5Endpoint{}, errors.New("missing host or port")
		}
		return socks5Endpoint{Host: u.Hostname(), Port: u.Port()}, nil
	}
	host, port, err := net.SplitHostPort(clean)
	if err != nil {
		return socks5Endpoint{}, err
	}
	if host == "" || port == "" {
		return socks5Endpoint{}, errors.New("missing host or port")
	}
	return socks5Endpoint{Host: host, Port: port}, nil
}

func runProbe(ctx context.Context, proxy socks5Endpoint, probe Probe) error {
	conn, err := connectSOCKS5(ctx, proxy, probe.Host, probe.Port)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	switch probe.Kind {
	case "DNS":
		return runDNSProbe(conn)
	case "TLS":
		return runTLSProbe(conn, probe.Host)
	default:
		return errors.New("unknown probe kind")
	}
}

func connectSOCKS5(ctx context.Context, proxy socks5Endpoint, targetHost string, targetPort int) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(proxy.Host, proxy.Port))
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if header[0] != 0x05 || header[1] != 0x00 {
		_ = conn.Close()
		return nil, errors.New("SOCKS5 auth rejected")
	}
	ip := net.ParseIP(targetHost).To16()
	if ip == nil || ip.To4() != nil {
		_ = conn.Close()
		return nil, errors.New("target is not IPv6")
	}
	request := make([]byte, 4+16+2)
	request[0] = 0x05
	request[1] = 0x01
	request[2] = 0x00
	request[3] = 0x04
	copy(request[4:20], ip)
	binary.BigEndian.PutUint16(request[20:22], uint16(targetPort))
	if _, err := conn.Write(request); err != nil {
		_ = conn.Close()
		return nil, err
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(conn, response); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if response[0] != 0x05 || response[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect failed: 0x%02x", response[1])
	}
	var skip int
	switch response[3] {
	case 0x01:
		skip = 4 + 2
	case 0x04:
		skip = 16 + 2
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(conn, length); err != nil {
			_ = conn.Close()
			return nil, err
		}
		skip = int(length[0]) + 2
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("SOCKS5 invalid address type %d", response[3])
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skip)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func runDNSProbe(conn net.Conn) error {
	query := dnsQuery("example.com")
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(query)))
	if _, err := conn.Write(size[:]); err != nil {
		return err
	}
	if _, err := conn.Write(query); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return err
	}
	length := int(binary.BigEndian.Uint16(size[:]))
	if length < 12 || length > 4096 {
		return fmt.Errorf("invalid DNS response length %d", length)
	}
	response := make([]byte, length)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != query[0] || response[1] != query[1] {
		return errors.New("DNS response id mismatch")
	}
	if response[2]&0x80 == 0 {
		return errors.New("DNS response QR bit missing")
	}
	return nil
}

func runTLSProbe(conn net.Conn, host string) error {
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
	})
	defer tlsConn.Close()
	return tlsConn.Handshake()
}

func dnsQuery(domain string) []byte {
	qname := dnsQName(domain)
	query := make([]byte, 12+len(qname)+4)
	var id [2]byte
	if _, err := rand.Read(id[:]); err != nil {
		n := uint16(time.Now().UnixNano())
		binary.BigEndian.PutUint16(id[:], n)
	}
	copy(query[0:2], id[:])
	query[2] = 0x01
	query[5] = 0x01
	copy(query[12:], qname)
	offset := 12 + len(qname)
	query[offset+1] = 0x01
	query[offset+3] = 0x01
	return query
}

func dnsQName(domain string) []byte {
	labels := strings.Split(domain, ".")
	size := 1
	for _, label := range labels {
		size += 1 + len(label)
	}
	result := make([]byte, size)
	offset := 0
	for _, label := range labels {
		result[offset] = byte(len(label))
		offset++
		copy(result[offset:], label)
		offset += len(label)
	}
	return result
}

func joinFailures(failures []string) string {
	limit := len(failures)
	if limit > 2 {
		limit = 2
	}
	result := strings.Join(failures[:limit], "; ")
	if len(failures) > limit {
		result += "; ..."
	}
	return result
}
