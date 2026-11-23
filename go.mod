module gonc-gui

go 1.27.1

require (
	github.com/go-ole/go-ole v1.3.0
	github.com/kbinani/screenshot v0.0.0-20250624051815-089614a94018
	github.com/klauspost/compress v1.20.1
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e
	github.com/threatexpert/gonc/v2 v2.6.14-0.20261123160155-c4b5503a1d0e
	github.com/wailsapp/wails/v2 v2.17.1
	github.com/xjasonlyu/tun2socks/v2 v2.7.0
	github.com/zeebo/blake3 v0.2.4
	golang.org/x/image v0.42.0
	golang.org/x/net v0.61.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
	golang.zx2c4.com/wireguard v0.0.0-20261006164505-2631ce99a06f
)

require (
	git.sr.ht/~jackmordaunt/go-toast/v2 v2.0.3 // indirect
	github.com/ajg/form v1.9.0 // indirect
	github.com/bep/debounce v1.2.1 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/eclipse/paho.mqtt.golang v1.5.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.16 // indirect
	github.com/gen2brain/shm v0.1.0 // indirect
	github.com/go-chi/chi/v5 v5.4.0 // indirect
	github.com/go-chi/cors v1.2.2 // indirect
	github.com/go-chi/render v1.0.3 // indirect
	github.com/go-gost/relay v0.7.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20210711035445-715c2860da7e // indirect
	github.com/jezek/xgb v1.1.1 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/klauspost/reedsolomon v1.12.6 // indirect
	github.com/labstack/echo/v4 v4.16.1 // indirect
	github.com/labstack/gommon v0.5.0 // indirect
	github.com/leaanthony/go-ansi-parser v1.6.1 // indirect
	github.com/leaanthony/gosod v1.0.4 // indirect
	github.com/leaanthony/slicer v1.6.0 // indirect
	github.com/leaanthony/u v1.1.1 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mdp/qrterminal/v3 v3.2.1 // indirect
	github.com/pion/dtls/v3 v3.1.10 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/stun/v3 v3.1.6 // indirect
	github.com/pion/transport/v4 v4.0.2 // indirect
	github.com/pion/transport/v5 v5.0.0 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/samber/lo v1.49.1 // indirect
	github.com/threatexpert/go-winpty v1.0.3 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	github.com/tkrajina/go-reflector v0.5.8 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/wailsapp/go-webview2 v1.0.22 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	github.com/xtaci/kcp-go/v5 v5.6.72 // indirect
	github.com/xtaci/smux v1.5.57 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/exp v0.0.0-20261011100332-faecfb24eb49 // indirect
	golang.org/x/mobile v0.0.0-20260611195102-4dd8f1dbf5d2 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/term v0.47.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	gvisor.dev/gvisor v0.0.0-20261010064756-cd110d9dec04 // indirect
	rsc.io/qr v0.2.0 // indirect
)

// Keep upstream import paths, but pin our fork with deferred TCP and IPv6 rejection.
replace github.com/xjasonlyu/tun2socks/v2 => github.com/threatexpert/tun2socks/v2 v2.0.0-20261120121624-b0ed9683704c

tool golang.org/x/mobile/cmd/gobind
