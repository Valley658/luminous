#Requires -RunAsAdministrator
# ============================================================================
# 실행 시간 제한(ExecutionTimeLimit)은 이미 무제한인데도 PastelliveApp이
# 정확히 6분마다 재시작되는 문제 - 가장 유력한 남은 원인은 트리거 자체에
# "몇 분마다 반복 실행" 설정이 걸려 있고, "이미 실행 중이면 어떻게 할지"
# 정책이 기존 걸 멈추고 새로 시작하는 쪽으로 되어 있는 경우다. 이러면
# 작업 스케줄러가 멀쩡히 잘 돌고 있는 서버를 6분마다 직접 끊어버린다.
#
# 진단 결과를 기다리지 않고 바로 고친다 - 트리거를 "그냥 켤 때 한 번
# 시작"(반복 없음)으로 깨끗하게 다시 만들고, 동시에 여러 개가 겹쳐 뜨지
# 않도록 "이미 실행 중이면 새로 시작하지 않음(IgnoreNew)"으로 고정한다.
# 기존 Action(watchdog.exe 실행 경로)은 그대로 유지하고 트리거/정책만 바꾼다.
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
Write-Host " $TaskName 트리거 재설정"
Write-Host "=============================================="

Write-Host "`n-- 고치기 전 상태 --"
Write-Host "MultipleInstancesPolicy: $($task.Settings.MultipleInstancesPolicy)"
$i = 0
foreach ($t in $task.Triggers) {
    $i++
    Write-Host "[$i] $($t.CimClass.CimClassName) Enabled=$($t.Enabled)"
    if ($t.Repetition -and $t.Repetition.Interval) {
        Write-Host "    (!!) Repetition.Interval=$($t.Repetition.Interval) Duration=$($t.Repetition.Duration)"
    }
}

$action = $task.Actions[0]
$newTrigger = New-ScheduledTaskTrigger -AtStartup
$newSettings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -StartWhenAvailable -MultipleInstances IgnoreNew
$newSettings.ExecutionTimeLimit = 'PT0S'

$principal = New-ScheduledTaskPrincipal -UserId $task.Principal.UserId -LogonType $task.Principal.LogonType -RunLevel Highest

Write-Host "`n다시 등록하는 중..."
Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
try {
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $newTrigger `
        -Principal $principal -Settings $newSettings -ErrorAction Stop | Out-Null
    Write-Host "등록 완료."
} catch {
    Write-Host "[오류] 재등록 실패: $($_.Exception.Message)"
    Read-Host "Enter 키를 누르면 창이 닫힙니다"
    exit 1
}

Write-Host "`n-- 고친 후 상태 --"
$newTask = Get-ScheduledTask -TaskName $TaskName
Write-Host "MultipleInstancesPolicy: $($newTask.Settings.MultipleInstancesPolicy)"
foreach ($t in $newTask.Triggers) {
    Write-Host "$($t.CimClass.CimClassName) Enabled=$($t.Enabled) Repetition.Interval=$($t.Repetition.Interval)"
}

Write-Host "`n서버를 한 번 재시작합니다..."
schtasks /end /tn $TaskName *> $null
Start-Sleep -Seconds 2
schtasks /run /tn $TaskName *> $null
Write-Host "재시작 완료."
Write-Host ""
Write-Host "이제 logs\service.log 에서 [watchdog] 실행: 줄 간격이 6분보다 훨씬 길어지는지 지켜봐주세요."
Read-Host "Enter 키를 누르면 창이 닫힙니다"
