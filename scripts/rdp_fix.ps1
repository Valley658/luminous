# RDP "잠시만 기다려 주세요"에서 멈추는 문제 수정 (RDP_수정.bat 이 관리자 권한으로 실행)
#  - 원격 세션이 그래픽카드(WDDM/NVIDIA) 드라이버와 하드웨어 인코딩을 쓰지 않게 함
#    (GTX 1060을 Ollama/VoiceStudio가 같이 쓰고 있어서 원격 화면 준비가 멈추는 경우가 많음)
#  - 연결 방식을 TCP로 고정 (UDP 협상 중 멈추는 경우 방지)
#  - 끊긴 채 남아 있는 원격 세션 정리 (선택)
#  - VoiceStudio 자동 실행을 로그인 90초 뒤로 늦춤 (로그인 순간 GPU를 잡지 않게)
$ErrorActionPreference = "Continue"
[Console]::OutputEncoding = [Text.Encoding]::UTF8
function Say($m, $c = "Gray") { Write-Host $m -ForegroundColor $c }
function Ask($m) { Write-Host -NoNewline "$m " -ForegroundColor Yellow; return (Read-Host) }

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$stamp = Get-Date -Format "yyyyMMdd_HHmmss"
$bak = Join-Path $root "rdp_backup_$stamp.reg"
$key = "HKLM\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services"
$ps  = "HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services"

Say ""
Say "  RDP '잠시만 기다려 주세요' 멈춤 수정" "Magenta"
Say ""

# 1. 백업
Say "[1/5] 지금 설정 백업" "Cyan"
if (-not (Test-Path $ps)) { New-Item -Path $ps -Force | Out-Null }
reg.exe export $key $bak /y | Out-Null
Say "  $bak  (되돌리려면 이 파일을 더블클릭)" "DarkGray"

# 2. 그래픽 관련 정책
Say "[2/5] 원격 세션이 그래픽카드를 쓰지 않게 설정" "Cyan"
$vals = [ordered]@{
    "fEnableWddmDriver"          = 0  # 원격 세션에 WDDM(그래픽카드) 드라이버 사용 안 함 → 예전 XDDM 방식
    "bEnumerateHWBeforeSW"       = 0  # 모든 원격 세션에 하드웨어 그래픽 어댑터 사용 안 함
    "AVCHardwareEncodePreferred" = 0  # 화면 압축에 GPU(NVENC) 사용 안 함
    "AVC444ModePreferred"        = 0  # AVC 4:4:4 모드 끔
    "SelectTransport"            = 1  # TCP만 사용 (UDP 협상 멈춤 방지)
}
foreach ($k in $vals.Keys) {
    New-ItemProperty -Path $ps -Name $k -Value $vals[$k] -PropertyType DWord -Force | Out-Null
    Say ("  {0} = {1}" -f $k, $vals[$k]) "Green"
}
gpupdate /target:computer /force | Out-Null
Say "  정책 적용 완료" "Green"

# 3. VoiceStudio 자동 실행 늦추기
Say "[3/5] VoiceStudio 자동 실행을 로그인 90초 뒤로" "Cyan"
$startup = [Environment]::GetFolderPath("Startup")
$users = Get-ChildItem "C:\Users" -Directory -ErrorAction SilentlyContinue
$done = $false
foreach ($u in $users) {
    $dir = Join-Path $u.FullName "AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup"
    $lnk = Join-Path $dir "VoiceStudio.lnk"
    if (Test-Path $lnk) {
        $keep = Join-Path $u.FullName "AppData\Roaming\VoiceStudio_autostart.lnk"
        Move-Item $lnk $keep -Force
        $cmd = Join-Path $dir "VoiceStudio_지연실행.cmd"
        [IO.File]::WriteAllText($cmd, "@echo off`r`ntimeout /t 90 /nobreak >nul`r`nstart `"`" `"$keep`"`r`n", (New-Object Text.UTF8Encoding($false)))
        Say "  $($u.Name): 바로 실행 → 90초 뒤 실행으로 바꿈" "Green"
        $done = $true
    }
}
if (-not $done) { Say "  VoiceStudio 자동 실행이 없거나 이미 바뀌어 있어요. 건너뜀." "DarkGray" }

# 4. 남아 있는 세션
Say "[4/5] 원격 세션 상태" "Cyan"
$q = (query session 2>$null) -join "`n"
Say $q "DarkGray"
$stale = @()
foreach ($line in (query session 2>$null | Select-Object -Skip 1)) {
    if ($line -match '^\s*>') { continue }  # 지금 이 창이 열린 세션
    if ($line -match '\s(\d+)\s+(Disc|디스크|연결 끊김|끊김)') { $stale += $Matches[1] }
}
if ($stale.Count -gt 0) {
    Say "  끊긴 채 남아 있는 세션: $($stale -join ', ')" "Yellow"
    foreach ($id in $stale) { logoff $id 2>$null; Say "  세션 $id 로그오프 (자동)" "Green" }
} else { Say "  끊긴 세션 없음" "Green" }

# 5. 마무리
Say "[5/5] 적용하려면 다시 시작이 필요해요" "Cyan"
Say "  원격 데스크톱 서비스를 다시 시작하거나 PC를 재부팅해야 새 설정이 적용돼요." 
Say "  (루미너스 서버, nginx, 터널은 SYSTEM 작업이라 재부팅 후 자동으로 다시 켜져요)" "DarkGray"
shutdown.exe /r /t 60 /c "RDP 설정 적용을 위한 재부팅 (취소: shutdown /a)"
Say "  1분 뒤 자동으로 재부팅해요. 취소하려면 cmd에서 shutdown /a" "Green"
