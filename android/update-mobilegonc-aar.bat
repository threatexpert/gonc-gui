@echo off
setlocal
cd /d "%~dp0"

set "ANDROID_DIR=%~dp0"

if "%ANDROID_HOME%"=="" set "ANDROID_HOME=D:\Android\sdk"
if "%ANDROID_SDK_ROOT%"=="" set "ANDROID_SDK_ROOT=D:\Android\sdk"
if "%GOPROXY%"=="" set "GOPROXY=https://goproxy.cn,direct"
set "GOFLAGS=-buildvcs=false"

echo Updating Android mobilegonc.aar...
echo ANDROID_HOME=%ANDROID_HOME%
echo ANDROID_SDK_ROOT=%ANDROID_SDK_ROOT%
echo.

cd /d "%ANDROID_DIR%..\..\gonetcat"
if errorlevel 1 (
    echo Failed to enter ..\..\gonetcat
    timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
    exit /b 1
)

gomobile bind -target=android/arm64 -androidapi=26 -trimpath -ldflags="-s -w -buildid= -checklinkname=0" -o "%ANDROID_DIR%app\libs\mobilegonc.aar" github.com/threatexpert/gonc/v2/mobilegonc
if errorlevel 1 (
    echo.
    echo gomobile bind failed.
    timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
    exit /b 1
)

cd /d "%ANDROID_DIR%"
if errorlevel 1 (
    echo Failed to enter android directory.
    timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
    exit /b 1
)

powershell.exe -NoProfile -ExecutionPolicy Bypass -File ".\scripts\strip-mobilegonc-aar.ps1"
if errorlevel 1 (
    echo.
    echo strip-mobilegonc-aar failed.
    timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
    exit /b 1
)

echo.
echo mobilegonc.aar updated:
echo %ANDROID_DIR%app\libs\mobilegonc.aar
echo.
echo Cleaning Gradle build cache so next APK build picks up the new AAR...
call "%ANDROID_DIR%gradlew.bat" clean
if errorlevel 1 (
    echo.
    echo Gradle clean failed.
    timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
    exit /b 1
)

echo.
echo Done. Run build-debug-apk.bat or build-release-apk.bat to build the APK.
echo.
timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
