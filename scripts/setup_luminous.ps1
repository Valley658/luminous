#Requires -RunAsAdministrator
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {}

$AppDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Write-Host "APPDIR = $AppDir"
Write-Host ""

function Get-FileWithProgress {
    param(
        [Parameter(Mandatory=$true)][string]$Uri,
        [Parameter(Mandatory=$true)][string]$OutFile,
        [string]$Label = '다운로드'
    )
    try {
        Import-Module BitsTransfer -ErrorAction Stop
        $job = Start-BitsTransfer -Source $Uri -Destination $OutFile -DisplayName $Label -Asynchronous -ErrorAction Stop
        while ($job.JobState -in @('Connecting', 'Transferring', 'TransientError')) {
            $totalMb = [math]::Round($job.BytesTotal / 1MB, 1)
            $doneMb = [math]::Round($job.BytesTransferred / 1MB, 1)
            $pct = if ($job.BytesTotal -gt 0) { [math]::Round(($job.BytesTransferred / $job.BytesTotal) * 100) } else { 0 }
            Write-Host "`r  진행률: $pct%  ($doneMb / $totalMb MB)   " -NoNewline
            Start-Sleep -Milliseconds 500
            $job = Get-BitsTransfer -JobId $job.JobId
        }
        Write-Host ""
        if ($job.JobState -eq 'Transferred') {
            Complete-BitsTransfer -BitsJob $job
            return
        } else {
            Remove-BitsTransfer -BitsJob $job -ErrorAction SilentlyContinue
            throw "BITS 작업 상태: $($job.JobState)"
        }
    } catch {
        Write-Host "  (BITS 다운로드 실패, 대체 방식으로 재시도: $($_.Exception.Message))"
    }

    $wc = New-Object System.Net.WebClient
    $script:gfwLastPct = -1
    $onProgress = Register-ObjectEvent -InputObject $wc -EventName DownloadProgressChanged -Action {
        $pct = $EventArgs.ProgressPercentage
        if ($pct -ge ($script:gfwLastPct + 5)) {
            $mb = [math]::Round($EventArgs.BytesReceived / 1MB, 1)
            $totalMb = [math]::Round($EventArgs.TotalBytesToReceive / 1MB, 1)
            Write-Host "`r  진행률: $pct%  ($mb / $totalMb MB)   " -NoNewline
            $script:gfwLastPct = $pct
        }
    }
    $onDone = Register-ObjectEvent -InputObject $wc -EventName DownloadFileCompleted -Action { $script:gfwDone = $true }
    $script:gfwDone = $false
    try {
        $wc.DownloadFileAsync([Uri]$Uri, $OutFile)
        while (-not $script:gfwDone) { Start-Sleep -Milliseconds 300 }
        Write-Host ""
    } finally {
        Unregister-Event -SourceIdentifier $onProgress.Name -ErrorAction SilentlyContinue
        Unregister-Event -SourceIdentifier $onDone.Name -ErrorAction SilentlyContinue
        $wc.Dispose()
    }
}

# ---------------------------------------------------------------------
Write-Host "=============================================="
Write-Host " [1/6] nginx 설치 (C:\nginx)"
Write-Host "=============================================="
if (Test-Path 'C:\nginx\nginx.exe') {
    Write-Host "  이미 설치되어 있음, 건너뜀."
} else {
    try {
        Write-Host "  다운로드 중: nginx-1.30.4 (nginx.org 공식) ..."
        $zip = "$env:TEMP\nginx.zip"
        Get-FileWithProgress -Uri 'https://nginx.org/download/nginx-1.30.4.zip' -OutFile $zip -Label 'nginx'
        Expand-Archive -Path $zip -DestinationPath 'C:\' -Force
        if (Test-Path 'C:\nginx') { Remove-Item 'C:\nginx' -Recurse -Force }
        Move-Item 'C:\nginx-1.30.4' 'C:\nginx'
        Remove-Item $zip -Force -ErrorAction SilentlyContinue
        if (Test-Path 'C:\nginx\nginx.exe') {
            Write-Host "  설치 완료: C:\nginx"
        } else {
            Write-Host "  [오류] 압축 해제 후 nginx.exe 를 찾을 수 없습니다."
        }
    } catch {
        Write-Host "  [오류] nginx 설치 실패: $($_.Exception.Message)"
        Write-Host "  수동으로 https://nginx.org/en/download.html 에서 받아 C:\nginx 에 압축을 풀어주세요."
    }
}

# ---------------------------------------------------------------------
Write-Host ""
Write-Host "=============================================="
Write-Host " [2/6] cloudflared 설치 (C:\cloudflared)"
Write-Host "=============================================="
if (Test-Path 'C:\cloudflared\cloudflared.exe') {
    Write-Host "  이미 설치되어 있음, 건너뜀."
} else {
    try {
        New-Item -ItemType Directory -Path 'C:\cloudflared' -Force | Out-Null
        Write-Host "  다운로드 중: cloudflared (Cloudflare 공식 GitHub 릴리스) ..."
        Get-FileWithProgress -Uri 'https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe' -OutFile 'C:\cloudflared\cloudflared.exe' -Label 'cloudflared'
        if (Test-Path 'C:\cloudflared\cloudflared.exe') {
            Write-Host "  설치 완료: C:\cloudflared\cloudflared.exe"
        } else {
            Write-Host "  [오류] cloudflared.exe 를 찾을 수 없습니다."
        }
    } catch {
        Write-Host "  [오류] cloudflared 다운로드 실패: $($_.Exception.Message)"
        Write-Host "  수동으로 https://github.com/cloudflare/cloudflared/releases/latest 에서"
        Write-Host "  cloudflared-windows-amd64.exe 를 받아 C:\cloudflared\cloudflared.exe 로 저장하세요."
    }
}

