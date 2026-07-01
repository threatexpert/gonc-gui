package vpnhelper

import "gonc-gui/internal/vpnconfig"

const (
	OpHello = "hello"
	OpStart = "start"
	OpStop  = "stop"
	OpExit  = "exit"
)

type request struct {
	Op     string           `json:"op"`
	Token  string           `json:"token,omitempty"`
	Config vpnconfig.Config `json:"config,omitempty"`
}

type response struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Logs  []string `json:"logs,omitempty"`
}
