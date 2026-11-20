#!/usr/bin/env bash
set -euo pipefail

binary=$(realpath "$1")
test_home=$(mktemp -d)
cleanup_home() {
  # WebKit helpers can finish writing caches just after the GUI exits.
  for delay in 0 0.1 0.3 1; do
    sleep "$delay"
    if rm -rf -- "$test_home"; then return 0; fi
  done
  return 1
}
trap cleanup_home EXIT
export HOME="$test_home" XDG_CONFIG_HOME="$test_home/config" XDG_RUNTIME_DIR="$test_home/runtime"
mkdir -m 700 "$XDG_RUNTIME_DIR"

# React calls the Go method LoadVPNProfiles on mount. A writer can open this
# FIFO only after the real embedded page has rendered and invoked that method.
profile="$XDG_CONFIG_HOME/Gonc/gonc-gui/vpn-profiles.json.enc"
mkdir -p "$(dirname "$profile")"
mkfifo "$profile"
timeout --kill-after=5s 90s xvfb-run -a dbus-run-session -- bash -eu -c '
  LD_BIND_NOW=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 "$1" &
  app_pid=$!
  stop_app() {
    kill "$app_pid" 2>/dev/null || true
    wait "$app_pid" || true
  }
  trap stop_app EXIT
  echo "{}" > "$2"
  kill -0 "$app_pid"
' _ "$binary" "$profile"
