@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion

rem 루미가 사진을 보고 스텔라이브 멤버를 알아맞히는 기능을 켜기 위한 설치
rem 스크립트. member-id-service/에 있는 아주 작은 로컬 전용 서비스를
rem 백그라운드로 등록한다(watchdog.exe가 감시 - go-server\bin\watchdog.exe
rem 참고). TTS 설치 때와 달리 onnxruntime/pillow/numpy만 쓰는 가벼운
rem 조합이라 버전 충돌 걱정이 훨씬 적음.

set "ROOT=%~dp0"
set "SVCDIR=%ROOT%services\member-id-service"
set "VENVDIR=%ProgramData%\LuminousMemberID\venv"
set "VENV_PY=%VENVDIR%\Scripts\python.exe"
set "RUNNER=%SVCDIR%\bin\_run_production.bat"
set "WATCHDOG_EXE=%ROOT%go-server\bin\watchdog.exe"

echo ============================================
echo  루미 사진 인식 기능 설치
echo ============================================
echo.

rem ---- [1/4] 파이썬 확인 -------------------------------------------------
echo [1/4] 파이썬 확인 중...
set "PY_LAUNCHER="
where py >nul 2>&1
if not errorlevel 1 set "PY_LAUNCHER=py -3"
if not defined PY_LAUNCHER (
    where python >nul 2>&1
    if not errorlevel 1 set "PY_LAUNCHER=python"
)
if not defined PY_LAUNCHER (
    echo [오류] 파이썬을 찾을 수 없음. https://www.python.org/downloads/ 에서
    echo    "Add python.exe to PATH" 체크하고 설치한 뒤 다시 실행해줘.
    pause
    exit /b 1
)
echo    사용할 파이썬: %PY_LAUNCHER%

rem ---- [2/4] 가상환경 + 패키지 설치 --------------------------------------
echo.
echo [2/4] 가상환경 준비 및 패키지 설치 중... (처음엔 몇 분 걸릴 수 있음)
if not exist "%VENVDIR%\Scripts\python.exe" (
    %PY_LAUNCHER% -m venv "%VENVDIR%"
    if errorlevel 1 (
        echo [오류] 가상환경 생성 실패.
        pause
        exit /b 1
    )
)
"%VENV_PY%" -m pip install --upgrade pip >nul 2>&1
"%VENV_PY%" -m pip install -r "%SVCDIR%\requirements.txt"
if errorlevel 1 (
    echo [오류] 패키지 설치 실패. 인터넷 연결을 확인하고 다시 실행해줘.
    pause
    exit /b 1
)

rem ---- [3/4] 설치 확인 -----------------------------------------------------
echo.
echo [3/4] 설치 확인 중...
"%VENV_PY%" -c "import onnxruntime, PIL, numpy; print('OK')" > "%TEMP%\memberid_check.log" 2>&1
if errorlevel 1 (
    echo [오류] 설치 확인 실패 - 로그:
    type "%TEMP%\memberid_check.log"
    pause
    exit /b 1
)
echo    OK.
del "%TEMP%\memberid_check.log" >nul 2>&1

rem ---- [4/4] 서비스 등록 --------------------------------------------------
echo.
echo [4/4] 백그라운드 서비스로 등록 중...
if not exist "%WATCHDOG_EXE%" (
    echo [오류] %WATCHDOG_EXE% 를 찾을 수 없음 - 먼저 빌드해줘:
    echo    cd go-server ^&^& go build -o bin\watchdog.exe .\cmd\watchdog
    pause
    exit /b 1
)

(
    echo @echo off
    echo set MEMBER_ID_SERVICE_PORT=8098
    echo set "MEMBERID_VENV_PY=%VENV_PY%"
    echo "%%~dp0..\..\..\go-server\bin\watchdog.exe" -exe "%%~dp0run_id.bat" -log "%%~dp0..\logs\service.log" -health "http://127.0.0.1:8098/health" -health-grace 120s
) > "%RUNNER%"
echo    실행 스크립트 작성 완료: %RUNNER%

schtasks /end /tn "LumiMemberIDService" >nul 2>&1
schtasks /delete /tn "LumiMemberIDService" /f >nul 2>&1
schtasks /create /tn "LumiMemberIDService" /tr "\"%RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
if errorlevel 1 (
    echo    [안내] 자동 시작 등록은 관리자 권한이 필요해서 건너뜀 - 지금
    echo    당장 쓰는 데는 문제 없지만, 컴퓨터를 재시작하면 이 파일을
    echo    관리자 권한으로 한 번 더 실행해줘야 자동 등록됨.
) else (
    schtasks /run /tn "LumiMemberIDService" >nul 2>&1
    echo    등록 및 시작 완료.
)

echo.
echo ============================================
echo  완료! 확인 방법 (명령 프롬프트에서):
echo    schtasks /query /tn LumiMemberIDService /fo list
echo    curl http://127.0.0.1:8098/health
echo  로그: %SVCDIR%\logs\service.log
echo  (최초 실행 시 인식 모델(약 90MB)을 한 번 받는데 몇 분 걸릴 수 있음 -
echo   그 다음부터는 바로 뜸)
echo ============================================
echo.
pause
