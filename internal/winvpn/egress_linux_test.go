package winvpn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEgressGuardRollsBackIPv4IfIPv6FilterFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	trace := filepath.Join(dir, "calls")
	t.Setenv("TRACE", trace)
	for _, tool := range []string{"iptables", "ip6tables"} {
		script := "#!/bin/sh\necho \"$0 $*\" >> \"$TRACE\"\n"
		if tool == "ip6tables" {
			script += "exit 1\n"
		}
		if err := os.WriteFile(filepath.Join(dir, tool), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	g, err := InstallEgressGuard(73)
	if err == nil || g.Ready() || len(g.undo) != 0 {
		t.Fatalf("partial dual-stack protection: %+v, %v", g, err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if !strings.Contains(log, "-D OUTPUT -j GONC_OUT_00000049") || !strings.HasSuffix(log, "-X GONC_OUT_00000049\n") {
		t.Fatal(log)
	}
}
