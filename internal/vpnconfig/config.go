package vpnconfig

type Config struct {
	SOCKS5Endpoint string   `json:"socks5Endpoint"`
	Routes         []string `json:"routes"`
	DNSServers     []string `json:"dnsServers"`
	EnableIPv6     bool     `json:"enableIPv6"`
	MTU            int      `json:"mtu"`
	LogLevel       string   `json:"logLevel"`
}
