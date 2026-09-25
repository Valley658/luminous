#Requires -Version 5.1
# ============================================================================
# MySQL DB 자동 백업 스크립트
#
# pastellive_db, lumi_db를 mysqldump로 덤프해서 backups\db\ 아래에 압축(zip)
# 저장하고, 오래된 백업은 자동으로 지웁니다(기본 14일 보관).
#
# 사람이 직접 실행하는 게 아니라, setup_db_backup.bat로 등록해두는
# "PastelliveDBBackup" 작업 스케줄러 작업이 매일 자동으로 실행합니다.
# 실행 결과는 항상 logs\db_backup.log에 기록됩니다.
#
# 수동으로 지금 바로 백업하고 싶으면 이 스크립트를 그냥 실행해도 됩니다:
#   powershell -ExecutionPolicy Bypass -File scripts\backup_db.ps1
# ============================================================================
$ErrorActionPreference = 'Continue'
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {}

$AppDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$EnvFile = Join-Path $AppDir '.env'
$BackupDir = Join-Path $AppDir 'backups\db'
$LogDir = Join-Path $AppDir 'logs'
$LogFile = Join-Path $LogDir 'db_backup.log'
$RetentionDays = 14

if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
if (-not (Test-Path $BackupDir)) { New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null }

function Write-Log {
    param([string]$Msg)
    $line = "[{0}] {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Msg
    Add-Content -Path $LogFile -Value $line -Encoding UTF8
    Write-Host $line
}

# ---- .env 파일에서 DB 접속 정보 읽기 ---------------------------------------
if (-not (Test-Path $EnvFile)) {
    Write-Log "[오류] .env 파일을 찾을 수 없습니다: $EnvFile"
    exit 1
}
$envVars = @{}
Get-Content $EnvFile -Encoding UTF8 | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq '' -or $line.StartsWith('#')) { return }
    $idx = $line.IndexOf('=')
    if ($idx -lt 1) { return }
    $key = $line.Substring(0, $idx).Trim()
    $val = $line.Substring($idx + 1).Trim()
    $envVars[$key] = $val
}

$DbHost = if ($envVars['DB_HOST']) { $envVars['DB_HOST'] } else { '127.0.0.1' }
$DbPort = if ($envVars['DB_PORT']) { $envVars['DB_PORT'] } else { '3306' }
$DbUser = if ($envVars['DB_USER']) { $envVars['DB_USER'] } else { 'root' }
$DbPass = $envVars['DB_PASSWORD']
$DbName = if ($envVars['DB_NAME']) { $envVars['DB_NAME'] } else { 'pastellive_db' }
$LumiDbName = if ($envVars['LUMI_DB_NAME']) { $envVars['LUMI_DB_NAME'] } else { 'lumi_db' }

if ($envVars['DB_BACKEND'] -and $envVars['DB_BACKEND'] -ne 'mysql') {
    Write-Log "[정보] DB_BACKEND=$($envVars['DB_BACKEND']) (MySQL이 아님) - 이 백업 스크립트는 MySQL 전용이라 건너뜁니다."
    exit 0
}

# ---- mysqldump.exe 찾기 -----------------------------------------------------
$mysqldump = Get-Command mysqldump.exe -ErrorAction SilentlyContinue
if ($mysqldump) {
    $mysqldumpPath = $mysqldump.Source
} else {
    $candidates = Get-ChildItem 'C:\Program Files\MySQL' -Recurse -Filter 'mysqldump.exe' -ErrorAction SilentlyContinue |
        Select-Object -First 1 -ExpandProperty FullName
    if (-not $candidates) {
        Write-Log "[오류] mysqldump.exe를 찾을 수 없습니다. MySQL Server가 설치되어 있는지 확인해주세요."
        exit 1
    }
    $mysqldumpPath = $candidates
}

$timestamp = Get-Date -Format 'yyyyMMdd_HHmmss'
$failed = $false

function Backup-Database {
    param([string]$Name)

    $sqlFile = Join-Path $BackupDir "${Name}_${timestamp}.sql"
    $zipFile = Join-Path $BackupDir "${Name}_${timestamp}.zip"

    $argList = @(
        "--host=$DbHost", "--port=$DbPort", "--user=$DbUser",
        '--single-transaction', '--quick', '--routines', '--triggers',
        '--default-character-set=utf8mb4',
        $Name
    )
    $envBackup = $env:MYSQL_PWD
    $env:MYSQL_PWD = $DbPass
    try {
        & $mysqldumpPath @argList 2>"$sqlFile.err" | Out-File -FilePath $sqlFile -Encoding UTF8
    } finally {
        $env:MYSQL_PWD = $envBackup
    }

    if (-not (Test-Path $sqlFile) -or (Get-Item $sqlFile).Length -eq 0) {
        $errText = if (Test-Path "$sqlFile.err") { Get-Content "$sqlFile.err" -Raw } else { '' }
        Write-Log "[$Name] [오류] 백업 실패 - 덤프 파일이 비어있음. $errText"
        Remove-Item $sqlFile -Force -ErrorAction SilentlyContinue
        Remove-Item "$sqlFile.err" -Force -ErrorAction SilentlyContinue
        $script:failed = $true
        return
    }
    Remove-Item "$sqlFile.err" -Force -ErrorAction SilentlyContinue

    try {
        Compress-Archive -Path $sqlFile -DestinationPath $zipFile -Force
        Remove-Item $sqlFile -Force
        $sizeKB = [math]::Round((Get-Item $zipFile).Length / 1KB, 1)
        Write-Log "[$Name] 백업 완료: $(Split-Path $zipFile -Leaf) (${sizeKB}KB)"
    } catch {
        Write-Log "[$Name] [경고] 압축 실패, .sql 원본 그대로 둠: $($_.Exception.Message)"
    }
}

Backup-Database -Name $DbName
Backup-Database -Name $LumiDbName

# ---- 오래된 백업 정리 --------------------------------------------------------
$cutoff = (Get-Date).AddDays(-$RetentionDays)
$old = Get-ChildItem $BackupDir -Filter '*.zip' -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt $cutoff }
foreach ($f in $old) {
    Remove-Item $f.FullName -Force -ErrorAction SilentlyContinue
    Write-Log "[정리] ${RetentionDays}일 넘은 백업 삭제: $($f.Name)"
}

if ($failed) {
    Write-Log "백업 작업 완료 (일부 실패 있음 - 위 로그 확인)"
    exit 1
} else {
    Write-Log "백업 작업 완료"
    exit 0
}
