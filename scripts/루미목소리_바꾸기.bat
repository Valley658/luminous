@echo off
rem 루미 목소리만 바꾸기 (VoiceStudio에서 새 목소리를 만든 뒤 실행)
rem 한글 안내는 cmd echo 대신 PowerShell 로 출력한다 (일부 콘솔에서 echo 한글이 두 번씩 찍히는 문제)
if /I "%~1"=="__UTF8RUN__" goto :main
chcp 65001 >nul
net session >nul 2>&1
if not "%errorlevel%"=="0" (
    powershell -NoProfile -Command "[Console]::OutputEncoding=[Text.Encoding]::UTF8; Write-Host '관리자 권한을 요청하는 중...'; Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)
cmd /d /c ""%~f0" __UTF8RUN__"
exit /b

:main
title Luminous - Lumi voice change
for %%I in ("%~dp0..") do set "APPDIR=%%~fI"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0lumi_voice_setup.ps1" -AppDir "%APPDIR%" -VoiceOnly
if errorlevel 1 (
    powershell -NoProfile -Command "[Console]::OutputEncoding=[Text.Encoding]::UTF8; Write-Host ''; Write-Host '[중단] 위 메시지를 확인해 주세요. 문제를 해결한 뒤 이 파일을 다시 실행하면 이어서 진행돼요.' -ForegroundColor Yellow"
)
echo.
pause
