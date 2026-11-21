package goncrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Decide once, before the reconnect DNS sockets are bound and marked.
func needsLinuxDNSCapture(ctx context.Context) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	tool, err := exec.LookPath("resolvectl")
	if err != nil {
		return true
	}
	if !resolvConfUsesStub("/etc/resolv.conf", "/run/systemd/resolve/resolv.conf") {
		return true
	}
	// An installed CLI or a leftover stub file alone cannot configure link DNS.
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return exec.CommandContext(probe, tool, "dns").Run() != nil
}

func resolvConfUsesStub(conf, upstreamList string) bool {
	path, err := filepath.EvalSymlinks(conf)
	// resolved's upstream list cannot express per-link DNS routing.
	if err != nil || path == upstreamList {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && usesResolvedStub(string(data))
}

func usesResolvedStub(conf string) bool {
	found := false
	for _, line := range strings.Split(conf, "\n") {
		line, _, _ = strings.Cut(line, "#")
		line, _, _ = strings.Cut(line, ";")
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "nameserver" {
			continue
		}
		if len(fields) < 2 || (fields[1] != "127.0.0.53" && fields[1] != "127.0.0.54") {
			return false
		}
		found = true
	}
	return found
}
