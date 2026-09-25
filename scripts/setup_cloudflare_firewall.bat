@echo off
net session >nul 2>&1
if not "%errorlevel%"=="0" (
    echo Requesting administrator privileges...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)
echo Running in DRY RUN mode first (no changes made).
echo To actually apply, run: powershell -ExecutionPolicy Bypass -File "%~dp0setup_cloudflare_firewall.ps1" -Apply
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0setup_cloudflare_firewall.ps1"
pause
