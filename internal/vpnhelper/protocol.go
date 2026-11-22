package vpnhelper

import (
	"encoding/json"
	"errors"

	"gonc-gui/internal/vpnconfig"
)

const (
	OpHello   = "hello"
	OpStart   = "start"
	OpDNS     = "dns"
	OpProxy   = "proxy"
	OpPrepare = "prepare"
	OpRefresh = "refresh"
	OpSeal    = "sealOutput"
	OpResume  = "resumeProtectedRoutes"
	OpStop    = "stop"
	OpExit    = "exit"
)

type request struct {
	Local   string           `json:"local,omitempty"`
	Address string           `json:"address,omitempty"`
	Op      string           `json:"op"`
	Token   string           `json:"token,omitempty"`
	Trace   bool             `json:"trace,omitempty"`
	Config  vpnconfig.Config `json:"config,omitempty"`
}

type response struct {
	BindAddress     string   `json:"bindAddress,omitempty"`
	RoutesSuspended bool     `json:"routesSuspended,omitempty"`
	Revision        uint64   `json:"revision,omitempty"`
	Local           string   `json:"local,omitempty"`
	Progress        bool     `json:"progress,omitempty"`
	OK              bool     `json:"ok"`
	Error           string   `json:"error,omitempty"`
	Logs            []string `json:"logs,omitempty"`
	DNSServers      []string `json:"dnsServers"`
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
