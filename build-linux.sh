#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

target="${1:-linux/amd64}"
tags="${WAILS_TAGS:-webkit2_41}"

case "$target" in
  amd64) target="linux/amd64" ;;
  arm64) target="linux/arm64" ;;
  linux/amd64|linux/arm64) ;;
  *)
    echo "Unsupported build target: $target" >&2
    echo "Usage: ./build-linux.sh [linux/amd64|linux/arm64]" >&2
    exit 1
    ;;
esac

echo "[1/5] Checking Linux build dependencies..."
missing=()
for cmd in go npm wails pkg-config gcc; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    missing+=("$cmd")
  fi
done
if ((${#missing[@]} > 0)); then
  echo "Missing required command(s): ${missing[*]}" >&2
  echo "Install Go, Node.js/npm, Wails, and build tools first." >&2
  echo "Ubuntu example: sudo apt install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev" >&2
  echo "Wails CLI: go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0" >&2
  exit 1
fi
if ! pkg-config --exists gtk+-3.0 webkit2gtk-4.1; then
  echo "Missing GTK/WebKitGTK development packages." >&2
  echo "Ubuntu 22/24 example: sudo apt install -y libgtk-3-dev libwebkit2gtk-4.1-dev" >&2
  exit 1
fi

echo "[2/5] Setting Go proxy..."
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

echo "[3/5] Installing frontend dependencies..."
npm install --prefix frontend

echo "[4/5] Building gonc-gui..."
if [[ -n "$tags" ]]; then
  wails build -platform "$target" -tags "$tags"
else
  wails build -platform "$target"
fi

echo "[5/5] Done."
echo
echo "Build completed successfully."
echo "Target: $target"
echo "Tags: ${tags:-<none>}"
echo "Output: $(pwd)/build/bin/gonc-gui"
