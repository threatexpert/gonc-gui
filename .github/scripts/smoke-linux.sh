#!/usr/bin/env bash
set -euo pipefail

binary=$(realpath "$1")
test_home=$(mktemp -d)
trap 'rm -rf -- "$test_home"' EXIT
export HOME="$test_home" XDG_CONFIG_HOME="$test_home/config" XDG_RUNTIME_DIR="$test_home/runtime"
mkdir -m 700 "$XDG_RUNTIME_DIR"

# React calls the Go method LoadVPNProfiles on mount. A writer can open this
# FIFO only after the real embedded page has rendered and invoked that method.
profile="$XDG_CONFIG_HOME/Gonc/gonc-gui/vpn-profiles.json.enc"
mkdir -p "$(dirname "$profile")"
mkfifo "$profile"
timeout 90 xvfb-run -a dbus-run-session -- bash -eu -c '
  LD_BIND_NOW=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 "$1" &
  app_pid=$!
  trap "kill $app_pid 2>/dev/null || true" EXIT
  echo "{}" > "$2"
  kill -0 "$app_pid"
' _ "$binary" "$profile"
