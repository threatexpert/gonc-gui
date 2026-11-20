package winvpn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gonc-gui/internal/vpnconfig"
)

func TestMissingFirewallToolFailsBeforeConfiguringVPN(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := captureFamilies(); err == nil || !strings.Contains(err.Error(), "requires iptables") {
		t.Fatal(err)
	}
}

func TestDNSCaptureSelectsOneFirewallBackend(t *testing.T) {
	for _, rejectNFT := range []bool{false, true} {
		t.Run(map[bool]string{false: "nft only", true: "nft transaction rejected"}[rejectNFT], func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			trace := filepath.Join(dir, "trace")
			t.Setenv("TRACE", trace)
			nft := "#!/bin/sh\necho \"nft $*\" >> \"$TRACE\"\nif [ \"$1\" = '-f' ]; then /bin/cat >> \"$TRACE\"; fi\n"
			if rejectNFT {
				nft += "exit 1\n"
				for _, tool := range []string{"iptables", "ip6tables"} {
					if err := os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\necho \"xt $*\" >> \"$TRACE\"\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(nft), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := captureFamilies(); err != nil {
				t.Fatal(err)
			}
			c := new(linuxDNSCapture)
			err := c.installRules([]dnsFamily{{"iptables", "127.0.0.1"}, {"ip6tables", "::1"}}, "GONC_TEST", []int{15453, 15454}, vpnconfig.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			log := string(data)
			if strings.Count(log, "nft -f -") != 1 || strings.Contains(log, "xt -w") != rejectNFT ||
				strings.Contains(log, "nft delete table ip6 GONC_TEST") == rejectNFT {
				t.Fatalf("unexpected install/cleanup commands:\n%s", log)
			}
		})
	}
}

func TestDNSCaptureRuleOrderAndRollback(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "calls")
	t.Setenv("GONC_RULE_TRACE", trace)
	tool := filepath.Join(dir, "iptables")
	const script = "#!/bin/sh\necho \"$*\" >> \"$GONC_RULE_TRACE\"\n[ \"$4 $5 $6\" != 'nat -I OUTPUT' ]\n"
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := vpnconfig.Config{LinuxDNSMark: 73, LinuxDNSBypass: "192.0.2.5:53", LinuxDNSDevice: "eth0"}
	c := new(linuxDNSCapture)
	if err := c.installFamilyRules("iptables", "GONC_TEST", 15453, cfg); err == nil {
		t.Fatal("expected nat OUTPUT failure")
	}
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, table := range []string{"nat", "filter"} {
		for _, proto := range []string{"tcp", "udp"} {
			prefix := table + " -A GONC_TEST -p " + proto + " --dport 53 "
			allow := strings.Index(log, prefix+"-d 192.0.2.5 -o eth0 -m mark --mark 0x49 -j RETURN")
			action := "-j REDIRECT --to-ports 15453"
			if table == "filter" {
				action = "! -o lo -j REJECT"
			}
			if allow < 0 || strings.Index(log, prefix+action) <= allow {
				t.Fatalf("missing or misplaced %s %s exception\n%s", table, proto, log)
			}
		}
	}
	if !strings.Contains(log, "filter -D OUTPUT -j GONC_TEST") || strings.Contains(log, "nat -D OUTPUT") || !strings.HasSuffix(log, "filter -X GONC_TEST\n") {
		t.Fatalf("incorrect partial rollback\n%s", log)
	}
}
