#Requires -RunAsAdministrator
$ErrorActionPreference = 'Continue'
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {}

$AppDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$BackupScript = Join-Path $AppDir 'scripts\backup_db.ps1'
$TaskName = 'PastelliveDBBackup'

Write-Host "=============================================="
Write-Host " DB 자동 백업 작업 등록: $TaskName"
Write-Host "=============================================="
Write-Host " 이 작업이 등록되면, 매일 새벽 4시에 pastellive_db와 lumi_db를"
Write-Host " 자동으로 백업해서 backups\db\ 폴더에 zip으로 저장합니다."
Write-Host " 14일 넘은 백업은 자동으로 지워집니다."
Write-Host " 백업 기록은 logs\db_backup.log 에서 확인할 수 있습니다."
Write-Host ""

if (-not (Test-Path $BackupScript)) {
    Write-Host "[오류] $BackupScript 를 찾을 수 없습니다."
    Read-Host "Enter 키를 누르면 창이 닫힙니다"
    exit 1
}

Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue

$action = New-ScheduledTaskAction -Execute 'powershell.exe' `
    -Argument "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$BackupScript`""

$trigger = New-ScheduledTaskTrigger -Daily -At '04:00'

$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest

$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 30) -MultipleInstances IgnoreNew

try {
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
        -Principal $principal -Settings $settings -Description "pastellive_db/lumi_db를 매일 자동 백업" `
        -ErrorAction Stop | Out-Null

    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Write-Host "등록 완료. 매일 새벽 4시에 자동 백업됩니다."
        Write-Host ""
        Write-Host "지금 바로 한 번 실행해보기: schtasks /run /tn $TaskName"
        Write-Host "확인:   schtasks /query /tn $TaskName"
        Write-Host "끄기:   scripts\disable_db_backup.bat"
    } else {
        Write-Host "[오류] 등록 명령은 실행됐지만 작업이 실제로 안 만들어졌습니다. 위 오류 메시지를 확인해주세요."
    }
} catch {
    Write-Host "[오류] 작업 등록 실패: $($_.Exception.Message)"
}

Read-Host "Enter 키를 누르면 창이 닫힙니다"
