@echo off
setlocal
cd /d "%~dp0"

echo Building Gonc debug APK...
call "%~dp0gradlew.bat" assembleDebug
if errorlevel 1 (
    echo.
    echo Debug APK build failed.
    timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
    exit /b 1
)

echo.
echo Debug APK built:
echo %~dp0app\build\outputs\apk\debug\app-debug.apk
echo.
timeout 5 >nul 2>nul || ping -n 6 127.0.0.1 >nul
