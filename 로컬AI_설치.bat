@echo off
if /I "%~1"=="__UTF8RUN__" goto :main

chcp 65001 >nul
cmd /d /c ""%~f0" __UTF8RUN__"
exit /b

:main
setlocal enabledelayedexpansion

echo ============================================
echo  루미 AI 채팅용 로컬 AI(Ollama) 설치
echo  - 이 서버(CPU 전용)에서 돌아갈 작은 한국어
echo    모델을 내려받음. 외부로 아무것도 전송하지
echo    않고, 이 컴퓨터 안에서만 동작함.
echo  - 한 번만 실행하면 됨. 이미 설치돼 있으면
echo    모델만 확인/다운로드하고 끝남.
echo ============================================
echo.

set "OLLAMA_MODEL=qwen2.5:3b-instruct-q4_K_M"
set "OLLAMA_EXE="

rem 어느 계정으로 Ollama를 실행하든(지금 로그인한 계정이든, 재부팅 후 자동시작될 때든)
rem 항상 같은 곳에서 모델을 찾도록 저장 위치를 고정해둔다. 이렇게 안 하면 지금 받은
rem 모델을 나중에 자동시작된 Ollama가 "없다"고 못 찾는 경우가 생길 수 있음.
set "OLLAMA_MODELS=%ProgramData%\Ollama\models"
if not exist "%OLLAMA_MODELS%" mkdir "%OLLAMA_MODELS%" >nul 2>&1

where ollama >nul 2>&1
if not errorlevel 1 (
    set "OLLAMA_EXE=ollama"
)

if not defined OLLAMA_EXE (
    if exist "%LocalAppData%\Programs\Ollama\ollama.exe" (
        set "OLLAMA_EXE=%LocalAppData%\Programs\Ollama\ollama.exe"
    )
)

if not defined OLLAMA_EXE (
    echo [1/3] Ollama가 설치돼 있지 않음 - winget으로 설치를 시도함...
    where winget >nul 2>&1
    if errorlevel 1 (
        echo.
        echo [오류] winget을 찾을 수 없음. 아래 페이지에서 직접 설치해줘:
        echo    https://ollama.com/download/windows
        echo 설치가 끝난 뒤 이 스크립트를 다시 실행해줘.
        pause
        exit /b 1
    )
    winget install --id Ollama.Ollama -e --accept-package-agreements --accept-source-agreements
    if errorlevel 1 (
        echo.
        echo [오류] winget 설치에 실패함. 아래 페이지에서 직접 설치해줘:
        echo    https://ollama.com/download/windows
        pause
        exit /b 1
    )
    echo    설치 완료. Ollama가 백그라운드에서 시작되도록 잠깐 대기...
    timeout /t 6 /nobreak >nul

    where ollama >nul 2>&1
    if not errorlevel 1 (
        set "OLLAMA_EXE=ollama"
    ) else if exist "%LocalAppData%\Programs\Ollama\ollama.exe" (
        set "OLLAMA_EXE=%LocalAppData%\Programs\Ollama\ollama.exe"
    ) else (
        echo.
        echo [안내] 설치는 됐지만 이 창에서 바로 인식이 안 될 수 있음.
        echo    새 명령 프롬프트 창을 열고 이 스크립트를 다시 실행해줘.
        pause
        exit /b 0
    )
) else (
    echo [1/3] Ollama가 이미 설치돼 있음.
)

