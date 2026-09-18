@echo off
setlocal enabledelayedexpansion
rem [2026-09-18: scripts\ 폴더로 옮겨지면서 %~dp0이 더 이상 프로젝트 루트가
rem 아니게 됐다 - air는 현재 디렉터리에서 .air.toml을 찾으므로, 루트로
rem 옮겨가지 않으면 설정 파일을 못 찾거나(또는 엉뚱한 폴더 기준으로 동작).]
cd /d "%~dp0.."

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
