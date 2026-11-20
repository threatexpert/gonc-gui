#!/usr/bin/env bash
set -euo pipefail

# Buster's signed packages are now hosted in the Debian archive.
echo 'deb https://archive.debian.org/debian buster main' > /etc/apt/sources.list
echo 'deb [check-valid-until=no] https://archive.debian.org/debian-security buster/updates main' > /etc/apt/sources.list.d/buster-security.list
export DEBIAN_FRONTEND=noninteractive
apt-get -o Acquire::https::CaInfo=/tmp/runner-ca.crt -o Acquire::Retries=3 update
apt-get -o Acquire::https::CaInfo=/tmp/runner-ca.crt -o Acquire::Retries=3 install -y --no-install-recommends \
  build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev xvfb xauth dbus

test "$(getconf GNU_LIBC_VERSION)" = "glibc 2.28"
# The native Wails build already generated the bindings and frontend/dist.
export PATH="/usr/local/go/bin:$PATH"
export GOWORK=off GOTOOLCHAIN=local GOMODCACHE=/go/pkg/mod GOPROXY=off CGO_ENABLED=1
binary=build/bin/gonc-gui
go build -mod=readonly -buildvcs=false -tags desktop,production -ldflags="-s -w" -o "$binary" .

readelf -d "$binary" | grep -F 'Shared library: [libwebkit2gtk-4.0.so.37]'
highest=$(readelf --version-info "$binary" | grep -oE 'GLIBC_[0-9]+(\.[0-9]+)+' | sort -Vu | tail -1)
echo "Highest required glibc symbol version: $highest"
dpkg --compare-versions "${highest#GLIBC_}" le 2.28

# Docker restricts nested user namespaces. This applies only to the test
# WebView loading the bundled page in this disposable container.
install -m 755 "$binary" /tmp/gonc-gui
runuser -u nobody -- env WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 \
  bash .github/scripts/smoke-linux.sh /tmp/gonc-gui
