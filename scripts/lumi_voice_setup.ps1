# 루미 목소리 한 번에 설치 + 오늘 바뀐 내용 적용 (scripts\루미목소리_설치.bat 이 관리자 권한으로 실행함)
#  1. VoiceStudio 확인 → 없으면 GitHub 최신 버전 받아 설치 → 실행 → PC 켤 때 자동 실행 등록
#  2. 루미 목소리 고르기 (VoiceStudio에 저장된 목소리 목록에서) → 시험 문장 들어 보기
#  3. .env 에 VOICESTUDIO_VOICE 저장
#  4. 예전 디스코드 봇 작업 정리, nginx 설정 다시 읽기(SYSTEM 권한으로)
#  5. 빌드배포.bat 실행 → 서버가 켜지면 고정 멘트 자동 녹음 확인
param([string]$AppDir = "")
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [Text.Encoding]::UTF8
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
if (-not $AppDir) { $AppDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path) }
$EnvPath  = Join-Path $AppDir ".env"
$VsBase   = "http://127.0.0.1:3900"
$SiteBase = "http://127.0.0.1:8081"
$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)

function Say($msg, $color = "Gray") { Write-Host $msg -ForegroundColor $color }
function Step($n, $msg) { Write-Host ""; Write-Host "[$n] $msg" -ForegroundColor Cyan }
function Ask($msg) { Write-Host -NoNewline "$msg " -ForegroundColor Yellow; return (Read-Host) }

# ---------- .env 읽기/쓰기 (한글이 깨지지 않게 UTF-8 그대로) ----------
function Get-EnvValue($key) {
    if (-not (Test-Path $EnvPath)) { return "" }
    foreach ($line in [IO.File]::ReadAllLines($EnvPath, [Text.Encoding]::UTF8)) {
        if ($line -match "^\s*$key\s*=(.*)$") { return $Matches[1].Trim() }
    }
    return ""
}
function Set-EnvValue($key, $value) {
    $text = ""
    if (Test-Path $EnvPath) {
        if (-not $script:EnvBackedUp) { Copy-Item $EnvPath "$EnvPath.bak_lumivoice" -Force; $script:EnvBackedUp = $true }
        $text = [IO.File]::ReadAllText($EnvPath, [Text.Encoding]::UTF8)
    }
    $nl = if ($text -match "`r`n") { "`r`n" } else { "`n" }
    $pattern = "(?m)^[ \t]*$key[ \t]*=.*$"
    if ([regex]::IsMatch($text, $pattern)) {
        $text = [regex]::Replace($text, $pattern, "$key=$value")
    } else {
        if ($text.Length -gt 0 -and -not $text.EndsWith("`n")) { $text += $nl }
        $text += "$nl# 루미 목소리 (VoiceStudio 목소리 ID) - scripts\루미목소리_설치.bat 이 저장함$nl$key=$value$nl"
    }
    [IO.File]::WriteAllText($EnvPath, $text, $Utf8NoBom)
}

