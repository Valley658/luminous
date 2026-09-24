@echo off
net session >nul 2>&1
if not "%errorlevel%"=="0" (
    echo Requesting administrator privileges...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)
echo Removing scheduled task: PastelliveAutoDeploy
schtasks /delete /tn "PastelliveAutoDeploy" /f
echo Done. Auto-apply is now OFF - use install_all.bat 15/20 to apply new builds manually.
pause
