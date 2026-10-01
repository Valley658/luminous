@echo off
if /I "%~1"=="__UTF8RUN__" goto :main
chcp 65001 >nul
net session >nul 2>&1
if not "%errorlevel%"=="0" (
    echo 관리자 권한을 요청하는 중...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)
cmd /d /c ""%~f0" __UTF8RUN__"
exit /b

:main
title 루미 목소리 설치 + 루미너스 업데이트 적용
for %%I in ("%~dp0..") do set "APPDIR=%%~fI"
echo ==============================================================
echo  루미 목소리 한 번에 설치 + 오늘 바뀐 내용 적용
echo   1. VoiceStudio 설치/실행 (없으면 자동으로 받아서 설치)
echo   2. 루미 목소리 고르기 + 시험으로 들어 보기
echo   3. .env 에 목소리 저장
echo   4. 예전 디스코드 봇 작업 정리
echo   5. nginx 다시 읽기 (팬 갤러리 사진 캐시)
echo   6. 빌드배포 + 고정 멘트 자동 녹음 확인
echo ==============================================================
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0lumi_voice_setup.ps1" -AppDir "%APPDIR%"
if errorlevel 1 (
    echo.
    echo [중단] 위 메시지를 확인해 주세요. 문제를 해결한 뒤 이 파일을 다시 실행하면 이어서 진행돼요.
)
echo.
pause