# ---------- VoiceStudio ----------
function Test-VoiceStudio {
    # 버전에 따라 API 포트가 다를 수 있어서 몇 개를 확인하고, 찾은 주소를 $VsBase 로 쓴다.
    foreach ($port in @(3900, 3902)) {
        try {
            $null = Invoke-WebRequest "http://127.0.0.1:$port/.well-known/voicestudio-speech" -UseBasicParsing -TimeoutSec 3
            $script:VsBase = "http://127.0.0.1:$port"
            return $true
        } catch { }
    }
    return $false
}
function Find-VsShortcut {
    # 바로가기(.lnk)를 먼저 찾고, 없으면 설치 폴더의 VoiceStudio.exe 를 찾는다.
    $dirs = @("$env:ProgramData\Microsoft\Windows\Start Menu\Programs", "$env:APPDATA\Microsoft\Windows\Start Menu\Programs", "$env:PUBLIC\Desktop", [Environment]::GetFolderPath("Desktop"))
    foreach ($d in $dirs) {
        if ($d -and (Test-Path $d)) {
            $s = Get-ChildItem $d -Recurse -Filter "VoiceStudio*.lnk" -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($s) { return $s.FullName }
        }
    }
    foreach ($d in @("$env:LOCALAPPDATA\Programs", $env:ProgramFiles, ${env:ProgramFiles(x86)})) {
        if ($d -and (Test-Path $d)) {
            $e = Get-ChildItem $d -Directory -Filter "*voicestudio*" -ErrorAction SilentlyContinue |
                 ForEach-Object { Get-ChildItem $_.FullName -Filter "VoiceStudio*.exe" -ErrorAction SilentlyContinue } |
                 Where-Object { $_.Name -notmatch "uninstall" } | Select-Object -First 1
            if ($e) { return $e.FullName }
        }
    }
    return $null
}
function Start-VoiceStudio($lnk) {
    # explorer 로 열면 관리자 권한이 아닌 평소 사용자 권한으로 실행됨
    Start-Process explorer.exe -ArgumentList "`"$lnk`""
}
function Wait-VoiceStudio($seconds) {
    $end = (Get-Date).AddSeconds($seconds)
    Write-Host -NoNewline "  VoiceStudio 켜지기를 기다리는 중"
    while ((Get-Date) -lt $end) {
        if (Test-VoiceStudio) { Write-Host " 완료!" -ForegroundColor Green; return $true }
        Write-Host -NoNewline "."
        Start-Sleep -Seconds 3
    }
    Write-Host ""
    return $false
}
function Install-VoiceStudio {
    Say "  GitHub에서 VoiceStudio 최신 버전을 찾는 중..."
    $rel = Invoke-RestMethod "https://api.github.com/repos/debpalash/VoiceStudio/releases/latest" -Headers @{ "User-Agent" = "luminous-setup" } -TimeoutSec 30
    # 0.5.4부터 윈도우 설치 파일이 .msi 에서 .exe (VoiceStudio-Electron-x.y.z-win-x64.exe) 로 바뀌었다. 둘 다 지원.
    $asset = $rel.assets | Where-Object { $_.name -match "win-x64\.exe$" } | Select-Object -First 1
    if (-not $asset) { $asset = $rel.assets | Where-Object { $_.name -match "_x64_.*\.msi$" -and $_.name -notmatch "Current_User" } | Select-Object -First 1 }
    if (-not $asset) { throw "윈도우용 설치 파일을 찾지 못했어요. https://github.com/debpalash/VoiceStudio/releases/latest 에서 직접 받아 설치한 뒤 이 파일을 다시 실행해 주세요." }
    $mb = [math]::Round($asset.size / 1MB)
    Say "  찾음: $($asset.name) ($mb MB, 버전 $($rel.tag_name))"
    Say "  설치하면 디스크를 약 10GB 써요. NVIDIA 그래픽카드가 있으면 더 빨라요." "DarkGray"
    if ((Ask "  지금 내려받아 설치할까요? (Y/N)") -notmatch "^[Yy]") { throw "설치를 취소했어요." }
    $installer = Join-Path $env:TEMP $asset.name
    Say "  내려받는 중... (크기에 따라 몇 분 걸려요)"
    $ProgressPreference = "SilentlyContinue"
    Invoke-WebRequest $asset.browser_download_url -OutFile $installer -UseBasicParsing
    Say "  설치 중... (1~2분 걸려요)"
    if ($installer -match "\.msi$") {
        $p = Start-Process msiexec.exe -ArgumentList "/i `"$installer`" /passive /norestart" -Wait -PassThru
    } else {
        $p = Start-Process $installer -ArgumentList "/S" -Wait -PassThru   # 조용히 설치
    }
    if ($p.ExitCode -ne 0 -and $p.ExitCode -ne 3010) { throw "설치 실패 (설치 프로그램 코드 $($p.ExitCode))" }
    Start-Sleep -Seconds 3
    Say "  설치 완료!" "Green"
}

function Get-Profiles {
    foreach ($path in @("/profiles", "/api/profiles", "/v1/profiles")) {
        try {
            # PowerShell 5 는 응답에 charset 이 없으면 한글을 깨뜨리므로 바이트를 UTF-8 로 직접 읽는다
            $resp = Invoke-WebRequest "$VsBase$path" -UseBasicParsing -TimeoutSec 10
            $bytes = $resp.RawContentStream.ToArray()
            $r = [Text.Encoding]::UTF8.GetString($bytes) | ConvertFrom-Json
            $list = $r
            if ($r -isnot [array]) {
                foreach ($k in @("profiles", "items", "data", "voices")) { if ($r.$k) { $list = $r.$k; break } }
            }
            $out = @()
            foreach ($p in @($list)) {
                $id = $p.id; if (-not $id) { $id = $p.profile_id }
                $name = $p.name; if (-not $name) { $name = $p.display_name }
                if ($id) { $out += [pscustomobject]@{ Id = "$id"; Name = "$name"; Kind = "$($p.kind)" } }
            }
            return ,$out
        } catch { }
    }
    return ,@()
}

