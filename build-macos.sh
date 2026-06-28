#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

target="${1:-}"
if [[ -z "$target" ]]; then
  arch="$(uname -m)"
  case "$arch" in
    arm64) target="darwin/arm64" ;;
    x86_64) target="darwin/amd64" ;;
    *)
      echo "Unsupported macOS architecture: $arch" >&2
      exit 1
      ;;
  esac
fi

case "$target" in
  amd64) target="darwin/amd64" ;;
  arm64) target="darwin/arm64" ;;
  darwin/amd64|darwin/arm64) ;;
  *)
    echo "Unsupported build target: $target" >&2
    echo "Usage: ./build-macos.sh [darwin/amd64|darwin/arm64]" >&2
    exit 1
    ;;
esac

echo "[1/4] Checking macOS build dependencies..."
missing=()
for cmd in go npm wails xcodebuild; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    missing+=("$cmd")
  fi
done
if ((${#missing[@]} > 0)); then
  echo "Missing required command(s): ${missing[*]}" >&2
  echo "Install Xcode Command Line Tools, Go, Node.js/npm, and Wails first." >&2
  echo "Xcode tools: xcode-select --install" >&2
  echo "Wails CLI: go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0" >&2
  exit 1
fi

echo "[2/4] Setting Go proxy..."
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

echo "[3/4] Installing frontend dependencies..."
npm install --prefix frontend

echo "[4/4] Building gonc-gui..."
wails build -platform "$target"

echo
echo "Build completed successfully."
echo "Target: $target"
echo "Output: $(pwd)/build/bin/gonc-gui.app"
