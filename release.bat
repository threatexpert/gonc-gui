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

set "PLATFORM=windows-amd64"
set "ANDROID_PLATFORM=android-arm64"
set "APP_NAME=gonc-gui"
set "RELEASE_NAME=%APP_NAME%-%VERSION%-%PLATFORM%"
set "ANDROID_RELEASE_NAME=%APP_NAME%-%VERSION%-%ANDROID_PLATFORM%"
set "DIST_DIR=%CD%\dist"
set "PACKAGE_DIR=%DIST_DIR%\%RELEASE_NAME%"
set "ZIP_PATH=%DIST_DIR%\%RELEASE_NAME%.zip"
set "SHA_PATH=%DIST_DIR%\%RELEASE_NAME%.sha256.txt"
set "APK_SRC=%CD%\android\app\build\outputs\apk\release\app-release.apk"
set "APK_PATH=%DIST_DIR%\%ANDROID_RELEASE_NAME%.apk"
set "APK_SHA_PATH=%DIST_DIR%\%ANDROID_RELEASE_NAME%.apk.sha256.txt"

echo Release: %RELEASE_NAME%
echo.

echo [1/7] Building Windows application...
call "%CD%\build.bat"
if errorlevel 1 goto failed

echo.
echo [2/7] Building Android release APK...
call "%CD%\android\build-release-apk.bat"
if errorlevel 1 goto failed
if not exist "%APK_SRC%" (
  echo Missing required file: %APK_SRC%
  goto failed
)

echo.
echo [3/7] Preparing package directory...
if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
if exist "%PACKAGE_DIR%" rmdir /s /q "%PACKAGE_DIR%"
if exist "%ZIP_PATH%" del /f /q "%ZIP_PATH%"
if exist "%SHA_PATH%" del /f /q "%SHA_PATH%"
if exist "%APK_PATH%" del /f /q "%APK_PATH%"
if exist "%APK_SHA_PATH%" del /f /q "%APK_SHA_PATH%"
mkdir "%PACKAGE_DIR%"
if errorlevel 1 goto failed

echo.
echo [4/7] Copying Windows release files...
copy /y "%CD%\build\bin\gonc-gui.exe" "%PACKAGE_DIR%\" >nul
if errorlevel 1 goto failed
copy /y "%CD%\build\bin\wintun.dll" "%PACKAGE_DIR%\" >nul
if errorlevel 1 goto failed

if exist "%CD%\README.md" copy /y "%CD%\README.md" "%PACKAGE_DIR%\" >nul
if exist "%CD%\LICENSE" copy /y "%CD%\LICENSE" "%PACKAGE_DIR%\" >nul

echo.
echo [5/7] Creating zip package...
powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%PACKAGE_DIR%' -DestinationPath '%ZIP_PATH%' -Force"
if errorlevel 1 goto failed

echo.
echo [6/7] Copying Android APK...
copy /y "%APK_SRC%" "%APK_PATH%" >nul
if errorlevel 1 goto failed

echo.
echo [7/7] Writing SHA256 checksums...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$hash = Get-FileHash -Algorithm SHA256 '%ZIP_PATH%'; ($hash.Hash.ToLower() + '  ' + [IO.Path]::GetFileName('%ZIP_PATH%')) | Set-Content -Encoding ASCII '%SHA_PATH%'"
if errorlevel 1 goto failed
powershell -NoProfile -ExecutionPolicy Bypass -Command "$hash = Get-FileHash -Algorithm SHA256 '%APK_PATH%'; ($hash.Hash.ToLower() + '  ' + [IO.Path]::GetFileName('%APK_PATH%')) | Set-Content -Encoding ASCII '%APK_SHA_PATH%'"
if errorlevel 1 goto failed

echo.
echo Release package completed successfully.
echo Zip:         %ZIP_PATH%
echo Zip SHA256:  %SHA_PATH%
echo APK:         %APK_PATH%
echo APK SHA256:  %APK_SHA_PATH%
exit /b 0

:failed
echo.
echo Release package failed.
exit /b 1
