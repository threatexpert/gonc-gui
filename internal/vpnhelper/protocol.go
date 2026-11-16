package vpnhelper

import (
	"encoding/json"
	"errors"

	"gonc-gui/internal/vpnconfig"
)

const (
	OpHello = "hello"
	OpStart = "start"
	OpDNS   = "dns"
	OpStop  = "stop"
	OpExit  = "exit"
)

type request struct {
	Op     string           `json:"op"`
	Token  string           `json:"token,omitempty"`
	Trace  bool             `json:"trace,omitempty"`
	Config vpnconfig.Config `json:"config,omitempty"`
}

type response struct {
	Progress   bool     `json:"progress,omitempty"`
	OK         bool     `json:"ok"`
	Error      string   `json:"error,omitempty"`
	Logs       []string `json:"logs,omitempty"`
	DNSServers []string `json:"dnsServers"`
}

// Progress frames are not command completion: keep reading until the final
// response, including when the caller does not consume trace messages.
func readResponse(dec *json.Decoder, trace func(string)) (response, error) {
	for {
		var resp response
		if err := dec.Decode(&resp); err != nil {
			return response{}, err
		}
		if trace != nil {
			for _, message := range resp.Logs {
				trace(message)
			}
		}
		if resp.Progress {
			continue
		}
		if !resp.OK {
			if resp.Error == "" {
				resp.Error = "VPN helper command failed"
			}
			return resp, errors.New(resp.Error)
		}
		return resp, nil
	}
}
