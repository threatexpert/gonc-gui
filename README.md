# gonc-gui

Desktop GUI wrapper for `gonc` P2P file transfer.

This project is intentionally separate from `gonetcat`. It does not import
`gonetcat` packages. It starts the `gonc` executable as a child process and
streams logs back to the UI.

## Layout

```text
gonc-gui/
  app.go                    Wails backend methods exposed to the frontend
  internal/goncrunner/      child process runner for gonc
  frontend/                 React UI
  bundled/gonc/             optional per-platform gonc executables
  scripts/                  helper scripts
```

## Development

Install dependencies:

```powershell
cd D:\threatexpert.cn\open\gonc-gui
npm install --prefix frontend
```

Run in development mode:

```powershell
$env:GOPROXY="https://goproxy.cn,direct"
wails dev
```

Build:

```powershell
$env:GOPROXY="https://goproxy.cn,direct"
wails build
```

## gonc executable lookup

At runtime, the app tries these locations:

1. Custom path entered in the UI
2. `bundled/gonc/<goos>-<goarch>/gonc(.exe)` beside the working directory
3. Sibling development checkout: `..\gonetcat\bin\gonc(.exe)`
4. Sibling development checkout: `..\gonetcat\gonc(.exe)`
5. `PATH`

For local Windows development, build `gonetcat` first, then run:

```powershell
.\scripts\sync-gonc.ps1 -GonetcatDir ..\gonetcat
```

## First supported flow

Sender:

```text
gonc -p2p <passphrase> -httpserver <file-or-folder>...
```

Receiver:

```text
gonc -p2p <passphrase> -download <save-dir>
```
