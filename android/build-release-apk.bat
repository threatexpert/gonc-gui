@echo off
setlocal
cd /d "%~dp0"

echo Building Gonc release APK...
call "%~dp0gradlew.bat" assembleRelease
if errorlevel 1 (
    echo.
    echo Release APK build failed.
    pause
    exit /b 1
)

echo.
echo Release APK output:
if exist "%~dp0app\build\outputs\apk\release\app-release.apk" echo %~dp0app\build\outputs\apk\release\app-release.apk
if exist "%~dp0app\build\outputs\apk\release\app-release-unsigned.apk" echo %~dp0app\build\outputs\apk\release\app-release-unsigned.apk
echo.
pause
