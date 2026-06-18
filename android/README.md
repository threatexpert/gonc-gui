# Gonc Android Preview

This is the first Android shell for Gonc. It focuses on the mobile entry flow:

- Open the app directly.
- Add files with Android's system document picker.
- Share a file from another app into Gonc with `ACTION_SEND` / `ACTION_SEND_MULTIPLE`.
- Open a file with Gonc through `ACTION_VIEW` when the file manager offers it.
- Generate a passphrase and prepare shared files in the app cache.

The transfer core is behind `GoncBridge`. The current active implementation is
`MobileGoncBridge`, which calls the gomobile-generated `mobilegonc.aar`. Shared
Android `content://` inputs are copied into app-private cache first, then passed to
the Go engine as normal filesystem paths.

## Project Layout

```text
android/
  settings.gradle
  build.gradle
  app/
    build.gradle
    src/main/AndroidManifest.xml
    src/main/java/cn/threatexpert/gonc/
      MainActivity.java
      GoncBridge.java
      MobileGoncBridge.java
      PreviewGoncBridge.java
      FileCache.java
      ShareItem.java
      Passwords.java
    libs/mobilegonc.aar
```

## Build

Open `android/` in Android Studio, let it sync Gradle, then run the `app` target on a
device or emulator.

This workspace has been tested with Gradle wrapper and Android SDK 35:

```powershell
cd D:\threatexpert.cn\open\gonc-gui\android
.\gradlew.bat assembleDebug
```

## Mobile Core Hook

The bridge boundary is:

```java
Session startP2PShare(Context context, List<ShareItem> items, String password, boolean useUdp, EventCallback callback);
Session startP2PReceive(Context context, String password, boolean useUdp, EventCallback callback);
```

The first real transfer bridge is now:

- Go package: `D:\threatexpert.cn\open\gonetcat\mobilegonc`
- Android bridge: `app/src/main/java/cn/threatexpert/gonc/MobileGoncBridge.java`
- AAR output: `app/libs/mobilegonc.aar`

Regenerate the AAR after changing `mobilegonc`:

```powershell
cd D:\threatexpert.cn\open\gonetcat
$env:GOPROXY="https://goproxy.cn,direct"
$env:ANDROID_HOME="D:\android\sdk"
$env:ANDROID_SDK_ROOT="D:\android\sdk"
$env:GOFLAGS="-buildvcs=false"
gomobile bind -target=android/arm64 -androidapi=26 -trimpath -ldflags="-s -w -buildid= -checklinkname=0" -o D:\threatexpert.cn\open\gonc-gui\android\app\libs\mobilegonc.aar github.com/threatexpert/gonc/v2/mobilegonc
cd D:\threatexpert.cn\open\gonc-gui\android
.\scripts\strip-mobilegonc-aar.ps1
```

This first bridge is path-based. A later bridge should stream files from
`ContentResolver` directly to avoid duplicating very large shared files in cache.