function Test-Voice($voiceId) {
    $out = Join-Path $env:TEMP "lumi_voice_test.mp3"
    $body = @{ model = "tts-1"; input = "안녕! 나는 루미너스에 사는 루미야. 앞으로 내 목소리로 대답해 줄게!"; voice = $voiceId; response_format = "mp3"; language = "ko" } | ConvertTo-Json
    Say "  시험 문장을 만드는 중... (처음엔 1분 넘게 걸릴 수 있어요)"
    Invoke-WebRequest "$VsBase/v1/audio/speech" -Method Post -ContentType "application/json; charset=utf-8" -Body ([Text.Encoding]::UTF8.GetBytes($body)) -OutFile $out -UseBasicParsing -TimeoutSec 300
    if ((Get-Item $out).Length -lt 1000) { throw "음성이 비어 있어요." }
    Start-Process $out
    return $true
}

# ==================== 시작 ====================
Write-Host ""
Write-Host "  루미 목소리 설치 + 루미너스 업데이트 적용" -ForegroundColor Magenta
Write-Host "   1. VoiceStudio 설치/실행   2. 루미 목소리 고르기 + 시험 듣기   3. .env 저장" -ForegroundColor DarkGray
Write-Host "   4. 예전 디스코드 봇 정리   5. nginx 다시 읽기   6. 빌드배포 + 고정 멘트 녹음 확인" -ForegroundColor DarkGray
Write-Host "  ($AppDir)" -ForegroundColor DarkGray

# ---------- 1. VoiceStudio ----------
Step "1/6" "VoiceStudio 확인"
if (Test-VoiceStudio) {
    Say "  VoiceStudio가 이미 켜져 있어요." "Green"
} else {
    $lnk = Find-VsShortcut
    if (-not $lnk) { Install-VoiceStudio; $lnk = Find-VsShortcut }
    if (-not $lnk) { throw "VoiceStudio 바로가기를 찾지 못했어요. 시작 메뉴에서 VoiceStudio를 직접 실행한 뒤 이 파일을 다시 실행해 주세요." }
    Say "  VoiceStudio를 실행해요. (처음 실행하면 음성 모델을 받느라 오래 걸릴 수 있어요)"
    Start-VoiceStudio $lnk
    while (-not (Wait-VoiceStudio 180)) {
        if ((Ask "  아직 안 켜졌어요. 앱 화면에서 준비가 끝날 때까지 더 기다릴까요? (Y=더 기다리기 / N=그만)") -notmatch "^[Yy]") {
            throw "VoiceStudio가 켜지지 않았어요. 앱이 완전히 켜진 뒤 이 파일을 다시 실행해 주세요."
        }
    }
}
$lnk = Find-VsShortcut
if ($lnk) {
    $startup = [Environment]::GetFolderPath("Startup")
    Copy-Item $lnk (Join-Path $startup "VoiceStudio.lnk") -Force
    Say "  PC를 켤 때 VoiceStudio가 자동으로 실행되게 등록했어요. (루미 목소리는 VoiceStudio가 켜져 있어야 만들어져요)" "DarkGray"
}

# ---------- 2. 목소리 고르기 ----------
Step "2/6" "루미 목소리 고르기"
$current = Get-EnvValue "VOICESTUDIO_VOICE"
$chosen = $null
while (-not $chosen) {
    $profiles = Get-Profiles
    if ($profiles.Count -eq 0) {
        Say "  VoiceStudio에 저장된 목소리가 아직 없어요." "Yellow"
    } else {
        Say "  VoiceStudio에 저장된 목소리:"
        $def = 0
        for ($i = 0; $i -lt $profiles.Count; $i++) {
            $p = $profiles[$i]
            $mark = ""
            if ($p.Id -eq $current) { $mark = "  <- 지금 쓰는 목소리"; $def = $i + 1 }
            elseif ($def -eq 0 -and $p.Name -match "루미|lumi") { $def = $i + 1 }
            Say ("   {0,2}. {1}  ({2}){3}" -f ($i + 1), $p.Name, $p.Id, $mark)
        }
    }
    Say ""
    Say "  새 목소리가 필요하면: VoiceStudio 앱 → 목소리 만들기(Voice Design)에서" "DarkGray"
    Say "    '밝고 장난스러운 10대 후반 여자, 친근한 반말' 같은 느낌으로 만들고 이름을 '루미'로 저장하세요." "DarkGray"
    $hint = if ($profiles.Count -gt 0) { "번호 입력" + $(if ($def) { " (Enter = ${def}번)" } else { "" }) + " / R = 목록 새로고침 / M = ID 직접 입력" } else { "앱에서 목소리를 만든 뒤 Enter (목록 새로고침) / M = ID 직접 입력" }
    $ans = Ask "  $hint :"
    if ($ans -match "^[Mm]$") { $chosen = (Ask "  목소리 ID:").Trim(); if (-not $chosen) { $chosen = $null }; continue }
    if ($ans -match "^[Rr]?$" -and -not ($ans -eq "" -and $def)) { continue }
    $n = 0
    if ($ans -eq "" -and $def) { $n = $def } elseif (-not [int]::TryParse($ans, [ref]$n)) { continue }
    if ($n -lt 1 -or $n -gt $profiles.Count) { Say "  목록에 없는 번호예요." "Yellow"; continue }
    $cand = $profiles[$n - 1]
    Say "  '$($cand.Name)' 목소리로 시험해 볼게요. 소리가 나는지 들어 보세요."
    try {
        Test-Voice $cand.Id | Out-Null
        if ((Ask "  이 목소리로 할까요? (Y = 확정 / N = 다른 목소리)") -match "^[Yy]") { $chosen = $cand.Id }
    } catch {
        Say "  시험 음성 만들기 실패: $($_.Exception.Message)" "Red"
        if ((Ask "  그래도 이 목소리로 저장할까요? (Y/N)") -match "^[Yy]") { $chosen = $cand.Id }
    }
}

