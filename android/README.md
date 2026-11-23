# Gonc Android

Android client for Gonc point-to-point secure file transfer.

The app supports:

- Send files selected from Android's document picker.
- Send folders selected from Android's directory picker.
- Receive files through a local `-httplocal` endpoint after P2P connects.
- Browse the peer's shared files, refresh the listing on demand, and download all
  or selected files.
- Share files from another app into Gonc with `ACTION_SEND` /
  `ACTION_SEND_MULTIPLE`.
- Open files with Gonc through `ACTION_VIEW` when a file manager offers it.
- Scan and display passphrase QR codes.
- Send to and receive from Gonc devices on the same network without a
  passphrase (Nearby), including the desktop app.

## Nearby

Send Files and Receive Files each have a **Passphrase | Nearby** switch where
the passphrase settings were. There is no separate screen.

- **Send Files, Nearby:** the passphrase area becomes a list of nearby
  devices. A paired device shows **Send**; an unpaired device shows **Pair**
  (compare a six-digit number on both devices) and, if it accepts files from
  unpaired devices, **Send** without pairing. Progress and **Cancel** appear in
  the device row.
- **Receive Files, Nearby:** a banner shows the name others see ("Ready to
  receive as ..."), with **Rename**, the **Accept files from unpaired devices**
  switch and the paired devices (**Forget**).
- **Incoming files:** a dialog shows the sender, the files and their size, and
  warns when the sender is not paired. **Accept** switches to Receive Files and
  downloads everything into the save location with the usual receiver.

The phone announces itself and looks for others only while a Nearby view is
on screen and the app is in the foreground, holding a Wi-Fi multicast lock for
that time. Transfers that are already running continue in the background.
The device identity and pairing keys are stored in `noBackupFilesDir`, sealed
with a key that is itself wrapped by an Android Keystore key.

## Received file actions

The current receive session marks a remote file as locally available only when
the expected target exists, is readable, is a regular file, and has exactly the
size reported by the remote listing. This lightweight check enables file actions
but is not a content-integrity guarantee.

Tap an available file's icon or name to open it through Android. Its overflow
menu also provides Open, Open with, Share, and File information. Availability
and resolved targets belong only to the current in-memory receive session; the
app does not persist a received-file or download history across process exits.

APK files use the same open action. Gonc requests
`android.permission.REQUEST_INSTALL_PACKAGES` only so it can hand the APK to
Android's package installer. Android controls unknown-source authorization and
installation confirmation; Gonc neither requests `INSTALL_PACKAGES` nor installs
packages silently.

## Architecture

The active bridge is `MobileGoncBridge`, backed by the gomobile-generated
`app/libs/mobilegonc.aar`.

Sending uses `AndroidFileSource` instead of copying selected files into app cache.
The Go HTTP file server asks Android for:

- `Stat`
- `ReadDir`
- `Open`
- `Read`
- `Close`

That lets `content://` files and selected folders stream from Android on demand.

Relevant files:

```text
android/
  app/build.gradle
  app/libs/mobilegonc.aar
  app/src/main/AndroidManifest.xml
  app/src/main/java/cn/threatexpert/gonc/
    MainActivity.java
    GoncBridge.java
    MobileGoncBridge.java
    NearbyController.java
    NearbyKey.java
    NearbyModel.java
    AndroidFileSource.java
    HttpReceiver.java
    ShareItem.java
    Passwords.java
  scripts/strip-mobilegonc-aar.ps1
  update-mobilegonc-aar.bat
  build-debug-apk.bat
  build-release-apk.bat
```

## Build APKs

From this directory:

```powershell
.\gradlew.bat assembleDebug
```

Or double-click:

```text
build-debug-apk.bat
```

Debug output:

```text
app\build\outputs\apk\debug\app-debug.apk
```

Release build:

```text
build-release-apk.bat
```

Release builds require signing. Create a private keystore outside git, then copy
`keystore.properties.example` to `keystore.properties` and fill in the real
values:

```text
storeFile=../gonc-release.jks
storePassword=your-store-password
keyAlias=gonc
keyPassword=your-key-password
```

`keystore.properties`, `*.jks`, and `*.keystore` are ignored by git.

Signed release output:

```text
app\build\outputs\apk\release\app-release.apk
```

Unsigned release APKs are intentionally blocked.

## Update mobilegonc.aar

After changing `android/nearbygo`, the shared Nearby code, or the gonc version in
gonc-gui's `go.mod`, run from this
directory:

```text
update-mobilegonc-aar.bat
```

The script:

1. Changes directory to the gonc-gui root.
2. Runs `gomobile bind` for gonc's `github.com/threatexpert/gonc/v2/mobilegonc`
   (passphrase transfers and VPN) and gonc-gui's `gonc-gui/android/nearbygo`
   (Nearby), in one AAR. The gonc version is the one gonc-gui's `go.mod` pins,
   the same as the desktop app; to try unmerged gonc changes, add a temporary
   `replace github.com/threatexpert/gonc/v2 => ../gonetcat`.
3. Writes `app\libs\mobilegonc.aar`.
4. Runs `scripts\strip-mobilegonc-aar.ps1`.

It only sets `ANDROID_HOME` and `ANDROID_SDK_ROOT` to `D:\Android\sdk` when those
environment variables are empty. Override them in your shell if your SDK is
elsewhere.

The gomobile flags used by the script include:

```text
-target=android/arm64
-androidapi=26
-trimpath
-ldflags="-s -w -buildid= -checklinkname=0"
```

## Bridge API

Java-side bridge boundary:

```java
Session startP2PShare(Context context, List<ShareItem> items, String password, boolean useUdp, EventCallback callback);
Session startP2PReceive(Context context, String password, boolean useUdp, EventCallback callback);
```

Go-side exported mobile API:

```go
func StartP2PShareSource(source AndroidFileSource, password string, useUDP bool, cb Callback) (*Session, error)
func StartP2PShareSourceWithLAN(source AndroidFileSource, password string, useUDP bool, p2pWithLAN bool, cb Callback) (*Session, error)
func StartP2PReceive(password string, useUDP bool, cb Callback) (*Session, error)
func StartP2PReceiveWithLAN(password string, useUDP bool, p2pWithLAN bool, cb Callback) (*Session, error)
```

Nearby lives in gonc-gui's `android/nearbygo` package (Java package `nearbygo`),
a thin wrapper around the desktop app's `internal/nearbyapp` manager:

```go
func StartNearby(storePath string, storeKey []byte, defaultName string, deviceType string, listener NearbyListener) (*NearbyNode, error)
```

`NearbyNode` reports devices, pairings and transfers to `NearbyListener` as
JSON events. For an accepted transfer it reports status `ready` with the
local HTTP endpoint of the sender's share; the app downloads from it and then
calls `FinishReceive`.
