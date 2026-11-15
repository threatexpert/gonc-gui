package goncrunner

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Gonc currently exposes the retry delay only through its logger. Recognize
// these exact messages, never generic timeout/UDP/proxy log lines. This affects
// presentation only; route lifecycle decisions still use connection reports.
var vpnRetryLogPattern = regexp.MustCompile(`(?:^|\[gonc\]\s*)Will retry in ([0-9]+) seconds?\.{0,3}$`)
var vpnFailureLogPattern = regexp.MustCompile(`(?:^|\[gonc\]\s*)P2P failed:\s*(.+)$`)

func vpnAttemptProgress(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "wait", "waiting", "discovering", "connecting", "negotiating", "reconnecting":
		return true
	}
	return false
}

func vpnConnectionFailureReason(status string) string {
	status = strings.TrimSpace(status)
	for _, prefix := range []string{"error:", "failed:", "failure:"} {
		if strings.HasPrefix(strings.ToLower(status), prefix) {
			return strings.TrimSpace(status[len(prefix):])
		}
	}
	switch strings.ToLower(status) {
	case "error", "failed", "failure", "timeout", "connection lost":
		return status
	}
	return ""
}

func (v *vpnDetailsTracker) connectionLog(message string, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Logs have no connection Topic. Never let a losing path's late log
	// overwrite a connected tunnel, even while its system VPN is configuring.
	if !v.data.Running || v.stopping || v.data.Connected {
		return false
	}
	changed := false
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if match := vpnFailureLogPattern.FindStringSubmatch(line); match != nil {
			v.data.ConnectionState, v.data.ConnectionError = "failed", strings.TrimSpace(match[1])
			v.retryAt = time.Time{}
			changed = true
		} else if match := vpnRetryLogPattern.FindStringSubmatch(line); match != nil {
			seconds, err := strconv.Atoi(match[1])
			if err != nil || seconds < 1 || seconds > 86400 {
				continue
			}
			v.data.ConnectionState = "retryWait"
			v.retryAt = now.Add(time.Duration(seconds) * time.Second)
			changed = true
		}
	}
	return changed
}

func (v *vpnDetailsTracker) connectionFailure(reason string, terminal bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.data.Running || v.stopping {
		return
	}
	v.data.ConnectionState = "failed"
	if !terminal || v.data.ConnectionError == "" {
		v.data.ConnectionError = strings.TrimSpace(reason)
	}
	v.retryAt = time.Time{}
	if terminal {
		v.data.Connected, v.data.Ready = false, false
	}
}
