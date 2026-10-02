@echo off
chcp 65001 >nul
rem frontend\js\*.js (원본) -> static\js\*.js (압축/minify, 사이트와 GitHub에 공개되는 파일)
cd /d "%~dp0..\tools"
where node >nul 2>&1
if errorlevel 1 (
    echo [오류] Node.js가 없어요. https://nodejs.org 에서 설치해 주세요.
    pause
    exit /b 1
)
if not exist "node_modules\terser" call npm ci --silent
node build-js.js
if errorlevel 1 (
    echo [오류] JS 빌드 실패. 위 메시지를 확인해 주세요. 사이트의 JS는 바뀌지 않았어요.
    pause
    exit /b 1
)
echo.
echo 완료! 사이트에서 Ctrl+F5 로 새로고침하면 바로 적용돼요. (서버 재시작 필요 없음)
if /I not "%~1"=="nopause" pause
