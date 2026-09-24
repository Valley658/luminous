#Requires -RunAsAdministrator
# ============================================================================
# 새 빌드(.new.exe) 자동 적용 스크립트
#
# go-server\bin\pastellive-server.new.exe / discord-bot.new.exe,
# services\c-image-service\bin\pastellive-image-service.new.exe 중 하나가
# 새로 생겨 있으면(=제가 빌드해서 놓아둔 것), install_all.bat 15/20번이
# 하던 것과 똑같은 방식으로 자동으로 적용합니다:
#   1) 해당 스케줄 작업(서비스) 중지
#   2) 기존 exe -> .old.exe로 백업
#   3) .new.exe -> 실제 exe로 교체
#   4) 스케줄 작업 다시 시작
#
# 이 스크립트 자체는 사람이 직접 실행하는 게 아니라, setup_autodeploy.bat로
# 등록해두는 "PastelliveAutoDeploy" 작업 스케줄러 작업이 몇 분마다 자동으로
# 실행합니다. 적용한 내용/실패 여부는 항상 logs\auto_deploy.log에 기록됩니다.
# ============================================================================
$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}

$AppDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$GoBin = Join-Path $AppDir 'go-server\bin'
$CImgBin = Join-Path $AppDir 'services\c-image-service\bin'
$LogDir = Join-Path $AppDir 'logs'
$LogFile = Join-Path $LogDir 'auto_deploy.log'
if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }

function Write-Log {
    param([string]$Msg)
    $line = "[{0}] {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Msg
    Add-Content -Path $LogFile -Value $line -Encoding UTF8
}

function Apply-Swap {
    param(
        [Parameter(Mandatory=$true)][string]$BinDir,
        [Parameter(Mandatory=$true)][string]$ExeName,   # 예: pastellive-server.exe
        [Parameter(Mandatory=$true)][string]$TaskName,  # 예: PastelliveApp
        [int]$Port = 0                                   # 0이면 포트 강제종료 생략
    )
    $newExe = Join-Path $BinDir ($ExeName -replace '\.exe$', '.new.exe')
    if (-not (Test-Path $newExe)) { return }

    Write-Log "[$ExeName] 새 빌드 발견 - 적용 시작"
    try {
        schtasks /end /tn $TaskName *> $null

        if ($Port -gt 0) {
            $conns = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
            foreach ($ownerPid in ($conns | Select-Object -ExpandProperty OwningProcess -Unique)) {
                Stop-Process -Id $ownerPid -Force -ErrorAction SilentlyContinue
            }
        }
        Start-Sleep -Seconds 2

        $exePath = Join-Path $BinDir $ExeName
        $oldPath = Join-Path $BinDir ($ExeName -replace '\.exe$', '.old.exe')
        if (Test-Path $oldPath) { Remove-Item $oldPath -Force -ErrorAction SilentlyContinue }
        if (Test-Path $exePath) { Move-Item $exePath $oldPath -Force }
        Move-Item $newExe $exePath -Force

        if (-not (Test-Path $exePath)) {
            Write-Log "[$ExeName] [오류] 교체 실패 - $exePath 가 없음"
            return
        }

        schtasks /run /tn $TaskName *> $null
        Write-Log "[$ExeName] 적용 완료 (이전 빌드는 $($ExeName -replace '\.exe$','.old.exe')로 백업됨)"
    } catch {
        Write-Log "[$ExeName] [오류] 적용 중 예외 발생: $($_.Exception.Message)"
    }
}

Apply-Swap -BinDir $GoBin    -ExeName 'pastellive-server.exe'       -TaskName 'PastelliveApp'            -Port 8081
Apply-Swap -BinDir $GoBin    -ExeName 'discord-bot.exe'             -TaskName 'PastelliveDiscordBot'
Apply-Swap -BinDir $CImgBin  -ExeName 'pastellive-image-service.exe' -TaskName 'PastelliveCImageService' -Port 8091