# ---------------------------------------------------------------------
Write-Host ""
Write-Host "=============================================="
Write-Host " [3/6] ffmpeg 설치 (C:\ffmpeg) - 영상/썸네일 처리용, 없어도 메인페이지는 뜸"
Write-Host "=============================================="
if (Test-Path 'C:\ffmpeg\bin\ffmpeg.exe') {
    Write-Host "  이미 설치되어 있음, 건너뜀."
} else {
    try {
        Write-Host "  다운로드 중: ffmpeg essentials build (gyan.dev) ..."
        $ffZip = "$env:TEMP\ffmpeg.zip"
        $ffExtract = "$env:TEMP\ffmpeg_extract"
        Get-FileWithProgress -Uri 'https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip' -OutFile $ffZip -Label 'ffmpeg'
        if (Test-Path $ffExtract) { Remove-Item $ffExtract -Recurse -Force }
        Expand-Archive -Path $ffZip -DestinationPath $ffExtract -Force
        $inner = Get-ChildItem -Path $ffExtract -Directory | Where-Object { $_.Name -like 'ffmpeg-*' } | Select-Object -First 1
        if ($inner) {
            Copy-Item -Path (Join-Path $inner.FullName '*') -Destination 'C:\ffmpeg\' -Recurse -Force
        }
        Remove-Item $ffZip -Force -ErrorAction SilentlyContinue
        Remove-Item $ffExtract -Recurse -Force -ErrorAction SilentlyContinue
        if (Test-Path 'C:\ffmpeg\bin\ffmpeg.exe') {
            Write-Host "  설치 완료: C:\ffmpeg"
        } else {
            Write-Host "  [오류] ffmpeg.exe 를 찾을 수 없습니다 (건너뜀, 필수는 아님)."
        }
    } catch {
        Write-Host "  [오류] ffmpeg 다운로드/설치 실패, 건너뜁니다 (필수는 아님): $($_.Exception.Message)"
    }
}

# ---------------------------------------------------------------------
Write-Host ""
Write-Host "=============================================="
Write-Host " [4/6] MySQL 확인 (사이트 DB, 127.0.0.1:3306)"
Write-Host "=============================================="
$svc = Get-Service | Where-Object { $_.Name -like '*mysql*' -or $_.Name -like '*maria*' }
if ($svc) {
    $svc | Select-Object Name, Status | Format-Table -AutoSize
} else {
    Write-Host "  [경고] MySQL/MariaDB 서비스가 설치되어 있지 않습니다."
    Write-Host "  https://dev.mysql.com/downloads/installer/ 에서 MySQL Server 를 설치하고,"
    Write-Host "  root 비밀번호를 .env 의 DB_PASSWORD 값과 동일하게 맞춰주세요."
    Write-Host "  (DB가 없으면 메인페이지가 계속 비어 보일 수 있습니다.)"
}

# ---------------------------------------------------------------------
Write-Host ""
Write-Host "=============================================="
Write-Host " [5/6] nginx / Meilisearch / Go 서버(로고 수정 반영) 등록 및 기동"
Write-Host "=============================================="
$installAll = Join-Path $AppDir 'deploy\windows\install_all.bat'

Write-Host "  -- nginx --"
& $installAll 3
Write-Host "  -- Meilisearch --"
& $installAll 4
Write-Host "  -- DB 스키마 확인/생성 (pastellive_db 없으면 생성) --"
& $installAll 11
Write-Host "  -- Go 서버 cutover (PastelliveApp 등록/기동) --"
& $installAll 14
Write-Host "  -- Go 서버에 로고 수정 반영본(pastellive-server.new.exe) 적용 --"
& $installAll 15

Write-Host ""
$cfToken = Read-Host "Cloudflare Tunnel 토큰을 입력하세요 (Zero Trust 대시보드 -> Networks -> Tunnels, 이미 설정했거나 나중에 하려면 그냥 Enter)"
if ($cfToken) {
    & $installAll 5 $cfToken
}

# ---------------------------------------------------------------------
Write-Host ""
Write-Host "=============================================="
Write-Host " [6/6] 전체 서비스 시작 (scripts\START_SERVICE.bat)"
Write-Host "=============================================="
& (Join-Path $AppDir 'scripts\START_SERVICE.bat')

Write-Host ""
Write-Host "=============================================="
Write-Host " 완료. 잠시 후 https://pastellive.co.kr 에서 확인해보세요."
Write-Host " 문제가 있으면 진단 도구를 실행하세요: deploy\windows\install_all.bat 7"
Write-Host "=============================================="
Read-Host "Enter 키를 누르면 창이 닫힙니다"
