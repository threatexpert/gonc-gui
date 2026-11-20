package vpnhelper

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"

	"gonc-gui/internal/vpnconfig"
	"gonc-gui/internal/winvpn"
)

func Run(connectAddr, token string) (result error) {
	if connectAddr == "" || token == "" {
		return fmt.Errorf("missing helper connection parameters")
	}
	network := "tcp"
	if runtime.GOOS == "linux" && strings.HasPrefix(connectAddr, "@") {
		network = "unix"
	}
	conn, err := net.Dial(network, connectAddr)
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

	var resources helperResources
	defer func() {
		// EOF/abnormal parent termination still attempts every cleanup and reports
		// any final failure to the helper's caller instead of silently succeeding.
		var err error
		for i := 0; i < 3; i++ {
			err = resources.cleanup()
			if err == nil {
				break
			}
		}
		result = errors.Join(result, err)
	}()
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			return nil
		}
		switch req.Op {
		case OpPrepare:
			if resources.active != nil || resources.bypass != nil {
				_ = enc.Encode(response{OK: false, Error: "VPN bypass already prepared"})
				continue
			}
			prepared, err := winvpn.PrepareBypass(req.Address, req.LocalIP)
			if prepared != nil {
				resources.bypass = prepared
			}
			if err != nil {
				_ = enc.Encode(response{OK: false, Error: err.Error()})
				continue
			}
			bound, address, err := prepareBindingService(conn, prepared)
			if err != nil {
				_ = enc.Encode(response{Error: err.Error()})
				continue
			}
			resources.bypass = bound
			_ = enc.Encode(response{OK: true, LocalIP: prepared.LocalIP, BindAddress: address})
		case OpRefresh:
			if resources.bypass == nil {
				_ = enc.Encode(response{Error: "VPN bypass is not prepared"})
				continue
			}
			local, revision, err := resources.bypass.Refresh()
			if err != nil {
				_ = enc.Encode(response{Error: err.Error()})
				continue
			}
			_ = enc.Encode(response{OK: true, LocalIP: local, Revision: revision})
		case OpProxy:
			if resources.active == nil {
				_ = enc.Encode(response{OK: false, Error: "VPN is not running"})
				continue
			}
			if err := resources.active.UpdateProxy(req.Address); err != nil {
				_ = enc.Encode(response{OK: false, Error: err.Error()})
				continue
			}
			_ = enc.Encode(response{OK: true})
		case OpDNS:
			if resources.active == nil {
				_ = enc.Encode(response{OK: false, Error: "VPN is not running"})
				continue
			}
			if err := resources.active.UpdateDNS(req.Config.DNSServers); err != nil {
				_ = enc.Encode(response{OK: false, Error: err.Error()})
				continue
			}
			_ = enc.Encode(response{OK: true, DNSServers: resources.active.DNSServers()})
		case OpStart:
			if resources.active != nil {
				if err := resources.active.Stop(); err != nil {
					_ = enc.Encode(response{Error: err.Error()})
					continue
				}
				resources.active = nil
			}
			var trace func(string)
			if req.Trace {
				trace = func(message string) {
					_ = enc.Encode(response{Progress: true, Logs: []string{message}})
				}
			}
			if b, ok := resources.bypass.(interface{ configureDNSBypass(*vpnconfig.Config) error }); ok && req.Config.BlockDNSLeak {
				if err := b.configureDNSBypass(&req.Config); err != nil {
					_ = enc.Encode(response{Error: err.Error()})
					continue
				}
			}
			session, err := winvpn.StartWithTrace(req.Config, trace)
			if session != nil {
				resources.active = session
			}
			if err != nil {
				_ = enc.Encode(response{OK: false, Error: err.Error()})
				continue
			}
			resources.active = session
			_ = enc.Encode(response{OK: true, DNSServers: session.DNSServers()})
		case OpStop, OpExit:
			if err := resources.cleanup(); err != nil {
				_ = enc.Encode(response{Error: err.Error()})
				continue // Keep IPC and failed resources alive so the user can retry.
			}
			_ = enc.Encode(response{OK: true})
			if req.Op == OpExit {
				return nil
			}
		default:
			_ = enc.Encode(response{OK: false, Error: "unknown helper command"})
		}
	}
}
