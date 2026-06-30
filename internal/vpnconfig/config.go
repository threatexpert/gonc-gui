package vpnconfig

type Config struct {
	SOCKS5Endpoint string   `json:"socks5Endpoint"`
	Routes         []string `json:"routes"`
	DNSServers     []string `json:"dnsServers"`
	BypassIPs      []string `json:"bypassIps"`
	EnableIPv6     bool     `json:"enableIPv6"`
	MTU            int      `json:"mtu"`
	RouteMetric    int      `json:"routeMetric"`
	BlockDNSLeak   bool     `json:"blockDnsLeak"`
	LogLevel       string   `json:"logLevel"`
}
