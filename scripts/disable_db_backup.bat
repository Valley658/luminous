@echo off
net session >nul 2>&1
if not "%errorlevel%"=="0" (
    echo Requesting administrator privileges...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)
echo Removing scheduled task: PastelliveDBBackup
schtasks /delete /tn "PastelliveDBBackup" /f
echo Done. Automatic DB backups are now OFF.
pause