# ---------- 3. .env ----------
Step "3/6" ".env 에 저장"
Set-EnvValue "VOICESTUDIO_VOICE" $chosen
if ($VsBase -ne "http://127.0.0.1:3900") { Set-EnvValue "VOICESTUDIO_URL" $VsBase; Say "  VOICESTUDIO_URL=$VsBase" "Green" }
Say "  VOICESTUDIO_VOICE=$chosen  (예전 .env 는 .env.bak_lumivoice 로 백업)" "Green"

# ---------- 4. 디스코드 봇 정리 ----------
Step "4/6" "예전 디스코드 봇 작업 정리"
cmd.exe /d /c "schtasks /end /tn PastelliveDiscordBot >nul 2>&1 & schtasks /delete /tn PastelliveDiscordBot /f >nul 2>&1" | Out-Null
Get-Process discord-bot -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Say "  정리 완료 (디스코드 로그인은 그대로예요)." "Green"

# ---------- 5. nginx ----------
Step "5/6" "nginx 설정 다시 읽기 (팬 갤러리 사진 캐시)"
$NginxDir = "C:\nginx"; $exe = Join-Path $NginxDir "nginx.exe"; $conf = Join-Path $NginxDir "conf\nginx.conf"
if (Test-Path $exe) {
    $test = cmd.exe /d /c "`"$exe`" -p `"$NginxDir`" -c `"$conf`" -t 2>&1"
    if ($LASTEXITCODE -ne 0) {
        Say "  nginx 설정 검사 실패 → 원래 설정으로 되돌릴게요." "Yellow"
        Say ($test | Out-String) "DarkGray"
        if (Test-Path "$conf.bak_fanartcache") { Copy-Item "$conf.bak_fanartcache" $conf -Force }
    } else {
        $t = "LuminousNginxReload"
        $null = schtasks.exe /create /tn $t /tr "`"$exe`" -p `"$NginxDir`" -c `"$conf`" -s reload" /sc once /st 00:00 /ru SYSTEM /rl HIGHEST /f
        $null = schtasks.exe /run /tn $t
        Start-Sleep -Seconds 3
        $null = schtasks.exe /delete /tn $t /f
        Say "  nginx 다시 읽기 완료." "Green"
    }
} else { Say "  C:\nginx 가 없어서 건너뜀." "Yellow" }

# ---------- 6. 빌드 + 배포 ----------
Step "6/6" "루미너스 서버 빌드 + 다시 시작 (빌드배포.bat)"
$build = Join-Path $AppDir "scripts\빌드배포.bat"
& cmd.exe /d /c "`"$build`" __UTF8RUN__"
Say ""
Say "  서버가 켜지기를 기다리는 중..."
$ok = $false
for ($i = 0; $i -lt 30; $i++) {
    try { $s = Invoke-RestMethod "$SiteBase/api/lumi/voice/status" -TimeoutSec 3; if ($s.enabled) { $ok = $true; break } } catch { }
    Start-Sleep -Seconds 2
}
if (-not $ok) {
    Say "  서버에서 루미 목소리가 아직 켜지지 않았어요. logs\service.log 를 확인해 주세요." "Yellow"
} else {
    Say "  루미 목소리 켜짐! 고정 멘트를 미리 녹음하는 중이에요 (최대 몇 분)..." "Green"
    $dir = Join-Path $AppDir "static\lumi_voice"
    for ($i = 0; $i -lt 60; $i++) {
        $n = @(Get-ChildItem $dir -Filter *.mp3 -ErrorAction SilentlyContinue).Count
        Write-Host -NoNewline "`r  녹음된 멘트: $n 개   "
        if ($n -ge 10) { break }
        Start-Sleep -Seconds 5
    }
    Write-Host ""
    Say "  사이트에서 루미에게 말을 걸고 답변 옆 🔊 버튼을 눌러 보세요." "Green"
}
