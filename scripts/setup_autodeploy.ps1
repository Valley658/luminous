#Requires -RunAsAdministrator
$ErrorActionPreference = 'Continue'
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {}

$AppDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$WatcherScript = Join-Path $AppDir 'scripts\watch_and_apply.ps1'
$TaskName = 'PastelliveAutoDeploy'

Write-Host "=============================================="
Write-Host " 새 빌드 자동 적용 작업 등록: $TaskName"
Write-Host "=============================================="
Write-Host " 이 작업이 등록되면, 앞으로 go-server\bin\pastellive-server.new.exe"
Write-Host " 같은 새 빌드 파일이 생길 때마다(제가 고치고 빌드해서 놓아두면) 2분 안에"
Write-Host " 자동으로 적용됩니다 - install_all.bat 15/20번을 직접 누르실 필요가 없어요."
Write-Host " 적용 기록은 logs\auto_deploy.log 에서 확인할 수 있습니다."
Write-Host ""

if (-not (Test-Path $WatcherScript)) {
    Write-Host "[오류] $WatcherScript 를 찾을 수 없습니다."
    Read-Host "Enter 키를 누르면 창이 닫힙니다"
    exit 1
}

# 이미 등록되어 있으면 지우고 새로 등록 (설정이 바뀌었을 수도 있으니 깨끗하게)
Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue

$action = New-ScheduledTaskAction -Execute 'powershell.exe' `
    -Argument "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$WatcherScript`""

$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) `
    -RepetitionInterval (New-TimeSpan -Minutes 2) `
    -RepetitionDuration (New-TimeSpan -Days 3650)

$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest

$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 5) -MultipleInstances IgnoreNew

try {
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
        -Principal $principal -Settings $settings -Description "새 pastellive 빌드(.new.exe)를 자동으로 감지해서 적용" `
        -ErrorAction Stop | Out-Null

    # 실제로 등록됐는지 다시 확인 - Register-ScheduledTask가 에러를 내고도
    # $ErrorActionPreference 설정에 따라 조용히 넘어가는 경우가 있어서,
    # "등록 완료" 메시지를 무조건 믿지 않고 한 번 더 확인한다.
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Write-Host "등록 완료. 2분마다 새 빌드가 있는지 확인합니다."
        Write-Host ""
        Write-Host "확인:   schtasks /query /tn $TaskName"
        Write-Host "끄기:   scripts\disable_autodeploy.bat"
    } else {
        Write-Host "[오류] 등록 명령은 실행됐지만 작업이 실제로 안 만들어졌습니다. 위 오류 메시지를 확인해주세요."
    }
} catch {
    Write-Host "[오류] 작업 등록 실패: $($_.Exception.Message)"
}

Read-Host "Enter 키를 누르면 창이 닫힙니다"
