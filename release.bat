@echo off
setlocal

cd /d "%~dp0"

set "VERSION=%~1"
if "%VERSION%"=="" set "VERSION=v1.0.0"

set "PLATFORM=windows-amd64"
set "APP_NAME=gonc-gui"
set "RELEASE_NAME=%APP_NAME%-%VERSION%-%PLATFORM%"
set "DIST_DIR=%CD%\dist"
set "PACKAGE_DIR=%DIST_DIR%\%RELEASE_NAME%"
set "ZIP_PATH=%DIST_DIR%\%RELEASE_NAME%.zip"
set "SHA_PATH=%DIST_DIR%\%RELEASE_NAME%.sha256.txt"

echo Release: %RELEASE_NAME%
echo.

echo [1/5] Building application...
call "%CD%\build.bat"
if errorlevel 1 goto failed

echo.
echo [2/5] Preparing package directory...
if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
if exist "%PACKAGE_DIR%" rmdir /s /q "%PACKAGE_DIR%"
if exist "%ZIP_PATH%" del /f /q "%ZIP_PATH%"
if exist "%SHA_PATH%" del /f /q "%SHA_PATH%"
mkdir "%PACKAGE_DIR%"
if errorlevel 1 goto failed

echo.
echo [3/5] Copying release files...
copy /y "%CD%\build\bin\gonc-gui.exe" "%PACKAGE_DIR%\" >nul
if errorlevel 1 goto failed

if not exist "%CD%\build\bin\bundled\gonc\windows-amd64\gonc.exe" (
  echo Missing required file: build\bin\bundled\gonc\windows-amd64\gonc.exe
  goto failed
)

xcopy "%CD%\build\bin\bundled" "%PACKAGE_DIR%\bundled\" /e /i /y >nul
if errorlevel 1 goto failed

if exist "%CD%\README.md" copy /y "%CD%\README.md" "%PACKAGE_DIR%\" >nul
if exist "%CD%\LICENSE" copy /y "%CD%\LICENSE" "%PACKAGE_DIR%\" >nul

echo.
echo [4/5] Creating zip package...
powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%PACKAGE_DIR%' -DestinationPath '%ZIP_PATH%' -Force"
if errorlevel 1 goto failed

echo.
echo [5/5] Writing SHA256 checksum...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$hash = Get-FileHash -Algorithm SHA256 '%ZIP_PATH%'; ($hash.Hash.ToLower() + '  ' + [IO.Path]::GetFileName('%ZIP_PATH%')) | Set-Content -Encoding ASCII '%SHA_PATH%'"
if errorlevel 1 goto failed

echo.
echo Release package completed successfully.
echo Zip:    %ZIP_PATH%
echo SHA256: %SHA_PATH%
exit /b 0

:failed
echo.
echo Release package failed.
exit /b 1
