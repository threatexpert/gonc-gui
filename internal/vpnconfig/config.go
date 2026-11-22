package vpnconfig

type Config struct {
	// Set by the Linux helper after it authenticates the socket-binding channel.
	LinuxEgressMark      uint32   `json:"-"`
	LinuxDNSBypass       string   `json:"-"`
	DisconnectProtection bool     `json:"disconnectProtection"`
	SOCKS5Endpoint       string   `json:"socks5Endpoint"`
	Routes               []string `json:"routes"`
	DNSServers           []string `json:"dnsServers"`
	BypassIPs            []string `json:"bypassIps"`
	EnableIPv6           bool     `json:"enableIPv6"` // Derived from Routes, never from exit reachability.
	RejectIPv6           bool     `json:"rejectIPv6"` // Reject captured TCP/UDP locally when the exit probe fails.
	MTU                  int      `json:"mtu"`
	RouteMetric          int      `json:"routeMetric"`
	BlockDNSLeak         bool     `json:"blockDnsLeak"`
	LogLevel             string   `json:"logLevel"`
}
