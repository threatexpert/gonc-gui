@echo off
setlocal
cd /d "%~dp0"

if "%ANDROID_HOME%"=="" set "ANDROID_HOME=D:\Android\sdk"
if "%ANDROID_SDK_ROOT%"=="" set "ANDROID_SDK_ROOT=D:\Android\sdk"
if "%GOPROXY%"=="" set "GOPROXY=https://goproxy.cn,direct"
set "GOFLAGS=-buildvcs=false"

echo Updating Android mobilegonc.aar...
echo ANDROID_HOME=%ANDROID_HOME%
echo ANDROID_SDK_ROOT=%ANDROID_SDK_ROOT%
echo.

cd /d "%~dp0..\gonetcat"
if errorlevel 1 (
    echo Failed to enter ..\gonetcat
    pause
    exit /b 1
)

gomobile bind -target=android/arm64 -androidapi=26 -trimpath -ldflags="-s -w -buildid= -checklinkname=0" -o "..\gonc-gui\android\app\libs\mobilegonc.aar" github.com/threatexpert/gonc/v2/mobilegonc
if errorlevel 1 (
    echo.
    echo gomobile bind failed.
    pause
    exit /b 1
)

cd /d "%~dp0android"
if errorlevel 1 (
    echo Failed to enter android directory.
    pause
    exit /b 1
)

powershell.exe -NoProfile -ExecutionPolicy Bypass -File ".\scripts\strip-mobilegonc-aar.ps1"
if errorlevel 1 (
    echo.
    echo strip-mobilegonc-aar failed.
    pause
    exit /b 1
)

echo.
echo mobilegonc.aar updated:
echo %~dp0android\app\libs\mobilegonc.aar
echo.
pause
