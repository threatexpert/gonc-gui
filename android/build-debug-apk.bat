@echo off
setlocal
cd /d "%~dp0"

echo Building Gonc debug APK...
call "%~dp0gradlew.bat" assembleDebug
if errorlevel 1 (
    echo.
    echo Debug APK build failed.
    pause
    exit /b 1
)

echo.
echo Debug APK built:
echo %~dp0app\build\outputs\apk\debug\app-debug.apk
echo.
pause
