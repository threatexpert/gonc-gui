@echo off
setlocal
cd /d "%~dp0"

set "RELEASE_DIR=%~dp0app\build\outputs\apk\release"
set "SIGNED_APK=%RELEASE_DIR%\app-release.apk"
set "UNSIGNED_APK=%RELEASE_DIR%\app-release-unsigned.apk"

if exist "%SIGNED_APK%" del /f /q "%SIGNED_APK%"
if exist "%UNSIGNED_APK%" del /f /q "%UNSIGNED_APK%"

echo Building Gonc release APK...
call "%~dp0gradlew.bat" assembleRelease
if errorlevel 1 (
    echo.
    echo Release APK build failed.
    echo Check android\keystore.properties. Release builds require a configured signing key.
    pause
    exit /b 1
)

if exist "%UNSIGNED_APK%" (
    echo.
    echo ERROR: unsigned release APK was produced, refusing to publish it:
    echo %UNSIGNED_APK%
    pause
    exit /b 1
)

if not exist "%SIGNED_APK%" (
    echo.
    echo ERROR: signed release APK was not produced:
    echo %SIGNED_APK%
    pause
    exit /b 1
)

echo.
echo Signed release APK:
echo %SIGNED_APK%
echo.
pause
