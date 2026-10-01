# VoiceStudio 진단 - 결과를 logs\voicestudio_diag.txt 에 저장 (관리자 권한 필요 없음)
$ErrorActionPreference = "Continue"
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$out = Join-Path $root "logs\voicestudio_diag.txt"
New-Item -ItemType Directory -Force (Split-Path $out) | Out-Null
$lines = New-Object System.Collections.Generic.List[string]
function L($s) { $lines.Add([string]$s); Write-Host $s }
function Try-Req($name, [scriptblock]$sb) {
    $t = [Diagnostics.Stopwatch]::StartNew()
    try { $r = & $sb; L ("[{0}] OK {1:n1}s  {2}" -f $name, $t.Elapsed.TotalSeconds, $r) }
    catch {
        $body = ""
        try { $sr = New-Object IO.StreamReader($_.Exception.Response.GetResponseStream()); $body = $sr.ReadToEnd() } catch { }
        L ("[{0}] FAIL {1:n1}s  {2}  {3}" -f $name, $t.Elapsed.TotalSeconds, $_.Exception.Message, $body)
    }
}
L ("== " + (Get-Date))
foreach ($port in 3900, 3902) {
    Try-Req "well-known:$port" { (Invoke-WebRequest "http://127.0.0.1:$port/.well-known/voicestudio-speech" -UseBasicParsing -TimeoutSec 5).Content }
}
$B = "http://127.0.0.1:3900"
Try-Req "models" { $r = Invoke-WebRequest "$B/v1/models" -UseBasicParsing -TimeoutSec 10; [Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray()) }
Try-Req "profiles" { $r = Invoke-WebRequest "$B/profiles" -UseBasicParsing -TimeoutSec 10; [Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray()).Substring(0, 300) }
Try-Req "health" { $r = Invoke-WebRequest "$B/health" -UseBasicParsing -TimeoutSec 10; [Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray()) }
$voice = ""
$envPath = Join-Path $root ".env"
foreach ($l in [IO.File]::ReadAllLines($envPath, [Text.Encoding]::UTF8)) { if ($l -match "^VOICESTUDIO_VOICE=(.*)$") { $voice = $Matches[1].Trim() } }
if (-not $voice) { $voice = "cbea135c" }
foreach ($case in @(
    @{ n = "speech-default-wav"; b = @{ model = "tts-1"; input = "안녕"; voice = "default"; response_format = "wav" } },
    @{ n = "speech-lumi-wav";    b = @{ model = "tts-1"; input = "안녕"; voice = $voice; response_format = "wav"; language = "ko" } },
    @{ n = "speech-lumi-mp3";    b = @{ model = "tts-1"; input = "안녕"; voice = $voice; response_format = "mp3"; language = "ko" } })) {
    $json = $case.b | ConvertTo-Json
    Try-Req $case.n { $r = Invoke-WebRequest "$B/v1/audio/speech" -Method Post -ContentType "application/json; charset=utf-8" -Body ([Text.Encoding]::UTF8.GetBytes($json)) -UseBasicParsing -TimeoutSec 120; "{0} bytes, {1}" -f $r.RawContentLength, $r.Headers["Content-Type"] }
}
L "== GPU"
try { (Get-CimInstance Win32_VideoController | ForEach-Object { "$($_.Name)  $([math]::Round($_.AdapterRAM/1GB,1))GB" }) | ForEach-Object { L $_ } } catch { }
L "== omnivoice.log (last 80)"
$log = Join-Path $env:APPDATA "OmniVoice\omnivoice.log"
if (Test-Path $log) { Get-Content $log -Tail 80 -Encoding UTF8 | ForEach-Object { L $_ } } else { L "(no log at $log)" }
[IO.File]::WriteAllLines($out, $lines, (New-Object Text.UTF8Encoding($false)))
Write-Host ""; Write-Host "저장됨: $out" -ForegroundColor Green
