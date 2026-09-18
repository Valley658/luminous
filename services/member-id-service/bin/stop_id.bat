@echo off
rem LumiMemberIDService를 확실하게 완전히 멈춘다. schtasks /end는 watchdog.exe
rem 자체를 윈도우가 밖에서 강제 종료하는 것이라 watchdog 안의 정리 코드
rem (taskkill /T로 자식 프로세스 트리까지 같이 죽이는 부분)가 못 도니, 혹시
rem 몰라 남아있는 python.exe도 명령줄로 찾아서 추가로 끝낸다.
schtasks /end /tn "LumiMemberIDService" >nul 2>&1
powershell -NoProfile -Command "Get-CimInstance Win32_Process | Where-Object { $_.Name -eq 'python.exe' -and $_.CommandLine -like '*member-id-service*' } | ForEach-Object { Write-Host ('  종료: PID ' + $_.ProcessId); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
echo LumiMemberIDService 완전히 중지함.
