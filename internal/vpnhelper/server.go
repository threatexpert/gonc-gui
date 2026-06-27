package vpnhelper

import (
	"encoding/json"
	"fmt"
	"net"

	"gonc-gui/internal/winvpn"
)

func Run(connectAddr, token string) error {
	if connectAddr == "" || token == "" {
		return fmt.Errorf("missing helper connection parameters")
	}
	conn, err := net.Dial("tcp", connectAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(request{Op: OpHello, Token: token}); err != nil {
		return err
	}
	var helloAck response
	if err := dec.Decode(&helloAck); err != nil {
		return err
	}
	if !helloAck.OK {
		return fmt.Errorf("helper hello rejected: %s", helloAck.Error)
	}

	var active *winvpn.Session
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			if active != nil {
				_ = active.Stop()
			}
			return nil
		}
		switch req.Op {
		case OpStart:
			if active != nil {
				_ = active.Stop()
				active = nil
			}
			session, err := winvpn.Start(req.Config)
			if err != nil {
				_ = enc.Encode(response{OK: false, Error: err.Error()})
				continue
			}
			active = session
			_ = enc.Encode(response{OK: true})
		case OpStop:
			if active != nil {
				_ = active.Stop()
				active = nil
			}
			_ = enc.Encode(response{OK: true})
		case OpExit:
			if active != nil {
				_ = active.Stop()
			}
			_ = enc.Encode(response{OK: true})
			return nil
		default:
			_ = enc.Encode(response{OK: false, Error: "unknown helper command"})
		}
	}
}
