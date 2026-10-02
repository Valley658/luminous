@echo off
if /I "%~1"=="__RUN__" goto :main
net session >nul 2>&1
if not "%errorlevel%"=="0" (
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -ArgumentList '__RUN__' -Verb RunAs"
    exit /b
)
:main
chcp 65001 >nul
title RDP client fix
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0rdp_client_fix.ps1"
echo.
pause
