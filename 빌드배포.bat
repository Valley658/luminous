@echo off
if /I "%~1"=="__UTF8RUN__" goto :main

chcp 65001 >nul
cmd /d /c ""%~f0" __UTF8RUN__"
exit /b

:main
setlocal enabledelayedexpansion
for %%I in ("%~dp0") do set "APPDIR=%%~fI"
if "%APPDIR:~-1%"=="\" set "APPDIR=%APPDIR:~0,-1%"
set "GODIR=%APPDIR%\go-server"
set "GOBIN=%GODIR%\bin"

echo ============================================
echo  Go 서버 새 버전 빌드 + 적용
echo  - go-server 소스 코드(핸들러, 라우트 등)를 고친 뒤
echo    실제로 반영하려면 이 파일을 실행해야 함.
echo  - 템플릿/CSS/JS만 고쳤을 땐 이 파일이 필요 없고
echo    서버재시작.bat만으로 충분함 (Go 코드를 고쳤을
echo    때만 이 파일이 필요함).
echo ============================================
echo.

where go >nul 2>&1
if errorlevel 1 (
    echo [오류] go 명령을 찾을 수 없음. Go가 설치돼 있는지, PATH에
    echo   잡혀 있는지 확인해줘. ^(https://go.dev/dl/^)
    pause
    exit /b 1
)

echo [1/4] 빌드 중... ^(go-server\bin\pastellive-server.new.exe^)
pushd "%GODIR%"
go build -o "bin\pastellive-server.new.exe" .\cmd\server
set "BUILD_ERR=%ERRORLEVEL%"
popd
if not "%BUILD_ERR%"=="0" (
    echo.
    echo [오류] 빌드 실패. 위 에러 메시지를 확인해줘.
    pause
    exit /b 1
)
echo   빌드 완료.

if not exist "%GOBIN%\pastellive-server.new.exe" (
    echo [오류] 빌드는 됐다는데 %GOBIN%\pastellive-server.new.exe 가 없음.
    pause
    exit /b 1
)

echo.
echo [2/4] PastelliveApp 중지 중...
schtasks /end /tn "PastelliveApp" >nul 2>&1
powershell -NoProfile -Command ^
    "$c = Get-NetTCPConnection -LocalPort 8081 -State Listen -ErrorAction SilentlyContinue;" ^
    "foreach ($p in ($c | Select-Object -ExpandProperty OwningProcess -Unique)) { Stop-Process -Id $p -Force -ErrorAction SilentlyContinue }"
timeout /t 2 /nobreak >nul

echo [3/4] 기존 exe 백업 후 새 exe로 교체 중...
if exist "%GOBIN%\pastellive-server.old.exe" del /f /q "%GOBIN%\pastellive-server.old.exe"
if exist "%GOBIN%\pastellive-server.exe" move /y "%GOBIN%\pastellive-server.exe" "%GOBIN%\pastellive-server.old.exe" >nul
move /y "%GOBIN%\pastellive-server.new.exe" "%GOBIN%\pastellive-server.exe" >nul
if not exist "%GOBIN%\pastellive-server.exe" (
    echo [오류] 교체 실패 - pastellive-server.exe 가 없음.
    pause
    exit /b 1
)

echo [4/4] PastelliveApp 다시 시작...
schtasks /run /tn "PastelliveApp"

echo.
echo ============================================
echo  완료! 몇 초 후 아래 로그에서 정상 기동됐는지 확인해줘:
echo    logs\service.log
echo  (문제 있으면 pastellive-server.old.exe 로 되돌릴 수 있음)
echo ============================================
pause
