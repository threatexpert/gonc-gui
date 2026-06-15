@echo off
setlocal

cd /d "%~dp0"

echo [1/3] Setting Go proxy...
set "GOPROXY=https://goproxy.cn,direct"

echo [2/3] Installing frontend dependencies...
call npm install --prefix frontend
if errorlevel 1 goto failed

echo [3/3] Building gonc-gui...
wails build
if errorlevel 1 goto failed

echo.
echo Build completed successfully.
echo Output: %CD%\build\bin\gonc-gui.exe
exit /b 0

:failed
echo.
echo Build failed.
exit /b 1
