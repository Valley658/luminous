@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "GOBINPATH=%USERPROFILE%\go\bin"
echo %PATH% | find /i "%GOBINPATH%" >nul
if errorlevel 1 set "PATH=%PATH%;%GOBINPATH%"

where air >nul 2>nul
if errorlevel 1 (
    echo [Air is not installed yet. Installing it once...]
    go install github.com/air-verse/air@latest
    if errorlevel 1 (
        echo [Error] Failed to install Air. Make sure Go is installed and on PATH.
        pause
        exit /b 1
    )
    echo [Install complete]
)

echo ==============================================================
echo  Local dev live-reload (Air)
echo  http://127.0.0.1:8082   (unrelated to the production port 8081)
echo  Saving a .go file or templates\*.html auto-rebuilds + restarts.
echo  Press Ctrl+C in this window to stop.
echo ==============================================================
echo.

air

echo.
echo Stopped.
pause
endlocal
