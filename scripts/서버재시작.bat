@echo off
if /I "%~1"=="__UTF8RUN__" goto :main

chcp 65001 >nul
cmd /d /c ""%~f0" __UTF8RUN__"
exit /b

:main
setlocal enabledelayedexpansion
rem [2026-09-18: scripts\ 폴더로 옮겨지면서 %~dp0이 더 이상 프로젝트 루트가
rem 아니게 됐다 - 한 단계 위(..)로 올라가야 실제 루트. APPDIR은 아래에서
rem "이 폴더 소속 프로세스만 종료"하는 안전장치라 잘못되면 재시작이 조용히
rem 실패할 수 있어 특히 중요함.]
for %%I in ("%~dp0..") do set "APPDIR=%%~fI"
if "%APPDIR:~-1%"=="\" set "APPDIR=%APPDIR:~0,-1%"

echo ============================================
echo  PastelliveApp (Go 웹서버) 재시작
echo  - 템플릿(templates\*.html)이나 정적 파일을 고친 뒤
echo    반영하려고 할 때 이 파일만 실행하면 됨.
echo  - nginx / Meilisearch / 디스코드봇 / 이미지서비스 등
echo    나머지 서비스는 건드리지 않음 (START_SERVICE.bat /
echo    STOP_SERVICE.bat 은 전체 서비스를 다룸).
echo ============================================
echo.

schtasks /query /tn "PastelliveApp" >nul 2>&1
if errorlevel 1 (
    echo [오류] "PastelliveApp" 작업이 아직 등록돼 있지 않음.
    echo   deploy\windows\install_all.bat 를 열어서 14번을 먼저 실행해줘.
    pause
    exit /b 1
)

echo [1/3] PastelliveApp 중지 중...
schtasks /end /tn "PastelliveApp" >nul 2>&1
powershell -NoProfile -Command ^
    "$appdir = '%APPDIR%';" ^
    "$c = Get-NetTCPConnection -LocalPort 8081 -State Listen -ErrorAction SilentlyContinue;" ^
    "$pids = $c | Select-Object -ExpandProperty OwningProcess -Unique;" ^
    "foreach ($procId in $pids) {" ^
    "  $proc = Get-CimInstance Win32_Process -Filter \"ProcessId=$procId\" -ErrorAction SilentlyContinue;" ^
    "  if ($proc -and $proc.CommandLine -and $proc.CommandLine.ToLower().Contains($appdir.ToLower())) {" ^
    "    Write-Host ('  강제 종료: PID ' + $procId + ' - ' + $proc.CommandLine);" ^
    "    Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue" ^
    "  } elseif ($proc) {" ^
    "    Write-Host ('  [건너뜀] PID ' + $procId + ' 은 포트 8081에 있지만 이 폴더 소속이 아님: ' + $proc.CommandLine)" ^
    "  }" ^
    "}"
echo   완료.

echo [2/3] 잠깐 대기...
timeout /t 2 /nobreak >nul

echo [3/3] PastelliveApp 다시 시작...
schtasks /run /tn "PastelliveApp"
echo   시작 신호를 보냈어.

echo.
echo 몇 초 후 아래 로그에서 정상 기동됐는지 확인해줘:
echo   logs\service.log
echo.
echo (nginx/Meilisearch/디스코드봇 등은 그대로 유지됨 - 손대지 않았음)
pause
