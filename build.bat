@echo off
setlocal

cd /d "%~dp0"

set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=windows/amd64"
if /i "%TARGET%"=="amd64" set "TARGET=windows/amd64"
if /i "%TARGET%"=="arm64" set "TARGET=windows/arm64"
set "SKIP_WINTUN_COPY="
if /i "%~2"=="--skip-wintun-copy" set "SKIP_WINTUN_COPY=1"

if /i "%TARGET%"=="windows/amd64" (
  set "WINTUN_ARCH=amd64"
) else if /i "%TARGET%"=="windows/arm64" (
  set "WINTUN_ARCH=arm64"
) else (
  echo Unsupported build target: %TARGET%
  goto failed
)

echo [1/4] Setting Go proxy...
set "GOPROXY=https://goproxy.cn,direct"

echo [2/4] Installing frontend dependencies...
call npm install --prefix frontend
if errorlevel 1 goto failed

echo [3/4] Building gonc-gui...
wails build -platform "%TARGET%"
if errorlevel 1 goto failed

echo [4/4] Copying Wintun runtime...
if not exist "%CD%\wintun\bin\%WINTUN_ARCH%\wintun.dll" (
  echo Missing required file: %CD%\wintun\bin\%WINTUN_ARCH%\wintun.dll
  goto failed
)
if "%SKIP_WINTUN_COPY%"=="" (
  copy /y "%CD%\wintun\bin\%WINTUN_ARCH%\wintun.dll" "%CD%\build\bin\" >nul
  if errorlevel 1 goto failed
) else (
  echo Skipped copying Wintun runtime to build\bin.
)

echo.
echo Build completed successfully.
echo Target: %TARGET%
echo Output: %CD%\build\bin\gonc-gui.exe
exit /b 0

:failed
echo.
echo Build failed.
exit /b 1
