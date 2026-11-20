package dnsproxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// Verify the CONNECT destination and DNS framing on the wire, including a
// second query over the same downstream TCP connection.
func TestTCPForwardsFramedQueriesThroughSOCKS(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	query := []byte{0, 12, 0x12, 0x34, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	reply := bytes.Clone(query)
	reply[4] = 0x81
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := ln.Accept()
			if err != nil {
				done <- err
				return
			}
			err = func() error {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				expect := func(want []byte) error {
					b := make([]byte, len(want))
					if _, err := io.ReadFull(conn, b); err != nil {
						return err
					}
					if !bytes.Equal(b, want) {
						return io.ErrUnexpectedEOF
					}
					return nil
				}
				if err := expect([]byte{5, 1, 0}); err != nil {
					return err
				}
				if _, err := conn.Write([]byte{5, 0}); err != nil {
					return err
				}
				if err := expect([]byte{5, 1, 0, 1, 192, 0, 2, 53, 0, 53}); err != nil {
					return err
				}
				if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1}); err != nil {
					return err
				}
				if err := expect(query); err != nil {
					return err
				}
				_, err := conn.Write(reply)
				return err
			}()
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	s := &Server{upstreams: []socksAddr{{host: "192.0.2.53", port: 53}}}
	go s.forwardTCP(context.Background(), server, "socks5://"+ln.Addr().String())
	client.SetDeadline(time.Now().Add(4 * time.Second))
	for i := 0; i < 2; i++ {
		if _, err := client.Write(query); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(reply))
		if _, err := io.ReadFull(client, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, reply) {
			t.Fatalf("got %x want %x", got, reply)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
