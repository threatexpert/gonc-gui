package dnsproxy

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"time"

	"golang.org/x/net/proxy"
)

func (s *Server) serveTCP() {
	defer s.wg.Done()
	slots := make(chan struct{}, 64)
	for {
		c, err := s.tcp.AcceptTCP()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			c.Close()
			continue
		}
		s.assocMu.RLock()
		ctx, endpoint := s.tcpContext, s.socks5Endpoint
		s.assocMu.RUnlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-slots }()
			defer c.Close()
			cancelRead := context.AfterFunc(ctx, func() { c.Close() })
			defer cancelRead()
			s.forwardTCP(ctx, c, endpoint)
		}()
	}
}

// DNS over TCP uses a two-byte length prefix; each query can try the next
// configured upstream without exposing a partial response to the client.
func (s *Server) forwardTCP(ctx context.Context, client net.Conn, endpoint string) {
	addr, err := parseSocksEndpoint(endpoint)
	if err != nil {
		return
	}
	socks, err := proxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: requestTimeout})
	if err != nil {
		return
	}
	dialer := socks.(proxy.ContextDialer)
	var header [2]byte
	for {
		_ = client.SetDeadline(time.Now().Add(requestTimeout))
		if _, err := io.ReadFull(client, header[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(header[:]))
		if n < 12 {
			return
		}
		query := make([]byte, 2+n)
		copy(query, header[:])
		if _, err := io.ReadFull(client, query[2:]); err != nil {
			return
		}
		var answer []byte
		for _, up := range s.upstreams {
			answer, err = exchangeTCP(ctx, dialer, net.JoinHostPort(up.host, strconv.Itoa(up.port)), query)
			if err == nil {
				break
			}
		}
		if err != nil {
			return
		}
		_ = client.SetWriteDeadline(time.Now().Add(requestTimeout))
		if _, err := client.Write(answer); err != nil {
			return
		}
	}
}
func exchangeTCP(parent context.Context, dialer proxy.ContextDialer, dest string, query []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	up, err := dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		return nil, err
	}
	defer up.Close()
	stop := context.AfterFunc(ctx, func() { up.Close() })
	defer stop()
	if _, err := up.Write(query); err != nil {
		return nil, err
	}
	var header [2]byte
	if _, err := io.ReadFull(up, header[:]); err != nil {
		return nil, err
	}
	answer := make([]byte, 2+int(binary.BigEndian.Uint16(header[:])))
	copy(answer, header[:])
	_, err = io.ReadFull(up, answer[2:])
	return answer, err
}
