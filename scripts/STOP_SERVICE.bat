@echo off
setlocal enabledelayedexpansion
rem [2026-09-18: 이 파일이 scripts\ 폴더로 옮겨지면서 %~dp0(이 스크립트 자신의
rem 위치)가 더 이상 프로젝트 루트가 아니게 됐다 - 한 단계 위(..)로 올라가야
rem 실제 루트가 나온다. 이 값(APPDIR)은 아래에서 "이 폴더 소속 프로세스만
rem 종료"하는 안전장치로 쓰이므로, 잘못되면 정작 꺼야 할 서버 프로세스를 못
rem 찾아서 그냥 지나쳐버리는 조용한 실패가 생길 수 있어 특히 중요함.]
for %%I in ("%~dp0..") do set "APPDIR=%%~fI"
if "%APPDIR:~-1%"=="\" set "APPDIR=%APPDIR:~0,-1%"

echo Stopping ALL pastellive-related services
echo   (only processes whose command line points at: %APPDIR%,
echo    plus nginx/Meilisearch - see comments in this file.
echo    Cloudflared tunnel is left running on purpose.)

echo [1/6] Stopping PastelliveApp (port 8081)...
schtasks /end /tn "PastelliveApp" >nul 2>&1
powershell -NoProfile -Command ^
    "$appdir = '%APPDIR%';" ^
    "$c = Get-NetTCPConnection -LocalPort 8081 -State Listen -ErrorAction SilentlyContinue;" ^
    "$pids = $c | Select-Object -ExpandProperty OwningProcess -Unique;" ^
    "foreach ($procId in $pids) {" ^
    "  $proc = Get-CimInstance Win32_Process -Filter \"ProcessId=$procId\" -ErrorAction SilentlyContinue;" ^
    "  if ($proc -and $proc.CommandLine -and $proc.CommandLine.ToLower().Contains($appdir.ToLower())) {" ^
    "    Write-Host ('  Stopping PID ' + $procId + ' - ' + $proc.CommandLine);" ^
    "    Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue" ^
    "  } elseif ($proc) {" ^
    "    Write-Host ('  [Skip] PID ' + $procId + ' is on port 8080 but its command line does not mention this pastellive folder - leaving it alone: ' + $proc.CommandLine)" ^
    "  }" ^
    "}"
echo   Done.

echo [2/5] Stopping PastelliveDiscordBot (Go - discord-bot.exe)...
schtasks /end /tn "PastelliveDiscordBot" >nul 2>&1
taskkill /f /im discord-bot.exe >nul 2>&1
echo   Done.

echo [3/5] Stopping nginx (and its 2-minute watchdog task, or it would just restart nginx)...
schtasks /end /tn "NginxWatchdog" >nul 2>&1
schtasks /end /tn "NginxStartup" >nul 2>&1
if exist "C:\nginx\nginx.exe" (
    "C:\nginx\nginx.exe" -p "C:\nginx" -s stop >nul 2>&1
)
taskkill /f /im nginx.exe >nul 2>&1
echo   Done.

echo [4/5] Stopping Meilisearch...
schtasks /end /tn "Meilisearch" >nul 2>&1
taskkill /f /im meilisearch.exe >nul 2>&1
echo   Done.

echo [5/5] Stopping C Image Service (port 8091)...
schtasks /end /tn "PastelliveCImageService" >nul 2>&1
powershell -NoProfile -Command ^
    "$c = Get-NetTCPConnection -LocalPort 8091 -State Listen -ErrorAction SilentlyContinue;" ^
    "foreach ($procId in ($c | Select-Object -ExpandProperty OwningProcess -Unique)) {" ^
    "  Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue" ^
    "}"
schtasks /end /tn "PastelliveJavaImageService" >nul 2>&1
echo   Done.

echo All pastellive-related services are stopped now.
echo   (Cloudflared tunnel was left running on purpose - stop it
echo    manually with "net stop cloudflared" if you really need to.)
echo   - To start them all again as-is:   START_SERVICE.bat
echo   - To fix something and redeploy the latest code:
echo       deploy\windows\install_all.bat
pause
