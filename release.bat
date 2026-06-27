@echo off
setlocal

cd /d "%~dp0"

set "VERSION=%~1"
if "%VERSION%"=="" (
  if not exist "%CD%\VERSION" (
    echo Missing VERSION file: %CD%\VERSION
    goto failed
  )
  set /p VERSION=<"%CD%\VERSION"
)
if "%VERSION%"=="" (
  echo VERSION file is empty.
  goto failed
)

set "APP_NAME=gonc-gui"
set "ANDROID_PLATFORM=android-arm64"
set "ANDROID_RELEASE_NAME=%APP_NAME%-%VERSION%-%ANDROID_PLATFORM%"
set "DIST_DIR=%CD%\dist"
set "APK_SRC=%CD%\android\app\build\outputs\apk\release\app-release.apk"
set "APK_PATH=%DIST_DIR%\%ANDROID_RELEASE_NAME%.apk"
set "APK_SHA_PATH=%DIST_DIR%\%ANDROID_RELEASE_NAME%.apk.sha256.txt"

echo Release: %APP_NAME% %VERSION%
echo.

if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"

echo [1/4] Building Windows amd64 package...
call :build_windows "windows/amd64" "windows-amd64" "amd64"
if errorlevel 1 goto failed

echo.
echo [2/4] Building Windows arm64 package...
call :build_windows "windows/arm64" "windows-arm64" "arm64"
if errorlevel 1 goto failed

echo.
echo [3/4] Building Android release APK...
call "%CD%\android\build-release-apk.bat"
if errorlevel 1 goto failed
if not exist "%APK_SRC%" (
  echo Missing required file: %APK_SRC%
  goto failed
)

echo.
echo [4/4] Copying Android APK and writing checksum...
if exist "%APK_PATH%" del /f /q "%APK_PATH%"
if exist "%APK_SHA_PATH%" del /f /q "%APK_SHA_PATH%"
copy /y "%APK_SRC%" "%APK_PATH%" >nul
if errorlevel 1 goto failed
powershell -NoProfile -ExecutionPolicy Bypass -Command "$hash = Get-FileHash -Algorithm SHA256 '%APK_PATH%'; ($hash.Hash.ToLower() + '  ' + [IO.Path]::GetFileName('%APK_PATH%')) | Set-Content -Encoding ASCII '%APK_SHA_PATH%'"
if errorlevel 1 goto failed

echo.
echo Release package completed successfully.
echo Windows amd64: %DIST_DIR%\%APP_NAME%-%VERSION%-windows-amd64.zip
echo Windows arm64: %DIST_DIR%\%APP_NAME%-%VERSION%-windows-arm64.zip
echo APK:           %APK_PATH%
exit /b 0

:build_windows
set "WAILS_TARGET=%~1"
set "PLATFORM=%~2"
set "WINTUN_ARCH=%~3"
set "RELEASE_NAME=%APP_NAME%-%VERSION%-%PLATFORM%"
set "PACKAGE_DIR=%DIST_DIR%\%RELEASE_NAME%"
set "ZIP_PATH=%DIST_DIR%\%RELEASE_NAME%.zip"
set "SHA_PATH=%DIST_DIR%\%RELEASE_NAME%.sha256.txt"

call "%CD%\build.bat" "%WAILS_TARGET%" "--skip-wintun-copy"
if errorlevel 1 exit /b 1

if exist "%PACKAGE_DIR%" rmdir /s /q "%PACKAGE_DIR%"
if exist "%ZIP_PATH%" del /f /q "%ZIP_PATH%"
if exist "%SHA_PATH%" del /f /q "%SHA_PATH%"
mkdir "%PACKAGE_DIR%"
if errorlevel 1 exit /b 1

copy /y "%CD%\build\bin\gonc-gui.exe" "%PACKAGE_DIR%\" >nul
if errorlevel 1 exit /b 1
copy /y "%CD%\wintun\bin\%WINTUN_ARCH%\wintun.dll" "%PACKAGE_DIR%\" >nul
if errorlevel 1 exit /b 1

if exist "%CD%\README.md" copy /y "%CD%\README.md" "%PACKAGE_DIR%\" >nul
if exist "%CD%\LICENSE" copy /y "%CD%\LICENSE" "%PACKAGE_DIR%\" >nul

powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%PACKAGE_DIR%' -DestinationPath '%ZIP_PATH%' -Force"
if errorlevel 1 exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -Command "$hash = Get-FileHash -Algorithm SHA256 '%ZIP_PATH%'; ($hash.Hash.ToLower() + '  ' + [IO.Path]::GetFileName('%ZIP_PATH%')) | Set-Content -Encoding ASCII '%SHA_PATH%'"
if errorlevel 1 exit /b 1

echo Windows package: %ZIP_PATH%
echo SHA256:          %SHA_PATH%
exit /b 0

:failed
echo.
echo Release package failed.
exit /b 1
