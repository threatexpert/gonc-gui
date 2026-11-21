//go:build linux && !android

package winvpn

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestReversePathFilterLifecycle(t *testing.T) {
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, all, original, during, admin, after string
	}{
		{"strict interface", "0", "1", "2", "", "1"},
		{"strict globally", "1", "0", "2", "", "0"},
		{"loose globally", "2", "1", "1", "", "1"},
		{"loose interface", "1", "2", "2", "", "2"},
		{"disabled", "0", "0", "0", "", "0"},
		{"external change", "0", "1", "2", "0", "0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conf := t.TempDir()
			put := func(name, value string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(conf, name, "rp_filter"), []byte(value+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			check := func(name, value string) {
				t.Helper()
				got, err := os.ReadFile(filepath.Join(conf, name, "rp_filter"))
				if err != nil || string(got) != value+"\n" {
					t.Fatalf("%s: got %q, want %s (%v)", name, got, value, err)
				}
			}
			for _, name := range []string{"all", iface.Name} {
				if err := os.Mkdir(filepath.Join(conf, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			put("all", tt.all)
			put(iface.Name, tt.original)
			s := bypassSelection{Name: iface.Name, Index: iface.Index, IP: "192.0.2.1"}
			b := &BypassSession{
				selectExit: func(string, string, bypassSelection) (bypassSelection, error) { return s, nil },
				prepare:    func(sel bypassSelection) (*bypassRoute, error) { return prepareReversePathFilter(conf, sel) },
			}
			if _, _, err := b.Refresh(); err != nil {
				t.Fatal(err)
			}
			check(iface.Name, tt.during)
			check("all", tt.all)
			first := b.route
			s.IP = "192.0.2.2"
			if ip, rev, err := b.Refresh(); err != nil || ip != s.IP || rev != 2 || first != b.route {
				t.Fatalf("address change: ip=%s revision=%d err=%v", ip, rev, err)
			}
			if tt.admin != "" {
				put(iface.Name, tt.admin)
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			check(iface.Name, tt.after)
			check("all", tt.all)
		})
	}
}
