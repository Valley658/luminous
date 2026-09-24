#Requires -RunAsAdministrator
# ============================================================================
# 로그에 실제 원인이 찍혀 있었다:
#   listen tcp 127.0.0.1:8081: bind: Only one usage of each socket address ...
# 즉 서버가 죽는 이유는 작업 스케줄러 트리거나 시간 제한이 아니라,
# "새 서버가 뜰 때 8081 포트를 이미 다른 프로세스가 쓰고 있어서" 였다.
# 지난번 트리거 재등록(fix_task_trigger.ps1)이 예전 watchdog/server 프로세스를
# 종료하지 못하고 남겨둔 채로 새 걸 또 띄우면서 포트 충돌이 시작된 것으로 보인다.
#
# 이 스크립트는:
#   1) PastelliveApp 작업을 끄고
#   2) watchdog.exe / pastellive-server.exe 남아있는 프로세스를 전부 강제 종료하고
#   3) 8081 포트를 실제로 잡고 있는 프로세스가 있으면 그것도 강제 종료한 뒤
#   4) 작업을 다시 시작한다.
# ============================================================================
$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}

$TaskName = 'PastelliveApp'

Write-Host "=============================================="
Write-Host " 포트 충돌(8081) 정리 + $TaskName 재시작"
Write-Host "=============================================="

Write-Host "`n-- 1) 작업 중지 --"
schtasks /end /tn $TaskName *> $null
Start-Sleep -Seconds 1

Write-Host "`n-- 2) 남아있는 watchdog.exe / pastellive-server.exe 프로세스 --"
$procs = Get-Process -Name 'watchdog', 'pastellive-server' -ErrorAction SilentlyContinue
if ($procs) {
    foreach ($p in $procs) {
        Write-Host "  종료: $($p.ProcessName) (PID=$($p.Id), 시작시각=$($p.StartTime))"
        Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    }
} else {
    Write-Host "  (남아있는 프로세스 없음)"
}
Start-Sleep -Seconds 2

Write-Host "`n-- 3) 8081 포트를 실제로 점유 중인 프로세스 확인 --"
$conns = Get-NetTCPConnection -LocalPort 8081 -ErrorAction SilentlyContinue
if ($conns) {
    $pids = $conns | Select-Object -ExpandProperty OwningProcess -Unique
    foreach ($ownerId in $pids) {
        try {
            $owner = Get-Process -Id $ownerId -ErrorAction Stop
            Write-Host "  8081 점유 중: $($owner.ProcessName) (PID=$ownerId) - 강제 종료"
            Stop-Process -Id $ownerId -Force -ErrorAction SilentlyContinue
        } catch {
            Write-Host "  PID $ownerId 정보 조회 실패 (이미 종료됐을 수 있음)"
        }
    }
} else {
    Write-Host "  (8081 포트를 점유한 프로세스 없음 - 정상)"
}
Start-Sleep -Seconds 2

Write-Host "`n-- 4) 재확인: 8081 포트 --"
$conns2 = Get-NetTCPConnection -LocalPort 8081 -ErrorAction SilentlyContinue
if ($conns2) {
    Write-Host "  (!!) 아직도 8081을 점유한 프로세스가 있습니다:"
    $conns2 | ForEach-Object { Write-Host "    PID=$($_.OwningProcess) State=$($_.State)" }
} else {
    Write-Host "  깨끗합니다 - 아무도 8081을 점유하고 있지 않음"
}

Write-Host "`n-- 5) 작업 다시 시작 --"
schtasks /run /tn $TaskName *> $null
Start-Sleep -Seconds 3
$check = Get-NetTCPConnection -LocalPort 8081 -ErrorAction SilentlyContinue
if ($check) {
    Write-Host "  서버가 8081 포트에 정상적으로 바인딩됐습니다."
} else {
    Write-Host "  (!!) 아직 8081에 바인딩된 프로세스가 안 보입니다 - 몇 초 더 걸릴 수 있으니 logs\service.log 를 확인해주세요."
}

Write-Host ""
Write-Host "이제 logs\service.log 를 지켜봐주세요 - 'listen tcp 127.0.0.1:8081: bind' 에러 없이"
Write-Host "'pastellive-go 서버 시작' 로그만 한 번 찍히고 재시작 없이 계속 떠 있어야 정상입니다."
Read-Host "Enter 키를 누르면 창이 닫힙니다"
