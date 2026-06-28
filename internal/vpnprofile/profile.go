package vpnprofile

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultName   = "Default"
	defaultDNS    = "8.8.8.8\n2001:4860:4860::8888"
	defaultRoutes = "0.0.0.0/1\n128.0.0.0/1\n::/0"
)

type Profile struct {
	Name       string `json:"name"`
	Passphrase string `json:"passphrase"`
	UseUDP     bool   `json:"useUdp"`
	RouteIPv6  bool   `json:"routeIpv6"`
	DNSServers string `json:"dnsServers"`
	RouteCIDRs string `json:"routeCidrs"`
	LinkConfig string `json:"linkConfig"`
	ExtraArgs  string `json:"extraArgs"`
	TunnelOnly bool   `json:"tunnelOnly"`
}

type Store struct {
	Version  int       `json:"version"`
	Selected int       `json:"selected"`
	Profiles []Profile `json:"profiles"`
}

func DefaultStore() Store {
	return Store{
		Version:  1,
		Selected: 0,
		Profiles: []Profile{DefaultProfile(defaultName)},
	}
}

func DefaultProfile(name string) Profile {
	if strings.TrimSpace(name) == "" {
		name = defaultName
	}
	return Profile{
		Name:       name,
		DNSServers: defaultDNS,
		RouteCIDRs: defaultRoutes,
	}
}

func Load() (Store, error) {
	path, err := filePath()
	if err != nil {
		return DefaultStore(), err
	}
	encrypted, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultStore(), nil
	}
	if err != nil {
		return DefaultStore(), err
	}
	data, err := unprotect(encrypted)
	if err != nil {
		return DefaultStore(), err
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return DefaultStore(), err
	}
	normalize(&store)
	return store, nil
}

func Save(store Store) error {
	normalize(&store)
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	encrypted, err := protect(data)
	if err != nil {
		return err
	}
	path, err := filePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0o600)
}

func filePath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Gonc", "gonc-gui", "vpn-profiles.json.enc"), nil
}

func userConfigDir() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}
	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if sudoUser == "" || sudoUser == "root" {
		return os.UserConfigDir()
	}
	u, err := user.Lookup(sudoUser)
	if err != nil || strings.TrimSpace(u.HomeDir) == "" {
		return os.UserConfigDir()
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(u.HomeDir, "Library", "Application Support"), nil
	}
	if configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configHome != "" && !strings.HasPrefix(configHome, "/root/") {
		return configHome, nil
	}
	return filepath.Join(u.HomeDir, ".config"), nil
}

func normalize(store *Store) {
	if store.Version <= 0 {
		store.Version = 1
	}
	if len(store.Profiles) == 0 {
		store.Profiles = []Profile{DefaultProfile(defaultName)}
	}
	for i := range store.Profiles {
		profile := &store.Profiles[i]
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			profile.Name = defaultName
		}
		profile.Passphrase = strings.TrimSpace(profile.Passphrase)
		profile.LinkConfig = strings.TrimSpace(profile.LinkConfig)
		profile.ExtraArgs = strings.TrimSpace(profile.ExtraArgs)
		profile.DNSServers = normalizeLines(profile.DNSServers)
		if profile.DNSServers == "" {
			profile.DNSServers = defaultDNS
		}
		profile.RouteCIDRs = normalizeLines(profile.RouteCIDRs)
		if profile.RouteCIDRs == "" {
			profile.RouteCIDRs = defaultRoutes
		}
	}
	if store.Selected < 0 {
		store.Selected = 0
	}
	if store.Selected >= len(store.Profiles) {
		store.Selected = len(store.Profiles) - 1
	}
}

func normalizeLines(value string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
