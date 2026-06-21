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

After changing `..\gonetcat\mobilegonc` or related Go packages, run from this
directory:

```text
update-mobilegonc-aar.bat
```

The script:

1. Changes directory to `..\..\gonetcat`.
2. Runs `gomobile bind` for `github.com/threatexpert/gonc/v2/mobilegonc`.
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
func StartP2PReceive(password string, useUDP bool, cb Callback) (*Session, error)
```
