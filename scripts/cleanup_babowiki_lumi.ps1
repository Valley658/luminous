# 바보위키 운영 중단 + 루미 AI 정리 (바보위키_루미AI_정리.bat 이 관리자 권한으로 실행)
param([string]$AppDir = "")
$ErrorActionPreference = "Continue"
[Console]::OutputEncoding = [Text.Encoding]::UTF8
if (-not $AppDir) { $AppDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path) }
function Say($m, $c = "Gray") { Write-Host $m -ForegroundColor $c }
function Step($n, $m) { Write-Host ""; Write-Host "[$n] $m" -ForegroundColor Cyan }
function Remove-Task($name) {
    cmd.exe /d /c "schtasks /query /tn `"$name`" >nul 2>&1"
    if ($LASTEXITCODE -eq 0) {
        cmd.exe /d /c "schtasks /end /tn `"$name`" >nul 2>&1 & schtasks /delete /tn `"$name`" /f >nul 2>&1" | Out-Null
        Say "  예약 작업 '$name' 삭제" "Green"
    } else { Say "  예약 작업 '$name' 없음 (건너뜀)" "DarkGray" }
}
function Stop-ByCmdLine($pattern, $label) {
    $n = 0
    Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -and $_.CommandLine -like $pattern } | ForEach-Object {
        if ($_.ProcessId -ne $PID) { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue; $n++ }
    }
    if ($n -gt 0) { Say "  $label 프로세스 $n개 종료" "Green" }
}
$Desktop = Split-Path -Parent $AppDir
$Wiki = Join-Path $Desktop "babo_wiki"
$stamp = Get-Date -Format "yyyyMMdd_HHmmss"

Write-Host ""
Write-Host "  바보위키 운영 중단 + 루미 AI 정리" -ForegroundColor Magenta

# ---------- 1. 바보위키 서버 멈추기 ----------
Step "1/6" "바보위키 서버 멈추기"
Remove-Task "BaboWiki"
Stop-ByCmdLine "*_run_babowiki_loop*" "바보위키 감시"
Stop-ByCmdLine "*babo_wiki*server.py*" "바보위키 서버"
Get-NetTCPConnection -LocalPort 8070 -State Listen -ErrorAction SilentlyContinue | ForEach-Object {
    Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue; Say "  8070 포트를 쓰던 프로세스 종료" "Green" }

# ---------- 2. 루미 AI 관련 프로그램 멈추기 ----------
Step "2/6" "루미 AI 관련 자동 실행 끄기 (사진 인식, Ollama 자동 실행, VoiceStudio 자동 실행)"
Remove-Task "LumiMemberIDService"
Stop-ByCmdLine "*member-id-service*" "사진 인식"
Remove-Task "OllamaService"
Get-Process ollama, "ollama app", ollama_llama_server -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
foreach ($u in Get-ChildItem "C:\Users" -Directory -ErrorAction SilentlyContinue) {
    $dir = Join-Path $u.FullName "AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup"
    foreach ($f in "VoiceStudio.lnk", "VoiceStudio_지연실행.cmd") {
        $p = Join-Path $dir $f
        if (Test-Path $p) { Remove-Item $p -Force; Say "  $($u.Name): 시작 프로그램에서 $f 제거" "Green" }
    }
}
Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path -like "*VoiceStudio*" } | Stop-Process -Force -ErrorAction SilentlyContinue
Say "  (Ollama, VoiceStudio 프로그램 자체는 지우지 않았어요. 안 쓰면 설정 > 앱에서 제거하세요)" "DarkGray"

# ---------- 3. nginx ----------
Step "3/6" "nginx에서 wiki.pastellive.co.kr 설정 빼고 다시 읽기"
$NginxDir = "C:\nginx"; $exe = Join-Path $NginxDir "nginx.exe"; $conf = Join-Path $NginxDir "conf\nginx.conf"
if (Test-Path $exe) {
    $test = cmd.exe /d /c "`"$exe`" -p `"$NginxDir`" -c `"$conf`" -t 2>&1"
    if ($LASTEXITCODE -ne 0) {
        Say "  nginx 설정 검사 실패 → 바로 전 설정으로 되돌려요." "Yellow"
        Say ($test | Out-String) "DarkGray"
        $bak = Get-ChildItem (Join-Path $NginxDir "conf") -Filter "nginx.conf.bak_remove_babowiki_*" | Sort-Object LastWriteTime -Descending | Select-Object -First 1
        if ($bak) { Copy-Item $bak.FullName $conf -Force }
    } else {
        $t = "LuminousNginxReload"
        $null = schtasks.exe /create /tn $t /tr "`"$exe`" -p `"$NginxDir`" -c `"$conf`" -s reload" /sc once /st 00:00 /ru SYSTEM /rl HIGHEST /f
        $null = schtasks.exe /run /tn $t
        Start-Sleep -Seconds 3
        $null = schtasks.exe /delete /tn $t /f
        Say "  nginx 다시 읽기 완료" "Green"
    }
} else { Say "  C:\nginx 가 없어서 건너뜀" "Yellow" }

# ---------- 4. 바보위키 폴더 정리 ----------
Step "4/6" "babo_wiki 폴더를 바탕화면\_to_delete 로 옮기기"
if (Test-Path $Wiki) {
    $dest = Join-Path $Desktop "_to_delete"
    New-Item -ItemType Directory -Force $dest | Out-Null
    $target = Join-Path $dest "babo_wiki_$stamp"
    Start-Sleep -Seconds 2
    try { Move-Item $Wiki $target -ErrorAction Stop; Say "  옮김: $target" "Green" }
    catch { Say "  옮기지 못했어요(파일을 쓰는 프로그램이 남아 있을 수 있어요): $($_.Exception.Message)" "Yellow"; Say "  PC를 재시작한 뒤 이 파일을 다시 실행하면 옮겨져요." "Yellow" }
} else { Say "  babo_wiki 폴더가 이미 없어요" "DarkGray" }

# ---------- 5. 루미너스 다시 빌드 ----------
Step "5/6" "루미너스 서버 다시 빌드 + 재시작 (루미 AI 빠진 버전)"
$build = Join-Path $AppDir "scripts\빌드배포.bat"
& cmd.exe /d /c "`"$build`" __UTF8RUN__"

# ---------- 6. 직접 해야 할 일 ----------
Step "6/6" "남은 일 (직접)"
Say "  Cloudflare 대시보드 → Zero Trust → Networks → Tunnels → (pastellive 터널) → Public Hostname" "Yellow"
Say "  에서 wiki.pastellive.co.kr 항목을 삭제하세요. (안 지우면 접속 시 루미너스 대신 오류 화면이 떠요)" "Yellow"
Say "  다 지워도 되면 바탕화면의 _to_delete 폴더를 직접 지우세요." "DarkGray"
Say ""
Say "  끝!" "Green"
