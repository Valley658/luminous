#Requires -RunAsAdministrator
# ============================================================================
# 실행 시간 제한(ExecutionTimeLimit)은 이미 무제한(PT0S)으로 확인됐는데도
# PastelliveApp이 정확히 6분마다 재시작되고 있다면, 원인은 다른 곳에 있다.
# 이 스크립트는 트리거(Repetition 설정)와 "이미 실행 중이면 어떻게 할지"
# 정책(MultipleInstances)을 그대로 출력한다 - 트리거에 "6분마다 반복" 같은
# 설정이 걸려 있고 정책이 "기존 걸 멈추고 새로 시작"이면, 작업 스케줄러가
# 멀쩡히 도는 프로세스를 6분마다 직접 끊어버리는 것으로 설명이 된다.
# ============================================================================
$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}

$TaskName = 'PastelliveApp'
$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if (-not $task) {
    Write-Host "[오류] '$TaskName' 작업을 찾을 수 없습니다."
    Read-Host "Enter 키를 누르면 창이 닫힙니다"
    exit 1
}

Write-Host "=============================================="
Write-Host " $TaskName 트리거/정책 진단"
Write-Host "=============================================="

Write-Host "`n-- MultipleInstances 정책 (0=Parallel, 1=Queue, 2=IgnoreNew, 3=StopExisting) --"
Write-Host $task.Settings.MultipleInstancesPolicy

Write-Host "`n-- ExecutionTimeLimit --"
Write-Host $task.Settings.ExecutionTimeLimit

Write-Host "`n-- RestartCount / RestartInterval (작업 자체 실패 시 재시도 설정) --"
Write-Host "RestartCount: $($task.Settings.RestartCount)"
Write-Host "RestartInterval: $($task.Settings.RestartInterval)"

Write-Host "`n-- Triggers --"
$i = 0
foreach ($t in $task.Triggers) {
    $i++
    Write-Host "[$i] Type=$($t.CimClass.CimClassName)"
    Write-Host "    Enabled=$($t.Enabled)  StartBoundary=$($t.StartBoundary)  EndBoundary=$($t.EndBoundary)"
    if ($t.Repetition) {
        Write-Host "    Repetition.Interval=$($t.Repetition.Interval)  Repetition.Duration=$($t.Repetition.Duration)  StopAtDurationEnd=$($t.Repetition.StopAtDurationEnd)"
    }
}

Write-Host "`n-- 실행 계정/권한 --"
Write-Host "RunLevel: $($task.Principal.RunLevel)  UserId: $($task.Principal.UserId)  LogonType: $($task.Principal.LogonType)"

Write-Host ""
Write-Host "위 내용을 그대로 복사해서 보여주세요."
Read-Host "Enter 키를 누르면 창이 닫힙니다"
