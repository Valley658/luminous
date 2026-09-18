$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "Requesting administrator privileges..."
    Start-Process powershell -Verb RunAs -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "`"$PSCommandPath`"")
    exit
}

function Invoke-FixRdp {
    Write-Host "[1/6] Enabling Remote Desktop in registry..."
    Set-ItemProperty -Path "HKLM:\System\CurrentControlSet\Control\Terminal Server" -Name "fDenyTSConnections" -Value 0 -Type DWord

    Write-Host "[2/6] Requiring Network Level Authentication..."
    Set-ItemProperty -Path "HKLM:\System\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp" -Name "UserAuthentication" -Value 1 -Type DWord

    Write-Host "[3/6] Forcing RDP to use software rendering instead of the GPU..."
    $tsPolicyPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services"
    if (-not (Test-Path $tsPolicyPath)) {
        New-Item -Path $tsPolicyPath -Force | Out-Null
    }
    Set-ItemProperty -Path $tsPolicyPath -Name "bEnumerateHWBeforeSW" -Value 0 -Type DWord

    Write-Host "[4/6] Enabling firewall rules for Remote Desktop..."
    $rules = Get-NetFirewallRule -Group "@FirewallAPI.dll,-28752" -ErrorAction SilentlyContinue
    if ($rules) {
        $rules | Enable-NetFirewallRule
        Write-Host ("  Enabled " + $rules.Count + " Remote Desktop firewall rule(s).")
    } else {
        Write-Host "  [Warn] Could not find the built-in Remote Desktop firewall rule group."
    }

    Write-Host "[5/6] Making sure the Remote Desktop service is running and set to auto-start..."
    Set-Service -Name "TermService" -StartupType Automatic
    Restart-Service -Name "TermService" -Force

    Write-Host "[6/6] Verifying..."
    Start-Sleep -Seconds 2
    $listening = Get-NetTCPConnection -LocalPort 3389 -State Listen -ErrorAction SilentlyContinue
    if ($listening) {
        Write-Host "  OK - port 3389 is listening."
    } else {
        Write-Host "  [Warn] Port 3389 is not listening yet - a reboot may be required."
    }
    Get-Service TermService | Format-List Name, Status, StartType

    Write-Host ""
    Write-Host "Done. Reconnect via RDP - GPU 'please wait' delay should be gone"
    Write-Host "(a full reboot guarantees it if it still appears)."
    Write-Host "Current IP addresses:"
    Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike "127.*" } | Select-Object InterfaceAlias, IPAddress | Format-Table -AutoSize
}

function Invoke-SetupFfmpeg {
    $installDir = "C:\ffmpeg"
    $binDir = Join-Path $installDir "bin"
    $ffmpegExe = Join-Path $binDir "ffmpeg.exe"

    if (Test-Path $ffmpegExe) {
        Write-Host "[setup_ffmpeg] Already installed: $ffmpegExe"
        return
    }

    $zipPath = Join-Path $env:TEMP "_ffmpeg_download.zip"
    $extractTmp = Join-Path $env:TEMP "_ffmpeg_extract_tmp"

    if (Test-Path $zipPath) { Remove-Item $zipPath -Force -ErrorAction SilentlyContinue }
    if (Test-Path $extractTmp) { Remove-Item $extractTmp -Recurse -Force -ErrorAction SilentlyContinue }

    $url = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"

    Write-Host "[setup_ffmpeg] Downloading official ffmpeg essentials build..."
    Write-Host "[setup_ffmpeg] URL: $url"

    try {
        Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing
    } catch {
        Write-Host "[setup_ffmpeg] Download failed: $_"
        Write-Host "Manually download from https://www.gyan.dev/ffmpeg/builds/ and extract ffmpeg.exe/ffprobe.exe to $binDir"
        return
    }

    if (-not (Test-Path $zipPath) -or (Get-Item $zipPath).Length -lt 10000000) {
        $sizeInfo = if (Test-Path $zipPath) { "{0:N1} MB" -f ((Get-Item $zipPath).Length / 1MB) } else { "0 MB" }
        Remove-Item $zipPath -Force -ErrorAction SilentlyContinue
        Write-Host "[setup_ffmpeg] Downloaded file size looks wrong (got $sizeInfo). Network may have dropped - try again."
        return
    }

    New-Item -ItemType Directory -Path $extractTmp -Force | Out-Null

    Write-Host "[setup_ffmpeg] Extracting..."
    try {
        Expand-Archive -Path $zipPath -DestinationPath $extractTmp -Force
    } catch {
        Remove-Item $zipPath -Force -ErrorAction SilentlyContinue
        Remove-Item $extractTmp -Recurse -Force -ErrorAction SilentlyContinue
        Write-Host "[setup_ffmpeg] Extraction failed (file may be corrupted): $_"
        Write-Host "Try again to download fresh."
        return
    }

    New-Item -ItemType Directory -Path $binDir -Force | Out-Null

    foreach ($bin in @("ffmpeg.exe", "ffprobe.exe")) {
        $exe = Get-ChildItem -Path $extractTmp -Recurse -Filter $bin | Select-Object -First 1
        if (-not $exe) {
            Write-Host "[setup_ffmpeg] Could not find $bin inside the extracted archive."
            return
        }
        Copy-Item -Path $exe.FullName -Destination (Join-Path $binDir $bin) -Force
        Write-Host "[setup_ffmpeg] Installed: $binDir\$bin"
    }

    Remove-Item $zipPath -Force -ErrorAction SilentlyContinue
    Remove-Item $extractTmp -Recurse -Force -ErrorAction SilentlyContinue

    Write-Host "[setup_ffmpeg] Done. FFMPEG_CMD default (C:\ffmpeg\bin\ffmpeg.exe) will now work with no .env change needed."
}

function Invoke-RemoveNightlySleep {
    $sleepTaskName = "PastelliveNightlySleep"
    $wakeTaskName  = "PastelliveMorningWake"

    foreach ($name in @($sleepTaskName, $wakeTaskName)) {
        $existing = Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
        if ($existing) {
            Unregister-ScheduledTask -TaskName $name -Confirm:$false
            Write-Host "Removed $name"
        } else {
            Write-Host "$name is not registered - nothing to remove"
        }
    }

    Write-Host ""
    Write-Host "Done. PC will no longer hibernate/wake automatically."
    Write-Host "The 01:00-05:00 site-unavailable window is now handled entirely inside"
    Write-Host "app.py (_maintenance_window_gate) - no PC-level scheduled task needed for it."
}

Write-Host "=================================================="
Write-Host " pastellive maintenance tools"
Write-Host "=================================================="
Write-Host "1) Fix RDP (enable + skip GPU wait delay)"
Write-Host "2) Setup ffmpeg (download if missing)"
Write-Host "3) Remove nightly sleep/wake scheduled tasks"
Write-Host "4) Run all of the above"
Write-Host "0) Exit"
Write-Host ""
$choice = Read-Host "Choose an option (0-4)"

switch ($choice) {
    "1" { Invoke-FixRdp }
    "2" { Invoke-SetupFfmpeg }
    "3" { Invoke-RemoveNightlySleep }
    "4" { Invoke-FixRdp; Invoke-SetupFfmpeg; Invoke-RemoveNightlySleep }
    default { Write-Host "Exiting." }
}

Write-Host ""
Write-Host "Press Enter to close..."
Read-Host | Out-Null
