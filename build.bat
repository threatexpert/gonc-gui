@echo off
setlocal

cd /d "%~dp0"

echo [1/4] Setting Go proxy...
set "GOPROXY=https://goproxy.cn,direct"

echo [2/4] Installing frontend dependencies...
call npm install --prefix frontend
if errorlevel 1 goto failed

echo [3/4] Building gonc-gui...
wails build
if errorlevel 1 goto failed

echo [4/4] Syncing bundled files...
if not exist "%CD%\bundled" (
  echo Missing bundled directory: %CD%\bundled
  goto failed
)
if not exist "%CD%\build\bin" mkdir "%CD%\build\bin"
xcopy "%CD%\bundled" "%CD%\build\bin\bundled\" /e /i /y >nul
if errorlevel 1 goto failed

echo.
echo Build completed successfully.
echo Output: %CD%\build\bin\gonc-gui.exe
exit /b 0

:failed
echo.
echo Build failed.
exit /b 1
