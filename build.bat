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

echo [4/4] Copying Wintun runtime...
if not exist "%CD%\wintun\bin\amd64\wintun.dll" (
  echo Missing required file: %CD%\wintun\bin\amd64\wintun.dll
  goto failed
)
copy /y "%CD%\wintun\bin\amd64\wintun.dll" "%CD%\build\bin\" >nul
if errorlevel 1 goto failed

echo.
echo Build completed successfully.
echo Output: %CD%\build\bin\gonc-gui.exe
exit /b 0

:failed
echo.
echo Build failed.
exit /b 1