rem 방문자도, 리도님도 Ollama가 뭔지 몰라도 되게끔 - 컴퓨터/서버를 껐다 켜도
rem Ollama가 항상 자동으로 같이 켜지도록 Windows 작업 스케줄러에 등록한다
rem (PastelliveApp이 등록된 방식과 동일: 로그인 없이도 SYSTEM 권한으로 시작됨).
set "OLLAMA_FULLPATH=%OLLAMA_EXE%"
if /I "%OLLAMA_EXE%"=="ollama" (
    for /f "delims=" %%P in ('where ollama 2^>nul') do set "OLLAMA_FULLPATH=%%P"
)
echo.
echo [서버 등록] 컴퓨터를 껐다 켜도 Ollama가 자동으로 실행되도록 등록 중...
setx /M OLLAMA_MODELS "%OLLAMA_MODELS%" >nul 2>&1
schtasks /create /tn "OllamaService" /tr "\"%OLLAMA_FULLPATH%\" serve" /sc onstart /ru SYSTEM /rl HIGHEST /f >nul 2>&1
if errorlevel 1 (
    echo    [안내] 자동 시작 등록은 관리자 권한이 필요해서 건너뜀 - 지금 당장
    echo    쓰는 데는 문제 없지만, 컴퓨터를 재시작하면 이 파일을 한 번 더
    echo    실행해줘야 할 수 있음. ^(관리자 권한으로 실행하면 등록됨^)
) else (
    echo    등록 완료 - 이제부터는 신경 안 써도 항상 자동으로 켜져 있음.
)

echo.
echo [서버 확인] Ollama가 지금 실행 중인지 확인 중...
tasklist /FI "IMAGENAME eq ollama.exe" 2>NUL | find /I /N "ollama.exe">NUL
if "%ERRORLEVEL%"=="1" (
    echo    지금은 꺼져 있어서 시작함...
    schtasks /run /tn "OllamaService" >nul 2>&1
    if errorlevel 1 start "" "%OLLAMA_FULLPATH%" serve
    timeout /t 5 /nobreak >nul
) else (
    echo    이미 실행 중.
)

echo.
echo [2/4] 한국어 대화용 모델(%OLLAMA_MODEL%) 다운로드 중...
echo    (용량이 몇 GB 정도라 처음엔 시간이 좀 걸릴 수 있음)
"%OLLAMA_EXE%" pull %OLLAMA_MODEL%
if errorlevel 1 (
    echo.
    echo [오류] 모델 다운로드에 실패함. 인터넷 연결을 확인하고 다시 실행해줘.
    pause
    exit /b 1
)

echo.
echo [3/4] 설치된 모델 확인:
"%OLLAMA_EXE%" list

rem 모델을 미리 한 번 메모리에 불러와둔다(예열). 이걸 안 하면 사이트에서 첫
rem 방문자가 루미에게 처음 질문할 때 모델을 불러오는 시간까지 같이 기다려야
rem 해서 "생각을 너무 오래 한다"고 느껴질 수 있음 - 여기서 미리 기다려주면
rem 방문자는 항상 빠른 답을 받게 됨.
echo.
echo [4/4] 모델을 미리 메모리에 불러오는 중 (예열)...
echo    처음 한 번은 시간이 좀 걸릴 수 있음 - 정상임.
"%OLLAMA_EXE%" run %OLLAMA_MODEL% "안녕" >nul 2>&1
if errorlevel 1 (
    echo    [안내] 예열에는 실패했지만 치명적인 문제는 아님 - 실제 사용 시
    echo    첫 질문만 조금 오래 걸리고 그 다음부터는 빨라짐.
) else (
    echo    예열 완료 - 이제 방문자 질문에 바로 빠르게 답할 수 있음.
)

echo.
echo ============================================
echo  완료! Ollama는 이제 컴퓨터가 켜질 때마다 자동으로
echo  같이 실행되며 http://127.0.0.1:11434 에서 대기함.
echo  (방문자도, 리도님도 Ollama를 따로 신경 쓸 필요 없음)
echo.
echo  이제 서버재시작.bat 을 실행해서 PastelliveApp을
echo  재시작하면 루미가 AI로 답변할 수 있게 됨.
echo.
echo  (모델을 바꾸고 싶으면 .env 에 OLLAMA_MODEL=... 을
echo   추가하고 위 pull 명령을 그 모델 이름으로 다시 실행)
echo ============================================
pause
