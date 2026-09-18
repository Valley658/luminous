@echo off
setlocal enabledelayedexpansion
title Pastellive Windows Deploy Tool

net session >nul 2>&1
if not "%errorlevel%"=="0" (
    echo Requesting administrator privileges...
    if "%~1"=="" (
        powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    ) else (
        powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -ArgumentList '%*' -Verb RunAs"
    )
    exit /b
)

if "%~1"=="1" (call :install_pastellive_app & exit /b)
if "%~1"=="2" (call :install_pastellive_discord_bot & exit /b)
if "%~1"=="3" (call :install_nginx & exit /b)
if "%~1"=="4" (call :install_meilisearch & exit /b)
if "%~1"=="5" (call :install_cloudflared "%~2" & exit /b)
if "%~1"=="6" (call :install_everything "%~2" & exit /b)
if "%~1"=="7" (call :diagnose_localhost & exit /b)
if "%~1"=="8" (call :setup_log_rotation "%~2" & exit /b)
if "%~1"=="9" (call :tune_mysql "%~2" & exit /b)
if "%~1"=="11" (call :restore_db_if_needed & exit /b)
if "%~1"=="14" (call :install_go_server & exit /b)
if "%~1"=="15" (call :apply_go_update & exit /b)
if "%~1"=="16" (call :rollback_to_python & exit /b)
if "%~1"=="17" (call :install_cimage_prod & exit /b)
if "%~1"=="18" (call :install_cimage_test & exit /b)
if "%~1"=="20" (call :apply_cimg_update & exit /b)
if "%~1"=="21" (call :install_nsfw_service & exit /b)
if "%~1"=="22" (call :install_phash_service & exit /b)
if "%~1"=="__run_auto" (call :auto_run & exit /b)
if "%~1"=="menu" goto menu
if "%~1"=="" goto auto_start
goto menu

:auto_start
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
if not exist "%APPDIR%\logs" mkdir "%APPDIR%\logs" >nul 2>&1
for /f "delims=" %%T in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set "RUNSTAMP=%%T"
set "RUNLOG=%APPDIR%\logs\install_all_run_%RUNSTAMP%.log"
echo Running automatic setup/check. Full output is being saved to:
echo   %RUNLOG%
call "%~f0" __run_auto 2>&1 | powershell -NoProfile -Command "$input | Tee-Object -FilePath '%RUNLOG%'"
echo Done. Full log saved to: %RUNLOG%
pause
exit /b 0

:auto_run
setlocal enabledelayedexpansion
set "AUTO_SILENT=1"
echo Pastellive automatic setup/check - %date% %time%

echo [1/8] DB schema check (auto-restore from local backup if empty)
call :restore_db_if_needed "silent"

echo [2/9] pastellive app - SKIPPED (PastelliveApp is now Go - see install_all.bat 14).
echo   Old-Python/ has been fully removed (rollback via install_all.bat 16 no longer
echo   works - the target folder is gone) - NOT auto-redeployed here anymore.

echo [3/9] pastellive discord bot (Go - discord-bot.exe)
call :do_install_discord_bots

echo [5/9] nginx (port 80)
powershell -NoProfile -Command "exit [int](-not (Get-NetTCPConnection -LocalPort 80 -State Listen -ErrorAction SilentlyContinue))" >nul 2>&1
if errorlevel 1 (
    echo   not listening - installing/starting...
    call :install_nginx
) else (
    echo   already up, skipping.
)

echo [6/9] Meilisearch (port 7700)
powershell -NoProfile -Command "exit [int](-not (Get-NetTCPConnection -LocalPort 7700 -State Listen -ErrorAction SilentlyContinue))" >nul 2>&1
if errorlevel 1 (
    echo   not listening - installing/starting...
    call :install_meilisearch
) else (
    echo   already up, skipping.
)

echo [7/9] Cloudflared tunnel service
sc query cloudflared >nul 2>&1
if errorlevel 1 (
    echo   not installed - skipped, needs a tunnel token, can't run unattended.
    echo   Run "install_all.bat 5 YOUR_TOKEN" once to set it up.
) else (
    echo   already installed, skipping.
)

echo [8/10] Redis + RQ background worker - removed (dead code, see :setup_redis_and_worker)
call :setup_redis_and_worker "silent"

echo [9/10] MySQL tuning (innodb_buffer_pool_size + slow query log)
call :tune_mysql "silent"

echo [10/10] Log rotation
call :setup_log_rotation "silent"

echo Automatic setup/check finished.
exit /b 0

:menu
cls
echo   Pastellive Windows Deploy Tool
echo  [1] Install/start pastellive app service
echo  [2] Install/start pastellive discord bot (Go - ops bot)
echo  [3] Install/start nginx service (nginx.exe must already be installed)
echo  [4] Install/start Meilisearch service
echo  [5] Install Cloudflared tunnel service (token required)
echo  [6] Install/start EVERYTHING (1,2,3,4 + Cloudflared)
echo  [7] Diagnose: why isn't http://localhost loading?
echo  [8] Set up/run automatic log rotation
echo  [9] Tune MySQL (innodb_buffer_pool_size + slow query log)
echo  [11] Restore DB from newest local backup (only if schema looks empty)
echo  [13] Redis + RQ background worker - removed, see :setup_redis_and_worker
echo  ---- Go server / c-image-service management (manual only, not part of [6]) ----
echo  [14] Go server cutover (PastelliveApp: Python -^> Go)
echo  [15] Apply new Go server build (swap in pastellive-server.new.exe)
echo  [16] Rollback PastelliveApp to Python (emergency)
echo  [17] c-image-service PRODUCTION cutover (port 8091, replaces java)
echo  [18] c-image-service TEST install (port 8092, side-by-side)
echo  [20] Apply new c-image-service build (swap in pastellive-image-service.new.exe)
echo  [21] Install/start nsfw-service (fanart/community AI auto-moderation, port 8095)
echo  [22] Install/start phash-service (fanart 재업로드/도용 탐지, port 8096)
echo  [0] Exit
set /p CHOICE="Choose and press Enter: "

if "%CHOICE%"=="1" (call :install_pastellive_app & goto menu)
if "%CHOICE%"=="2" (call :install_pastellive_discord_bot & goto menu)
if "%CHOICE%"=="3" (call :install_nginx & goto menu)
if "%CHOICE%"=="4" (call :install_meilisearch & goto menu)
if "%CHOICE%"=="5" goto ask_cloudflared
if "%CHOICE%"=="7" (call :diagnose_localhost & goto menu)
if "%CHOICE%"=="8" (call :setup_log_rotation & goto menu)
if "%CHOICE%"=="9" (call :tune_mysql & goto menu)
if "%CHOICE%"=="11" (call :restore_db_if_needed & goto menu)
if "%CHOICE%"=="13" (call :setup_redis_and_worker & goto menu)
if "%CHOICE%"=="14" (call :install_go_server & goto menu)
if "%CHOICE%"=="15" (call :apply_go_update & goto menu)
if "%CHOICE%"=="16" (call :rollback_to_python & goto menu)
if "%CHOICE%"=="17" (call :install_cimage_prod & goto menu)
if "%CHOICE%"=="18" (call :install_cimage_test & goto menu)
if "%CHOICE%"=="20" (call :apply_cimg_update & goto menu)
if "%CHOICE%"=="21" (call :install_nsfw_service & goto menu)
if "%CHOICE%"=="22" (call :install_phash_service & goto menu)
if "%CHOICE%"=="6" goto do_everything
if "%CHOICE%"=="0" exit /b 0
echo Invalid choice.
pause
goto menu

:ask_cloudflared
set /p CF_TOKEN="Enter Cloudflare Tunnel token (Zero Trust dashboard -> Networks -> Tunnels): "
if "%CF_TOKEN%"=="" (
    echo No token entered, cancelled.
    pause
    goto menu
)
call :install_cloudflared "%CF_TOKEN%"
goto menu

:do_everything
set /p CF_TOKEN="Enter Cloudflare Tunnel token (Zero Trust dashboard -> Networks -> Tunnels, leave blank to skip Cloudflared): "
call :install_everything "%CF_TOKEN%"
goto menu

:install_everything
setlocal
set "ALL_TOKEN=%~1"
echo [1/5] pastellive app service
call :install_pastellive_app
echo [2/5] pastellive discord bot (Go - ops bot)
call :install_pastellive_discord_bot
echo [3/5] nginx service
call :install_nginx
echo [4/5] Meilisearch service
call :install_meilisearch
if "%ALL_TOKEN%"=="" (
    echo [5/5] Cloudflared skipped (no token given)
) else (
    echo [5/5] Cloudflared tunnel service
    call :install_cloudflared "%ALL_TOKEN%"
)
echo All done.
pause
exit /b 0

:diagnose_localhost
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"

echo [1/7] Scheduled task status (which folder does the task actually point to?)
echo -- PastelliveApp --
schtasks /query /tn "PastelliveApp" /fo list /v 2>nul || echo   NOT REGISTERED - run option [1] first.
echo -- NginxStartup --
schtasks /query /tn "NginxStartup" /fo list /v 2>nul || echo   NOT REGISTERED - run option [3] first.

echo [2/7] This script's own folder (what option [1]/[3] WOULD register now)
echo   APPDIR = %APPDIR%

echo [3/7] Exact command line of every running python.exe / nginx.exe
echo (look for a path that does NOT match APPDIR above - that is a stale/
echo  orphaned process from a different folder or a different Windows user)
powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \"name='python.exe' or name='nginx.exe'\" | Select-Object ProcessId, Name, CommandLine | Format-List"

echo [3b/7] MySQL/MariaDB status (app.py needs 127.0.0.1:3306 to start)
powershell -NoProfile -Command "$svc = Get-Service | Where-Object { $_.Name -like '*mysql*' -or $_.Name -like '*maria*' }; if ($svc) { $svc | Select-Object Name, Status, DisplayName | Format-Table -AutoSize } else { Write-Host '  No MySQL/MariaDB Windows service found (not installed, or installed without a service).' }"
tasklist /fi "imagename eq mysqld.exe" /fo table
powershell -NoProfile -Command "$p3306 = Get-NetTCPConnection -LocalPort 3306 -State Listen -ErrorAction SilentlyContinue; if ($p3306) { Write-Host '  Port 3306: LISTENING' } else { Write-Host '  Port 3306: NOT LISTENING' }"

echo [4/7] Is anything listening on the expected ports?
powershell -NoProfile -Command "$p8080 = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue; if ($p8080) { Write-Host ('  Port 8080 (pastellive app): LISTENING, owned by PID ' + ($p8080 | Select-Object -First 1 -ExpandProperty OwningProcess)) } else { Write-Host '  Port 8080 (pastellive app): NOT LISTENING' }; $p80 = Get-NetTCPConnection -LocalPort 80 -State Listen -ErrorAction SilentlyContinue; if ($p80) { Write-Host ('  Port 80 (nginx): LISTENING, owned by PID ' + ($p80 | Select-Object -First 1 -ExpandProperty OwningProcess)) } else { Write-Host '  Port 80 (nginx): NOT LISTENING' }"

echo [5/7] Are the processes actually running?
echo -- python.exe (pastellive app) --
tasklist /fi "imagename eq python.exe" /fo table
echo -- nginx.exe --
tasklist /fi "imagename eq nginx.exe" /fo table

echo [6/7] Direct HTTP test
echo -- http://127.0.0.1:8080/  (app directly, bypasses nginx) --
powershell -NoProfile -Command "try { $r = Invoke-WebRequest -Uri 'http://127.0.0.1:8080/' -UseBasicParsing -TimeoutSec 5; Write-Host ('  OK - HTTP ' + $r.StatusCode) } catch { Write-Host ('  FAILED - ' + $_.Exception.Message) }"
echo -- http://localhost/  (through nginx) --
powershell -NoProfile -Command "try { $r = Invoke-WebRequest -Uri 'http://localhost/' -UseBasicParsing -TimeoutSec 5; Write-Host ('  OK - HTTP ' + $r.StatusCode) } catch { Write-Host ('  FAILED - ' + $_.Exception.Message) }"

echo [7/7] Recent log lines
echo -- %APPDIR%\logs\service.log (last 15 lines) --
if exist "%APPDIR%\logs\service.log" (
    powershell -NoProfile -Command "Get-Content -Path '%APPDIR%\logs\service.log' -Tail 15"
) else (
    echo   not found yet - app hasn't been started via option [1]
)
echo -- C:\nginx\logs\error.log (last 15 lines) --
if exist "C:\nginx\logs\error.log" (
    powershell -NoProfile -Command "Get-Content -Path 'C:\nginx\logs\error.log' -Tail 15"
) else (
    echo   not found yet - nginx hasn't been started via option [3]
)

echo Diagnosis summary:
echo  - If port 8080 is NOT LISTENING: the pastellive app never started.
echo    Check service.log above for the actual Python error, fix it, then
echo    run option [1] again.
echo  - If port 8080 IS LISTENING but port 80 is NOT: nginx isn't installed
echo    or failed to start. Run option [3] and read its output/error.log.
echo  - If both ports ARE LISTENING but the HTTP test still FAILED: check
echo    Windows Firewall isn't blocking inbound connections on 80/8080.
pause
exit /b 0

:setup_log_rotation
setlocal enabledelayedexpansion
set "ROT_SILENT=%~1"
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "MAX_SIZE_BYTES=20971520"
set "KEEP_COUNT=14"
set "LOGROT_TASK_NAME=PastelliveLogRotation"

echo Log rotation

schtasks /query /tn "%LOGROT_TASK_NAME%" >nul 2>&1
if errorlevel 1 (
    echo Registering daily scheduled task "%LOGROT_TASK_NAME%" ^(03:30^)...
    schtasks /create /tn "%LOGROT_TASK_NAME%" /tr "\"%~f0\" 8 silent" /sc daily /st 03:30 /ru SYSTEM /rl HIGHEST /f >nul
) else (
    echo Scheduled task "%LOGROT_TASK_NAME%" already registered, skipping.
)

call :rotate_stop_start "PastelliveApp" "%APPDIR%\logs" "service.log"
call :rotate_stop_start "PastelliveDiscordBot" "%APPDIR%\logs" "discord_bot.log"
call :rotate_stop_start "Meilisearch" "C:\meilisearch\logs" "meilisearch.log"
call :rotate_nginx_reopen "C:\nginx\logs" "error.log"
call :rotate_nginx_reopen "C:\nginx\logs" "timing.log"

echo Done.
if not "%ROT_SILENT%"=="silent" pause
exit /b 0

:rotate_stop_start
setlocal
set "TNAME=%~1"
set "LDIR=%~2"
set "LFILE=%~3"
set "LPATH=%LDIR%\%LFILE%"

if not exist "%LPATH%" (
    echo   %LFILE%: not found, skipping.
    goto :rss_done
)

set "LSIZE=0"
for %%F in ("%LPATH%") do set "LSIZE=%%~zF"
if %LSIZE% LSS %MAX_SIZE_BYTES% (
    echo   %LFILE%: %LSIZE% bytes, under limit, skipping.
    goto :rss_done
)

echo   %LFILE%: %LSIZE% bytes, over limit - rotating ^(stopping %TNAME% briefly^)...
schtasks /end /tn "%TNAME%" >nul 2>&1
timeout /t 2 /nobreak >nul

for /f "delims=" %%T in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set "STAMP=%%T"
move /y "%LPATH%" "%LDIR%\%LFILE%.%STAMP%" >nul

schtasks /run /tn "%TNAME%" >nul 2>&1
echo   %LFILE%: rotated to %LFILE%.%STAMP%, %TNAME% restarted.

call :prune_old "%LDIR%" "%LFILE%.*"

:rss_done
endlocal
exit /b 0

:rotate_nginx_reopen
setlocal
set "LDIR=%~1"
set "LFILE=%~2"
set "LPATH=%LDIR%\%LFILE%"

if not exist "%LPATH%" (
    echo   %LFILE%: not found, skipping.
    goto :rnr_done
)

set "LSIZE=0"
for %%F in ("%LPATH%") do set "LSIZE=%%~zF"
if %LSIZE% LSS %MAX_SIZE_BYTES% (
    echo   %LFILE%: %LSIZE% bytes, under limit, skipping.
    goto :rnr_done
)

echo   %LFILE%: %LSIZE% bytes, over limit - rotating ^(nginx -s reopen, no downtime^)...
for /f "delims=" %%T in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set "STAMP=%%T"
move /y "%LPATH%" "%LDIR%\%LFILE%.%STAMP%" >nul
"C:\nginx\nginx.exe" -p "C:\nginx" -s reopen >nul 2>&1
echo   %LFILE%: rotated to %LFILE%.%STAMP%, nginx told to reopen.

call :prune_old "%LDIR%" "%LFILE%.*"

:rnr_done
endlocal
exit /b 0

:prune_old
setlocal enabledelayedexpansion
set "PDIR=%~1"
set "PPAT=%~2"
set "COUNT=0"
for /f "delims=" %%A in ('dir /b /o-d "%PDIR%\%PPAT%" 2^>nul') do (
    set /a COUNT+=1
    if !COUNT! GTR %KEEP_COUNT% del /f /q "%PDIR%\%%A" >nul 2>&1
)
endlocal
exit /b 0

:install_pastellive_app
echo   PastelliveApp is Go now - use option [14] to cut over to Go,
echo   or [16] to roll back to Python (install_all.bat 14 / 16).
exit /b 0

:install_pastellive_discord_bot
call :do_install_discord_bots
exit /b 0

:install_pastellive_rq_worker
set "IPR_SILENT="
if "%AUTO_SILENT%"=="1" set "IPR_SILENT=silent"
call :setup_redis_and_worker "%IPR_SILENT%"
exit /b 0

:install_nginx
setlocal enabledelayedexpansion
set "NGINX_DIR=C:\nginx"
set "NGINX_EXE=%NGINX_DIR%\nginx.exe"
set "TEMPLATE=%~dp0..\nginx\nginx.conf.template"
set "OUT_CONF=%NGINX_DIR%\conf\nginx.conf"
for %%I in ("%~dp0..\..") do set "PASTELLIVE_DIR=%%~fI"
set "STATIC_DIR=%PASTELLIVE_DIR%\static"
set "STATIC_DIR=%STATIC_DIR:\=/%"
set "PROJECT_DIR_SLASH=%PASTELLIVE_DIR:\=/%"

if not exist "%NGINX_EXE%" (
    echo [Error] %NGINX_EXE% not found.
    echo   Download nginx/Windows-1.30.4 ^(Stable^) from http://nginx.org/en/download.html
    echo   and unzip it so nginx.exe ends up at exactly that path, then re-run this script.
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)

echo [1/5] Creating required folders...
if not exist "%NGINX_DIR%\proxy_cache" mkdir "%NGINX_DIR%\proxy_cache"
if not exist "%NGINX_DIR%\logs" mkdir "%NGINX_DIR%\logs"

echo [2/5] Generating nginx.conf from nginx.conf.template...
if not exist "%TEMPLATE%" (
    echo [Error] Template not found: %TEMPLATE%
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)
powershell -NoProfile -Command "(Get-Content -Raw '%TEMPLATE%') -replace [regex]::Escape('__STATIC_DIR__'), '%STATIC_DIR%' -replace [regex]::Escape('__PROJECT_DIR__'), '%PROJECT_DIR_SLASH%' | Set-Content -NoNewline '%OUT_CONF%'"
if not exist "%OUT_CONF%" (
    echo [Error] Failed to generate %OUT_CONF%
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)
echo   Wrote %OUT_CONF%

echo [3/5] Testing config...
"%NGINX_EXE%" -p "%NGINX_DIR%" -c "%OUT_CONF%" -t
if errorlevel 1 (
    echo [Error] nginx config test failed - fix the errors above before continuing.
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)

echo [4/5] Writing watchdog script...
> "%NGINX_DIR%\ensure_nginx_running.bat" (
    echo @echo off
    echo powershell -NoProfile -Command "if (-not (Get-NetTCPConnection -LocalPort 80 -State Listen -ErrorAction SilentlyContinue)) { exit 1 } else { exit 0 }"
    echo if errorlevel 1 ^(
    echo     echo [%%date%% %%time%%] port 80 not listening - starting nginx ^>^> "%NGINX_DIR%\logs\watchdog.log"
    echo     "%NGINX_EXE%" -p "%NGINX_DIR%" -c "%OUT_CONF%"
    echo ^)
)

echo [5/5] Registering scheduled tasks and starting nginx...
schtasks /end /tn "NginxStartup" >nul 2>&1
schtasks /delete /tn "NginxStartup" /f >nul 2>&1
schtasks /delete /tn "NginxWatchdog" /f >nul 2>&1
taskkill /F /IM nginx.exe /T >nul 2>&1
timeout /t 1 /nobreak >nul
schtasks /create /tn "NginxStartup" /tr "\"%NGINX_EXE%\" -p \"%NGINX_DIR%\" -c \"%OUT_CONF%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
schtasks /create /tn "NginxWatchdog" /tr "\"%NGINX_DIR%\ensure_nginx_running.bat\"" /sc minute /mo 2 /ru SYSTEM /rl HIGHEST /f
schtasks /run /tn "NginxStartup"

echo Done. nginx should be listening on port 80 within a few seconds.
echo Check with:   tasklist /fi "imagename eq nginx.exe"
echo Reload conf:  "%NGINX_EXE%" -p "%NGINX_DIR%" -s reload
echo Stop:         "%NGINX_EXE%" -p "%NGINX_DIR%" -s stop
echo Logs:         %NGINX_DIR%\logs\
echo Still needed: point Cloudflare Tunnel's Public Hostname routes at
echo http://localhost:80 (or wherever cloudflared runs on this box).
echo NOTE (2026-09-09): status.pastellive.co.kr and errors.pastellive.co.kr were
echo removed (Uptime Kuma + the /system-errors page are both gone) - if either
echo still has a Zero Trust Public Hostname route, remove it there too.
echo n8n.pastellive.co.kr / dashboard.pastellive.co.kr routes may still be stale;
echo check separately before removing those.
if not "%AUTO_SILENT%"=="1" pause
exit /b 0

:install_meilisearch
setlocal enabledelayedexpansion
set "MEILI_DIR=C:\meilisearch"
set "MEILI_EXE=%MEILI_DIR%\meilisearch.exe"
set "MEILI_VERSION=v1.51.0"
set "MEILI_URL=https://github.com/meilisearch/meilisearch/releases/download/%MEILI_VERSION%/meilisearch-windows-amd64.exe"
set "TASK_NAME=Meilisearch"
set "ENV_FILE=%~dp0..\..\.env"

echo [1/6] Creating C:\meilisearch folders...
if not exist "%MEILI_DIR%" mkdir "%MEILI_DIR%"
if not exist "%MEILI_DIR%\data" mkdir "%MEILI_DIR%\data"
if not exist "%MEILI_DIR%\logs" mkdir "%MEILI_DIR%\logs"

echo [2/6] Downloading meilisearch.exe (%MEILI_VERSION%) if missing...
if exist "%MEILI_EXE%" (
    echo   Already present: %MEILI_EXE%
) else (
    where curl >nul 2>&1
    if not errorlevel 1 (
        curl -L -o "%MEILI_EXE%" "%MEILI_URL%"
    ) else (
        powershell -NoProfile -Command "Invoke-WebRequest -Uri '%MEILI_URL%' -OutFile '%MEILI_EXE%'"
    )
    if not exist "%MEILI_EXE%" (
        echo [Error] Download failed. Manually download from:
        echo   https://github.com/meilisearch/meilisearch/releases
        echo   ^(pick the meilisearch-windows-amd64.exe asset^) and save it as %MEILI_EXE%
        if not "%AUTO_SILENT%"=="1" pause
        exit /b 1
    )
    echo   Downloaded to %MEILI_EXE%
)

echo [3/6] Reading MEILISEARCH_KEY from pastellive .env...
set "MEILI_KEY="
if exist "%ENV_FILE%" (
    for /f "usebackq tokens=1,* delims==" %%A in ("%ENV_FILE%") do (
        if /i "%%A"=="MEILISEARCH_KEY" set "MEILI_KEY=%%B"
    )
)
if not defined MEILI_KEY (
    echo   [Warn] MEILISEARCH_KEY not found in %ENV_FILE% - generating a random one instead.
    echo   You will need to copy this into pastellive's .env as MEILISEARCH_KEY afterward.
    for /f "delims=" %%K in ('powershell -NoProfile -Command "[guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')"') do set "MEILI_KEY=%%K"
    echo   Generated key: !MEILI_KEY!
) else (
    echo   Reusing existing MEILISEARCH_KEY from .env - no .env change needed.
)

echo [4/6] Writing the self-restarting loop wrapper...
> "%MEILI_DIR%\run_meilisearch_loop.bat" (
    echo @echo off
    echo :loop
    echo "%MEILI_EXE%" --db-path "%MEILI_DIR%\data" --http-addr 127.0.0.1:7700 --master-key "!MEILI_KEY!" --env production ^>^> "%MEILI_DIR%\logs\meilisearch.log" 2^>^&1
    echo echo [%%date%% %%time%%] meilisearch.exe exited, restarting in 5s ^>^> "%MEILI_DIR%\logs\meilisearch.log"
    echo timeout /t 5 /nobreak ^>nul
    echo goto loop
)

echo [5/6] Registering scheduled task...
schtasks /end /tn "%TASK_NAME%" >nul 2>&1
schtasks /delete /tn "%TASK_NAME%" /f >nul 2>&1
schtasks /create /tn "%TASK_NAME%" /tr "\"%MEILI_DIR%\run_meilisearch_loop.bat\"" /sc onstart /ru SYSTEM /rl HIGHEST /f

echo [6/6] Starting now...
schtasks /run /tn "%TASK_NAME%"

echo Done. Meilisearch should be reachable at http://127.0.0.1:7700 within a few seconds.
echo Check status with:  schtasks /query /tn %TASK_NAME%
echo Logs:               %MEILI_DIR%\logs\meilisearch.log
echo pastellive's .env already has:
echo   MEILISEARCH_URL=http://127.0.0.1:7700
echo   MEILISEARCH_KEY=!MEILI_KEY!
echo so no further .env change is needed as long as the key above matches.
echo To stop:            schtasks /end /tn %TASK_NAME%
if not "%AUTO_SILENT%"=="1" pause
exit /b 0

:install_redis
setlocal enabledelayedexpansion
set "REDIS_DIR=C:\Program Files\Redis"
set "REDIS_MSI_URL=https://github.com/tporadowski/redis/releases/download/v5.0.14.1/Redis-x64-5.0.14.1.msi"
set "REDIS_MSI_PATH=%TEMP%\Redis-x64-5.0.14.1.msi"

echo [1/3] Checking whether something is already listening on 6379...
powershell -NoProfile -Command "exit [int](-not (Get-NetTCPConnection -LocalPort 6379 -State Listen -ErrorAction SilentlyContinue))" >nul 2>&1
if not errorlevel 1 (
    echo   already up, skipping install.
    endlocal
    exit /b 0
)

echo [2/3] Downloading Redis-x64-5.0.14.1.msi (tporadowski/redis, BSD license, free for production)...
where curl >nul 2>&1
if not errorlevel 1 (
    curl -L -o "%REDIS_MSI_PATH%" "%REDIS_MSI_URL%"
) else (
    powershell -NoProfile -Command "Invoke-WebRequest -Uri '%REDIS_MSI_URL%' -OutFile '%REDIS_MSI_PATH%'"
)
if not exist "%REDIS_MSI_PATH%" (
    echo [Error] Download failed. Manually download from:
    echo   https://github.com/tporadowski/redis/releases
    echo   and run it, or install Memurai/WSL2 Redis instead.
    endlocal
    exit /b 1
)

echo [3/3] Installing silently (registers a Windows service automatically, port 6379)...
msiexec /i "%REDIS_MSI_PATH%" /quiet /norestart
timeout /t 5 /nobreak >nul

powershell -NoProfile -Command "exit [int](-not (Get-NetTCPConnection -LocalPort 6379 -State Listen -ErrorAction SilentlyContinue))" >nul 2>&1
if errorlevel 1 (
    echo [Error] Installed but nothing is listening on 6379 yet - check the "Redis" service
    echo   in services.msc, or install manually from the MSI at %REDIS_MSI_PATH%.
    endlocal
    exit /b 1
)
echo   Redis is up on 127.0.0.1:6379.
endlocal
exit /b 0

:setup_redis_and_worker
echo   Skipped - RQ worker was removed (dead code, nothing used it since the Go
echo   rewrite). See the comment in this script if you need it for a Python rollback.
exit /b 0
exit /b 0

:install_cloudflared
setlocal enabledelayedexpansion
set "TUNNEL_TOKEN=%~1"
if "%TUNNEL_TOKEN%"=="" (
    echo [Error] A Cloudflare Tunnel token is required.
    pause
    exit /b 1
)

echo [1/3] Checking for cloudflared.exe...
where cloudflared >nul 2>&1
if errorlevel 1 (
    if exist "C:\cloudflared\cloudflared.exe" (
        set "PATH=C:\cloudflared;%PATH%"
    ) else (
        echo   Not found. Installing via winget...
        winget install --id Cloudflare.cloudflared -e --accept-source-agreements --accept-package-agreements
        if errorlevel 1 (
            echo [Error] winget install failed. Manually download cloudflared.exe from:
            echo   https://github.com/cloudflare/cloudflared/releases/latest
            echo   and place it at C:\cloudflared\cloudflared.exe, then re-run this script.
            pause
            exit /b 1
        )
    )
)

echo [2/3] Removing any previous cloudflared service on this machine...
cloudflared service uninstall >nul 2>&1

echo [3/3] Installing cloudflared as a Windows service with the tunnel token...
cloudflared service install %TUNNEL_TOKEN%

echo Done. Check status with:
echo   sc query cloudflared
echo   Get-Service cloudflared   (PowerShell)
echo IMPORTANT - still needed:
echo  1. Stop/remove cloudflared on the Tokyo VM (it's now running here instead).
echo  2. In the Cloudflare Zero Trust dashboard, edit each Public Hostname route's
echo     origin URL for this tunnel from the old Tailscale IP to:
echo       http://localhost:8080         (pastellive.co.kr, studio, admin, api routes -
echo                                       use the actual local port each service uses)
echo     No Tailscale IP is needed anymore since cloudflared runs on the same machine
echo     as the app now.
pause
exit /b 0

:tune_mysql
setlocal enabledelayedexpansion
if /i "%~1"=="silent" set "AUTO_SILENT=1"
echo [1/5] Locating the MySQL/MariaDB Windows service...
set "MYSQL_SVC="
set "MYSQL_INI="
for /f "usebackq tokens=1,2 delims=|" %%A in (`powershell -NoProfile -Command "$svc = Get-CimInstance Win32_Service | Where-Object { ($_.Name -like '*mysql*' -or $_.Name -like '*maria*') -and $_.State -eq 'Running' } | Select-Object -First 1; if ($svc) { $ini = ''; $cand = Get-ChildItem 'C:\ProgramData\MySQL' -Filter my.ini -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1; if ($cand) { $ini = $cand.FullName }; Write-Output ($svc.Name + '|' + $ini) } else { Write-Output '|' }"`) do (
    set "MYSQL_SVC=%%A"
    set "MYSQL_INI=%%B"
)

if "%MYSQL_SVC%"=="" (
    echo [Error] No running MySQL/MariaDB Windows service found - install/start MySQL first.
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)
echo   Service: %MYSQL_SVC%

if "%MYSQL_INI%"=="" (
    echo [Error] Found the service but could not locate its my.ini automatically.
    echo   Edit it by hand instead - typical path:
    echo   C:\ProgramData\MySQL\MySQL Server X.Y\my.ini
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)
echo   Config: %MYSQL_INI%

echo [2/5] Checking how much RAM this machine has...
set "MYSQL_BUFFER_MB="
for /f "usebackq delims=" %%R in (`powershell -NoProfile -Command "[math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1MB * 0.6)"`) do set "MYSQL_BUFFER_MB=%%R"
if "%MYSQL_BUFFER_MB%"=="" set "MYSQL_BUFFER_MB=1024"
echo   Target innodb_buffer_pool_size (if not already set): %MYSQL_BUFFER_MB%M (about 60%% of total RAM)

echo [3/5] Locating python.exe (used to edit my.ini safely)...
set "PYTHON_EXE="
for /f "delims=" %%i in ('where python 2^>nul') do (
    if not defined PYTHON_EXE set "PYTHON_EXE=%%i"
)
if not defined PYTHON_EXE (
    echo [Error] python not found in PATH
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)

echo [4/5] Applying settings (only adds what is missing, backs up my.ini first)...
set "TUNE_OUT=%TEMP%\pastellive_tune_mysql_out.txt"
"%PYTHON_EXE%" "%~dp0..\..\scripts\_tune_mysql.py" "%MYSQL_INI%" "%MYSQL_BUFFER_MB%" > "%TUNE_OUT%" 2>&1
set "TUNE_RESULT="
for /f "usebackq delims=" %%L in ("%TUNE_OUT%") do (
    echo   %%L
    echo %%L | findstr /b "CHANGED" >nul && set "TUNE_RESULT=CHANGED"
)
del "%TUNE_OUT%" >nul 2>&1

echo [5/5] Restarting MySQL if settings actually changed...
if not "%TUNE_RESULT%"=="CHANGED" goto tune_no_restart
net stop "%MYSQL_SVC%"
net start "%MYSQL_SVC%"
echo   Restarted %MYSQL_SVC%.
goto tune_restart_done
:tune_no_restart
echo   Nothing changed (both settings already present) - no restart needed.
:tune_restart_done

echo Done. Check current values with, from a MySQL client:
echo   SHOW VARIABLES LIKE 'innodb_buffer_pool_size';
echo   SHOW VARIABLES LIKE 'slow_query_log%%';
if not "%AUTO_SILENT%"=="1" pause
exit /b 0

:restore_db_if_needed
setlocal enabledelayedexpansion
if /i "%~1"=="silent" set "AUTO_SILENT=1"
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "ENV_FILE=%APPDIR%\.env"
set "DB_HOST=127.0.0.1"
set "DB_PORT=3306"
set "DB_USER=root"
set "DB_PASSWORD="
set "DB_NAME=stelive_db"
if exist "%ENV_FILE%" (
    for /f "usebackq tokens=1,* delims==" %%A in ("%ENV_FILE%") do (
        if /i "%%A"=="DB_HOST" set "DB_HOST=%%B"
        if /i "%%A"=="DB_PORT" set "DB_PORT=%%B"
        if /i "%%A"=="DB_USER" set "DB_USER=%%B"
        if /i "%%A"=="DB_PASSWORD" set "DB_PASSWORD=%%B"
        if /i "%%A"=="DB_NAME" set "DB_NAME=%%B"
    )
)

echo   [1/5] Locating a MySQL client...
set "MYSQL_EXE="
for /f "delims=" %%F in ('where mysql 2^>nul') do if not defined MYSQL_EXE set "MYSQL_EXE=%%F"
if not defined MYSQL_EXE (
    for /f "delims=" %%F in ('where /R "C:\Program Files\MySQL" mysql.exe 2^>nul') do if not defined MYSQL_EXE set "MYSQL_EXE=%%F"
)
if not defined MYSQL_EXE (
    echo     [Warn] No mysql.exe found under PATH or C:\Program Files\MySQL - cannot check/restore DB schema, skipping.
    exit /b 0
)
echo     Using: %MYSQL_EXE%

set "MYSQL_PW_ARG="
if not "%DB_PASSWORD%"=="" set "MYSQL_PW_ARG=-p%DB_PASSWORD%"

echo   [2/5] Making sure the database container itself exists...
"%MYSQL_EXE%" --default-character-set=utf8mb4 -h "%DB_HOST%" -P %DB_PORT% -u "%DB_USER%" %MYSQL_PW_ARG% -e "CREATE DATABASE IF NOT EXISTS `%DB_NAME%` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;" 2>"%TEMP%\pastellive_db_create.txt"
if errorlevel 1 (
    echo     [Warn] Could not even connect to the MySQL server to check/create the
    echo     database - see the error below. Check MySQL is running and DB_USER/
    echo     DB_PASSWORD in .env are correct. Skipping auto-restore.
    type "%TEMP%\pastellive_db_create.txt"
    del "%TEMP%\pastellive_db_create.txt" >nul 2>&1
    exit /b 0
)
del "%TEMP%\pastellive_db_create.txt" >nul 2>&1
echo     OK.

echo   [3/5] Checking whether the DB schema looks initialized (members table)...
set "CHECK_OUT=%TEMP%\pastellive_db_check.txt"
"%MYSQL_EXE%" --default-character-set=utf8mb4 -h "%DB_HOST%" -P %DB_PORT% -u "%DB_USER%" %MYSQL_PW_ARG% "%DB_NAME%" -e "SELECT 1 FROM members LIMIT 1;" > "%CHECK_OUT%" 2>&1
if not errorlevel 1 (
    echo     members table exists - schema already initialized, nothing to do.
    del "%CHECK_OUT%" >nul 2>&1
    exit /b 0
)
findstr /c:"doesn't exist" "%CHECK_OUT%" >nul 2>&1
if errorlevel 1 (
    echo     [Warn] Could not confirm schema state ^(this was not a missing-table error -
    echo     see the actual mysql output below^) - skipping auto-restore to be safe:
    type "%CHECK_OUT%"
    del "%CHECK_OUT%" >nul 2>&1
    exit /b 0
)
del "%CHECK_OUT%" >nul 2>&1
echo     members table is missing - this looks like a fresh/empty database.

echo   [4/5] Looking for the newest local backup under DB\...
set "NEWEST_BACKUP="
for /f "delims=" %%F in ('dir /b /o-d "%APPDIR%\DB\auto_backup_*.sql" 2^>nul') do if not defined NEWEST_BACKUP set "NEWEST_BACKUP=%APPDIR%\DB\%%F"
if not defined NEWEST_BACKUP (
    echo     [Warn] No DB\auto_backup_*.sql file found here - those files are not shipped
    echo     via git ^(too large / real user data^), so copy one over from another server
    echo     via RDP first, then re-run this script ^(or "install_all.bat 11"^) to restore
    echo     it automatically.
    exit /b 0
)
echo     Found: %NEWEST_BACKUP%

echo   [5/5] Restoring from that backup...
"%MYSQL_EXE%" --default-character-set=utf8mb4 -h "%DB_HOST%" -P %DB_PORT% -u "%DB_USER%" %MYSQL_PW_ARG% "%DB_NAME%" < "%NEWEST_BACKUP%"
if errorlevel 1 (
    echo   [Error] DB restore failed - see the mysql error above.
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)
"%MYSQL_EXE%" --default-character-set=utf8mb4 -h "%DB_HOST%" -P %DB_PORT% -u "%DB_USER%" %MYSQL_PW_ARG% "%DB_NAME%" -e "SELECT 1 FROM members LIMIT 1;" >nul 2>&1
if errorlevel 1 (
    echo   [Error] members table still missing after restore - something went wrong, check manually.
    if not "%AUTO_SILENT%"=="1" pause
    exit /b 1
)
echo     Done - DB schema restored from %NEWEST_BACKUP%.
if not "%AUTO_SILENT%"=="1" pause
exit /b 0

:do_install_discord_bots
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "GODIR=%APPDIR%\go-server"
set "GOBIN=%GODIR%\bin"
set "DBOT_EXE=%GOBIN%\discord-bot.exe"
set "DBOT_RUNNER=%GOBIN%\_run_discordbot_loop.bat"
if not exist "%GOBIN%" mkdir "%GOBIN%" >nul 2>&1

echo ==============================================================
echo  Discord bot (Go) install/restart - ops bot
echo ==============================================================
echo  Old-Python\app\discord_bot.py is gone - fully replaced by
echo  discord-bot.exe (no Python needed). Reads the project-root .env
echo  (DISCORD_BOT_TOKEN, DISCORD_ALERT_CHANNEL_ID) as before.
echo.

if not exist "%DBOT_EXE%" (
    echo [Error] %DBOT_EXE% not found.
    if not "%AUTO_SILENT%"=="1" pause
    endlocal & exit /b 1
)

if exist "%GOBIN%\discord-bot.new.exe" (
    echo [swap] Applying updated discord-bot.exe...
    schtasks /end /tn "PastelliveDiscordBot" >nul 2>&1
    if exist "%GOBIN%\discord-bot.old.exe" del /f /q "%GOBIN%\discord-bot.old.exe"
    move /y "%DBOT_EXE%" "%GOBIN%\discord-bot.old.exe" >nul
    move /y "%GOBIN%\discord-bot.new.exe" "%DBOT_EXE%" >nul
)

if not exist "%APPDIR%\logs" mkdir "%APPDIR%\logs" >nul 2>&1

echo [1/3] Writing restart-loop wrapper script...
(
    echo @echo off
    echo for %%%%I in ^("%%~dp0..\.."^) do set "PROJECT_DIR=%%%%~fI"
    echo :loop
    echo "%%~dp0discord-bot.exe" ^>^> "%%~dp0..\..\logs\discord_bot.log" 2^>^&1
    echo echo [%%date%% %%time%%] discord-bot.exe exited, restarting in 5s ^>^> "%%~dp0..\..\logs\discord_bot.log"
    echo timeout /t 5 /nobreak ^>nul
    echo goto loop
) > "%DBOT_RUNNER%"
echo   Wrote %DBOT_RUNNER%

echo [2/3] Registering PastelliveDiscordBot...
schtasks /end /tn "PastelliveDiscordBot" >nul 2>&1
schtasks /delete /tn "PastelliveDiscordBot" /f >nul 2>&1
schtasks /create /tn "PastelliveDiscordBot" /tr "\"%DBOT_RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
if errorlevel 1 (
    echo [Error] schtasks /create failed for PastelliveDiscordBot - see the error above.
    echo         Common cause: not running this .bat as Administrator.
    if not "%AUTO_SILENT%"=="1" pause
    endlocal & exit /b 1
)
schtasks /run /tn "PastelliveDiscordBot"

echo [3/3] Done.
echo Check status:
echo   schtasks /query /tn PastelliveDiscordBot /fo list
echo Logs: %APPDIR%\logs\discord_bot.log
echo To stop:    schtasks /end /tn PastelliveDiscordBot
echo To restart: just run this script again (install_all.bat 2) - it is safe to re-run.
if not "%AUTO_SILENT%"=="1" pause
endlocal
exit /b 0

:install_go_server
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "GODIR=%APPDIR%\go-server"
set "GOBIN=%GODIR%\bin"
set "WATCHDOG_EXE=%GOBIN%\watchdog.exe"
set "RUNNER=%GOBIN%\_run_server_watchdog.bat"
if not exist "%GOBIN%" mkdir "%GOBIN%" >nul 2>&1

echo ==============================================================
echo  pastellive Go server cutover (PastelliveApp: Python -^> Go)
echo ==============================================================
echo  This stops the running Python site (PastelliveApp) and
echo  re-registers the same task name to run the Go server instead.
echo  To undo: install_all.bat 16  (rollback to Python)
echo.
pause

echo [1/4] Checking pastellive-server.exe...
if not exist "%GOBIN%\pastellive-server.exe" (
    echo [Error] %GOBIN%\pastellive-server.exe not found.
    echo   ^(exe/watchdog now live in go-server\bin\ - 2026-09-08 reorg^)
    pause
    endlocal & exit /b 1
)
echo   Found: %GOBIN%\pastellive-server.exe

echo [2/4] Checking watchdog.exe (supervises the process - no Python needed)...
if not exist "%WATCHDOG_EXE%" (
    echo [Error] %WATCHDOG_EXE% not found - build it first:
    echo   cd go-server ^&^& go build -o watchdog.exe .\cmd\watchdog
    pause
    endlocal & exit /b 1
)
echo   Found: %WATCHDOG_EXE%

echo [3/4] Stopping current PastelliveApp task...
schtasks /end /tn "PastelliveApp" >nul 2>&1
schtasks /delete /tn "PastelliveApp" /f >nul 2>&1
powershell -NoProfile -Command "$c = Get-NetTCPConnection -LocalPort 8081 -State Listen -ErrorAction SilentlyContinue; foreach ($p in ($c | Select-Object -ExpandProperty OwningProcess -Unique)) { Stop-Process -Id $p -Force -ErrorAction SilentlyContinue }"
timeout /t 2 /nobreak >nul

if not exist "%GODIR%\logs" mkdir "%GODIR%\logs"

echo [4/4] Registering "PastelliveApp" to run the Go server (via watchdog.exe), and starting it...
(
    echo @echo off
    echo for %%%%I in ^("%%~dp0..\.."^) do set "PROJECT_DIR=%%%%~fI"
    echo "%%~dp0watchdog.exe" -exe "%%~dp0pastellive-server.exe" -log "%%~dp0..\logs\service.log" -health "http://127.0.0.1:8081/api/live_status"
) > "%RUNNER%"
schtasks /create /tn "PastelliveApp" /tr "\"%RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
schtasks /run /tn "PastelliveApp"

echo.
echo Done. The Go server listens on 127.0.0.1:8081 (see config.go LISTEN_ADDR
echo default) and nginx.conf.template's upstream now points there too.
echo (2026-09-08: this used to say 8080/"no nginx changes needed" - that was
echo stale and caused a long-running site-wide 504 outage. If you ever change
echo LISTEN_ADDR in .env, update nginx.conf.template's upstream to match.)
echo.
echo Check status:  schtasks /query /tn PastelliveApp /fo list
echo Logs:          %GODIR%\logs\service.log
echo Stop:          schtasks /end /tn PastelliveApp
echo Restart:       schtasks /end /tn PastelliveApp  ^&^&  schtasks /run /tn PastelliveApp
echo.
echo To roll back to Python: install_all.bat 16
echo.
echo Note: existing logged-in users will need to log in again after this
echo switch (session cookie format changed - no data is lost).
pause
endlocal
exit /b 0

:apply_go_update
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "GODIR=%APPDIR%\go-server"
set "GOBIN=%GODIR%\bin"

echo ==============================================================
echo  Apply updated pastellive-server.exe (swap in new build)
echo ==============================================================
echo  This stops PastelliveApp, replaces pastellive-server.exe with
echo  pastellive-server.new.exe (must already be sitting next to it,
echo  in go-server\bin\), then starts PastelliveApp again.
echo.

if not exist "%GOBIN%\pastellive-server.new.exe" (
    echo [Error] %GOBIN%\pastellive-server.new.exe not found. Nothing to apply.
    pause
    endlocal & exit /b 1
)

pause

echo [1/4] Stopping PastelliveApp...
schtasks /end /tn "PastelliveApp" >nul 2>&1
powershell -NoProfile -Command "$c = Get-NetTCPConnection -LocalPort 8081 -State Listen -ErrorAction SilentlyContinue; foreach ($p in ($c | Select-Object -ExpandProperty OwningProcess -Unique)) { Stop-Process -Id $p -Force -ErrorAction SilentlyContinue }"
timeout /t 2 /nobreak >nul

echo [2/4] Backing up current exe as pastellive-server.old.exe...
if exist "%GOBIN%\pastellive-server.old.exe" del /f /q "%GOBIN%\pastellive-server.old.exe"
if exist "%GOBIN%\pastellive-server.exe" move /y "%GOBIN%\pastellive-server.exe" "%GOBIN%\pastellive-server.old.exe" >nul

echo [3/4] Installing new build...
move /y "%GOBIN%\pastellive-server.new.exe" "%GOBIN%\pastellive-server.exe" >nul
if not exist "%GOBIN%\pastellive-server.exe" (
    echo [Error] Swap failed - pastellive-server.exe missing after move.
    pause
    endlocal & exit /b 1
)

echo [4/4] Starting PastelliveApp...
schtasks /run /tn "PastelliveApp"

echo.
echo Done. Check status:  schtasks /query /tn PastelliveApp /fo list
echo Logs:                %GODIR%\logs\service.log
echo If something is wrong, previous exe is saved as pastellive-server.old.exe
echo (stop the task, delete the new exe, rename .old.exe back to .exe, start the task).
pause
endlocal
exit /b 0

:apply_cimg_update
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "CIMGDIR=%APPDIR%\services\c-image-service"
set "CIMGBIN=%CIMGDIR%\bin"

echo ==============================================================
echo  Apply updated pastellive-image-service.exe (swap in new build)
echo ==============================================================
echo  This stops PastelliveCImageService, replaces
echo  pastellive-image-service.exe with pastellive-image-service.new.exe
echo  (must already be sitting next to it, in c-image-service\bin\),
echo  then starts PastelliveCImageService again.
echo.

if not exist "%CIMGBIN%\pastellive-image-service.new.exe" (
    echo [Error] %CIMGBIN%\pastellive-image-service.new.exe not found. Nothing to apply.
    pause
    endlocal & exit /b 1
)

pause

echo [1/4] Stopping PastelliveCImageService...
schtasks /end /tn "PastelliveCImageService" >nul 2>&1
powershell -NoProfile -Command "$c = Get-NetTCPConnection -LocalPort 8091 -State Listen -ErrorAction SilentlyContinue; foreach ($p in ($c | Select-Object -ExpandProperty OwningProcess -Unique)) { Stop-Process -Id $p -Force -ErrorAction SilentlyContinue }"
timeout /t 2 /nobreak >nul

echo [2/4] Backing up current exe as pastellive-image-service.old.exe...
if exist "%CIMGBIN%\pastellive-image-service.old.exe" del /f /q "%CIMGBIN%\pastellive-image-service.old.exe"
if exist "%CIMGBIN%\pastellive-image-service.exe" move /y "%CIMGBIN%\pastellive-image-service.exe" "%CIMGBIN%\pastellive-image-service.old.exe" >nul

echo [3/4] Installing new build...
move /y "%CIMGBIN%\pastellive-image-service.new.exe" "%CIMGBIN%\pastellive-image-service.exe" >nul
if not exist "%CIMGBIN%\pastellive-image-service.exe" (
    echo [Error] Swap failed - pastellive-image-service.exe missing after move.
    pause
    endlocal & exit /b 1
)

echo [4/4] Starting PastelliveCImageService...
schtasks /run /tn "PastelliveCImageService"

echo.
echo Done. Check status:  schtasks /query /tn PastelliveCImageService /fo list
echo Logs:                %CIMGDIR%\logs\service.log
echo If something is wrong, previous exe is saved as pastellive-image-service.old.exe
echo (stop the task, delete the new exe, rename .old.exe back to .exe, start the task).
pause
endlocal
exit /b 0

:install_nsfw_service
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "NSFWDIR=%APPDIR%\services\nsfw-service"
set "NSFWBIN=%NSFWDIR%\bin"
set "RUNNER=%NSFWBIN%\_run_production.bat"
set "WATCHDOG_EXE=%APPDIR%\go-server\bin\watchdog.exe"
if not exist "%NSFWBIN%" mkdir "%NSFWBIN%" >nul 2>&1

echo ==============================================================
echo  nsfw-service install (fanart/community AI auto-moderation)
echo ==============================================================
echo  This registers nsfw-service (Python + NudeNet, bundled portable
echo  Python interpreter - no separate Python install needed) as a
echo  permanent scheduled task (PastelliveNsfwService, port 8095).
echo.
echo  fanart uploads: high-risk detections are rejected outright,
echo  ambiguous ones are auto-flagged into the existing fanart report
echo  queue for a human to check. Other upload spots (profile picture,
echo  community post images, video comment images) only auto-reject
echo  high-risk detections (no auto-flag queue there yet).
echo.
echo  If this task is never started, go-server just fails open (every
echo  upload passes through unfiltered) - so this step is optional but
echo  recommended.
echo.

if not exist "%NSFWDIR%\runtime\python\python.exe" (
    echo [Error] %NSFWDIR%\runtime\python\python.exe not found.
    echo   ^(bundled portable Python is missing - check nsfw-service\runtime\^)
    pause
    endlocal & exit /b 1
)
if not exist "%NSFWBIN%\run_nsfw.bat" (
    echo [Error] %NSFWBIN%\run_nsfw.bat not found.
    pause
    endlocal & exit /b 1
)
if not exist "%WATCHDOG_EXE%" (
    echo [Error] %WATCHDOG_EXE% not found - build it first:
    echo   cd go-server ^&^& go build -o bin\watchdog.exe .\cmd\watchdog
    pause
    endlocal & exit /b 1
)

pause

echo [1/3] Writing launcher script (%RUNNER%)...
(
    echo @echo off
    echo set NSFW_SERVICE_PORT=8095
    echo set NSFW_SERVICE_ALLOWED_ROOT=%%~dp0..\..\..\static
    echo "%%~dp0..\..\..\go-server\bin\watchdog.exe" -exe "%%~dp0run_nsfw.bat" -log "%%~dp0..\logs\service.log" -health "http://127.0.0.1:8095/health"
) > "%RUNNER%"
echo   Wrote %RUNNER%

echo [2/3] Registering scheduled task (PastelliveNsfwService, port 8095)...
schtasks /end /tn "PastelliveNsfwService" >nul 2>&1
schtasks /delete /tn "PastelliveNsfwService" /f >nul 2>&1
schtasks /create /tn "PastelliveNsfwService" /tr "\"%RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
if errorlevel 1 (
    echo [Error] schtasks /create failed - see the error above. The task was NOT
    echo         registered. Common cause: not running this .bat as Administrator.
    pause
    endlocal & exit /b 1
)
schtasks /run /tn "PastelliveNsfwService"

echo [3/3] Restarting PastelliveApp so go-server picks up MODERATION_SERVICE_URL...
schtasks /end /tn "PastelliveApp" >nul 2>&1
timeout /t 2 /nobreak >nul
schtasks /run /tn "PastelliveApp"

echo.
echo ==============================================================
echo  Done. First start loads the NudeNet model (a few seconds).
echo  Verify:
echo    schtasks /query /tn PastelliveNsfwService /fo list
echo    curl http://127.0.0.1:8095/health
echo    curl -X POST http://127.0.0.1:8095/moderate -H "Content-Type: application/json" -d "{\"path\":\"<static\ 하위 실제 이미지 경로>\"}"
echo  Logs: %NSFWDIR%\logs\service.log
echo ==============================================================
pause
endlocal
exit /b 0

:install_phash_service
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "PHASHDIR=%APPDIR%\services\phash-service"
set "PHASHBIN=%PHASHDIR%\bin"
set "RUNNER=%PHASHBIN%\_run_production.bat"
set "WATCHDOG_EXE=%APPDIR%\go-server\bin\watchdog.exe"
if not exist "%PHASHBIN%" mkdir "%PHASHBIN%" >nul 2>&1

echo ==============================================================
echo  phash-service install (fanart 재업로드/도용 탐지, Zig)
echo ==============================================================
echo  This registers pastellive-phash-service.exe (perceptual-hash
echo  계산 전용, 이미지 디코딩은 c-image-service가 담당) as a permanent
echo  scheduled task (PastellivePhashService, port 8096).
echo.
echo  fanart 업로드마다 c-image-service의 /grayscale32로 32x32 흑백
echo  픽셀을 받아 이 서비스가 DCT 기반 해시를 계산하고, Go 서버가 기존
echo  게시물들과 해밍거리를 비교해 유사도가 높으면 자동으로 신고 큐에
echo  등록합니다(업로드 자체를 막지는 않음).
echo.
echo  이 작업을 등록하지 않으면 재업로드 탐지 기능만 조용히 꺼집니다
echo  (다른 업로드 기능에는 영향 없음) - 선택 사항입니다.
echo.

if not exist "%PHASHBIN%\pastellive-phash-service.exe" (
    echo [Error] %PHASHBIN%\pastellive-phash-service.exe not found.
    pause
    endlocal & exit /b 1
)
if not exist "%WATCHDOG_EXE%" (
    echo [Error] %WATCHDOG_EXE% not found - build it first:
    echo   cd go-server ^&^& go build -o bin\watchdog.exe .\cmd\watchdog
    pause
    endlocal & exit /b 1
)

pause

echo [1/3] Writing launcher script (%RUNNER%)...
(
    echo @echo off
    echo set PHASH_SERVICE_PORT=8096
    echo "%%~dp0..\..\..\go-server\bin\watchdog.exe" -exe "%%~dp0pastellive-phash-service.exe" -log "%%~dp0..\logs\service.log" -health "http://127.0.0.1:8096/health"
) > "%RUNNER%"
echo   Wrote %RUNNER%

echo [2/3] Registering scheduled task (PastellivePhashService, port 8096)...
schtasks /end /tn "PastellivePhashService" >nul 2>&1
schtasks /delete /tn "PastellivePhashService" /f >nul 2>&1
schtasks /create /tn "PastellivePhashService" /tr "\"%RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
if errorlevel 1 (
    echo [Error] schtasks /create failed - see the error above. The task was NOT
    echo         registered. Common cause: not running this .bat as Administrator.
    pause
    endlocal & exit /b 1
)
schtasks /run /tn "PastellivePhashService"

echo [3/3] Restarting PastelliveApp so go-server picks up PHASH_SERVICE_URL...
schtasks /end /tn "PastelliveApp" >nul 2>&1
timeout /t 2 /nobreak >nul
schtasks /run /tn "PastelliveApp"

echo.
echo ==============================================================
echo  Done. Verify:
echo    schtasks /query /tn PastellivePhashService /fo list
echo    curl http://127.0.0.1:8096/health
echo  Logs: %PHASHDIR%\logs\service.log
echo ==============================================================
pause
endlocal
exit /b 0

:rollback_to_python
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"

echo ==============================================================
echo  Rollback: Go server -^> Python (app.py)
echo ==============================================================
echo  This stops PastelliveApp (Go) and re-registers the original
echo  Python app.py under the same task name. nginx does not need
echo  to change (both listen on 127.0.0.1:8080).
echo.
pause

echo [1/2] Stopping current PastelliveApp (Go) task...
schtasks /end /tn "PastelliveApp" >nul 2>&1
schtasks /delete /tn "PastelliveApp" /f >nul 2>&1
powershell -NoProfile -Command "$c = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue; foreach ($p in ($c | Select-Object -ExpandProperty OwningProcess -Unique)) { Stop-Process -Id $p -Force -ErrorAction SilentlyContinue }"
timeout /t 2 /nobreak >nul

echo [2/2] Re-registering Python (app.py) via Old-Python\scripts\_service_loop.py...
call :install_service_generic "PastelliveApp" "Old-Python\app\app.py" "service" "full" "port" silent

echo.
echo Done. Python site is running again on 127.0.0.1:8080.
echo Check status:  schtasks /query /tn PastelliveApp /fo list
echo Logs:          %APPDIR%\logs\service.log
pause
endlocal
exit /b 0

:install_service_generic
setlocal enabledelayedexpansion
set "TASK_NAME=%~1"
set "ENTRY_FILE=%~2"
set "LOG_NAME=%~3"
set "PIP_MODE=%~4"
set "KILL_MODE=%~5"
set "SILENT=%~6"
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"

if not defined TASK_NAME (
    echo [Error] usage: call :install_service_generic ^<task_name^> ^<entry_file^> ^<log_basename^> ^<pip_mode^> ^<kill_mode^> [silent]
    endlocal & exit /b 1
)

echo [1/5] Locating python.exe...
set "PYTHON_EXE="
for /f "delims=" %%i in ('where python 2^>nul') do (
    if not defined PYTHON_EXE set "PYTHON_EXE=%%i"
)
if not defined PYTHON_EXE (
    echo [Error] python not found in PATH
    if not "%SILENT%"=="silent" pause
    endlocal & exit /b 1
)
echo   Using %PYTHON_EXE%

echo [2/5] Installing Python dependencies (pip_mode=%PIP_MODE%)...
if /i "%PIP_MODE%"=="full" (
    "%PYTHON_EXE%" -m pip install --upgrade pip --break-system-packages
    "%PYTHON_EXE%" -m pip install -r "%APPDIR%\Old-Python\app\requirements.txt" --break-system-packages
) else if /i "%PIP_MODE%"=="shared" (
    "%PYTHON_EXE%" -m pip install -r "%APPDIR%\Old-Python\app\requirements.txt" --break-system-packages
) else (
    echo   Skipped.
)

echo [3/5] Removing any existing %TASK_NAME% scheduled task...
schtasks /end /tn "%TASK_NAME%" >nul 2>&1
schtasks /delete /tn "%TASK_NAME%" /f >nul 2>&1
if /i "%KILL_MODE%"=="port" (
    powershell -NoProfile -Command "$c = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue; foreach ($p in ($c | Select-Object -ExpandProperty OwningProcess -Unique)) { Stop-Process -Id $p -Force -ErrorAction SilentlyContinue } "
    timeout /t 2 /nobreak >nul
) else if /i "%KILL_MODE%"=="cmdline" (
    for %%F in ("%ENTRY_FILE%") do set "ENTRY_BASENAME=%%~nxF"
    powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \"name='python.exe'\" | Where-Object { $_.CommandLine -like '*!ENTRY_BASENAME!*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
    timeout /t 2 /nobreak >nul
)

if not exist "%APPDIR%\logs" mkdir "%APPDIR%\logs"

echo [4/5] Registering scheduled task (runs _service_loop.py, which relaunches %ENTRY_FILE% forever)...
schtasks /create /tn "%TASK_NAME%" /tr "\"%PYTHON_EXE%\" \"%APPDIR%\Old-Python\scripts\_service_loop.py\" \"%APPDIR%\" \"%ENTRY_FILE%\" \"%LOG_NAME%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f

echo [5/5] Starting now (without waiting for a reboot)...
schtasks /run /tn "%TASK_NAME%"

echo Done. Check status with:
echo   schtasks /query /tn %TASK_NAME% /fo list
echo Logs: %APPDIR%\logs\%LOG_NAME%.log
echo To stop:    schtasks /end /tn %TASK_NAME%
echo To restart: schtasks /end /tn %TASK_NAME%  ^&^&  schtasks /run /tn %TASK_NAME%
if not "%SILENT%"=="silent" pause
endlocal
exit /b 0

:install_cimage_prod
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "CIMGDIR=%APPDIR%\services\c-image-service"
set "CIMGBIN=%CIMGDIR%\bin"
set "GODIR=%APPDIR%\go-server"
set "RUNNER=%CIMGBIN%\_run_production.bat"
set "WATCHDOG_EXE=%GODIR%\bin\watchdog.exe"
if not exist "%CIMGBIN%" mkdir "%CIMGBIN%" >nul 2>&1

echo ==============================================================
echo  c-image-service PRODUCTION cutover (java-image-service removed)
echo ==============================================================
echo  This will:
echo    1. Register/start pastellive-image-service.exe as a permanent
echo       scheduled task (PastelliveCImageService, port 8091).
echo    2. Fully delete the PastelliveJavaImageService scheduled task
echo       (java-image-service's source/jdk/tools were already removed
echo       from this repo - there is no going back to it here; if you
echo       ever need it again, restore java-image-service\ from git
echo       history and re-run its old installer from that commit).
echo.
echo  The project root .env has already been updated so JAVA_IMAGE_SERVICE_URL
echo  points at http://127.0.0.1:8091 - after this script you still need to
echo  restart PastelliveApp (go-server) so it picks up that change:
echo    schtasks /end /tn PastelliveApp  ^&^&  schtasks /run /tn PastelliveApp
echo.
echo  Make sure you already validated this exe with install_all.bat 18
echo  (test install, port 8092) + curl before running this.
echo.

if not exist "%CIMGBIN%\pastellive-image-service.exe" (
    echo [Error] %CIMGBIN%\pastellive-image-service.exe not found.
    echo   ^(exe now lives in c-image-service\bin\ - 2026-09-08 reorg^)
    pause
    endlocal & exit /b 1
)
if not exist "%WATCHDOG_EXE%" (
    echo [Error] %WATCHDOG_EXE% not found - build it first:
    echo   cd go-server ^&^& go build -o bin\watchdog.exe .\cmd\watchdog
    pause
    endlocal & exit /b 1
)

pause

echo [1/5] Checking tool paths (no Python needed - watchdog.exe supervises the process)...
set "CIMGTOOLS=%CIMGDIR%\tools"
set "CWEBP_PATH_ARG=cwebp"
if exist "%CIMGTOOLS%\cwebp.exe" set "CWEBP_PATH_ARG=%%~dp0..\tools\cwebp.exe"
set "DWEBP_PATH_ARG=dwebp"
if exist "%CIMGTOOLS%\dwebp.exe" set "DWEBP_PATH_ARG=%%~dp0..\tools\dwebp.exe"
set "AVIFENC_PATH_ARG=avifenc"
if exist "%CIMGTOOLS%\avifenc.exe" set "AVIFENC_PATH_ARG=%%~dp0..\tools\avifenc.exe"
set "GIF2WEBP_PATH_ARG=gif2webp"
if exist "%CIMGTOOLS%\gif2webp.exe" set "GIF2WEBP_PATH_ARG=%%~dp0..\tools\gif2webp.exe"
set "FFMPEG_PATH_ARG=ffmpeg"
if exist "C:\ffmpeg\bin\ffmpeg.exe" set "FFMPEG_PATH_ARG=C:\ffmpeg\bin\ffmpeg.exe"
echo   CWEBP_PATH=%CWEBP_PATH_ARG%
echo   DWEBP_PATH=%DWEBP_PATH_ARG%
echo   AVIFENC_PATH=%AVIFENC_PATH_ARG%
echo   GIF2WEBP_PATH=%GIF2WEBP_PATH_ARG%
echo   FFMPEG_PATH=%FFMPEG_PATH_ARG%

echo [2/5] Writing launcher script (%RUNNER%)...
(
    echo @echo off
    echo set IMAGE_SERVICE_PORT=8091
    echo set IMAGE_SERVICE_ALLOWED_ROOT=%%~dp0..\..\..\static
    echo set CWEBP_PATH=%CWEBP_PATH_ARG%
    echo set DWEBP_PATH=%DWEBP_PATH_ARG%
    echo set AVIFENC_PATH=%AVIFENC_PATH_ARG%
    echo set GIF2WEBP_PATH=%GIF2WEBP_PATH_ARG%
    echo set FFMPEG_PATH=%FFMPEG_PATH_ARG%
    echo "%%~dp0..\..\..\go-server\bin\watchdog.exe" -exe "%%~dp0pastellive-image-service.exe" -log "%%~dp0..\logs\service.log" -health "http://127.0.0.1:8091/health"
) > "%RUNNER%"
echo   Wrote %RUNNER%

echo [3/5] Stopping + disabling the test task (PastelliveCImageServiceTest)...
schtasks /end /tn "PastelliveCImageServiceTest" >nul 2>&1
schtasks /change /tn "PastelliveCImageServiceTest" /disable >nul 2>&1

echo [4/5] Registering permanent scheduled task (PastelliveCImageService, port 8091)...
schtasks /end /tn "PastelliveCImageService" >nul 2>&1
schtasks /delete /tn "PastelliveCImageService" /f >nul 2>&1
schtasks /create /tn "PastelliveCImageService" /tr "\"%RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
if errorlevel 1 (
    echo [Error] schtasks /create failed - see the error above. The task was NOT
    echo         registered. Nothing else below will work until this is fixed.
    echo         Common cause: not running this .bat as Administrator.
    pause
    endlocal & exit /b 1
)
schtasks /run /tn "PastelliveCImageService"

echo [5/5] Removing java-image-service scheduled task (fully deleted, not just disabled -
echo        its source/jdk/tools are already gone from this repo)...
schtasks /end /tn "PastelliveJavaImageService" >nul 2>&1
schtasks /delete /tn "PastelliveJavaImageService" /f >nul 2>&1

echo Done registering. Restart PastelliveApp now so it re-reads .env:
schtasks /end /tn "PastelliveApp" >nul 2>&1
timeout /t 2 /nobreak >nul
schtasks /run /tn "PastelliveApp"

echo.
echo ==============================================================
echo  Cutover applied. Verify:
echo    schtasks /query /tn PastelliveCImageService /fo list
echo    curl http://127.0.0.1:8091/health
echo    (then try a real GIF/image upload on the site and check it
echo     actually gets resized/converted, not just left as-is)
echo.
echo  If something's wrong: java-image-service is gone (task deleted,
echo  source/jdk/tools removed from the repo), so there is no quick
echo  toggle back. To restore it: find the last commit before its
echo  removal in git history, restore the java-image-service\ folder
echo  and deploy\windows\_install_java_image_service.bat from it, then
echo  run that installer. In the meantime, c-image-service failing just
echo  means uploads keep the original file unconverted (no crash) - see
echo  c-image-service\README.md and this repo's own logs for what broke.
echo ==============================================================
pause
endlocal
exit /b 0

:install_cimage_test
setlocal enabledelayedexpansion
for %%I in ("%~dp0..\..") do set "APPDIR=%%~fI"
set "CIMGDIR=%APPDIR%\services\c-image-service"
set "CIMGBIN=%CIMGDIR%\bin"
set "GODIR=%APPDIR%\go-server"
set "RUNNER=%CIMGBIN%\_run_test.bat"
set "WATCHDOG_EXE=%GODIR%\bin\watchdog.exe"
if not exist "%CIMGBIN%" mkdir "%CIMGBIN%" >nul 2>&1

echo ==============================================================
echo  c-image-service TEST install (port 8092, NOT production)
echo ==============================================================
echo  NOTE: c-image-service is already production (port 8091, task
echo  PastelliveCImageService) since the java-image-service cutover
echo  (java-image-service has since been removed entirely). This is
echo  only for validating a FUTURE rebuild of pastellive-image-service.exe
echo  before replacing the one
echo  production is actually running, so it uses a different port
echo  (8092) to avoid colliding with it.
echo.
echo  This registers pastellive-image-service.exe as its own scheduled
echo  task on port 8092, running side by side with the production
echo  instance (8091). JAVA_IMAGE_SERVICE_URL / production traffic are
echo  never touched by this script.
echo.

if not exist "%CIMGBIN%\pastellive-image-service.exe" (
    echo [Error] %CIMGBIN%\pastellive-image-service.exe not found.
    echo   ^(exe now lives in c-image-service\bin\ - 2026-09-08 reorg^)
    pause
    endlocal & exit /b 1
)
if not exist "%WATCHDOG_EXE%" (
    echo [Error] %WATCHDOG_EXE% not found - build it first:
    echo   cd go-server ^&^& go build -o bin\watchdog.exe .\cmd\watchdog
    pause
    endlocal & exit /b 1
)

echo [1/4] Checking tool paths (no Python needed - watchdog.exe supervises the process)...
set "CIMGTOOLS=%CIMGDIR%\tools"
set "CWEBP_PATH_ARG=cwebp"
if exist "%CIMGTOOLS%\cwebp.exe" set "CWEBP_PATH_ARG=%%~dp0..\tools\cwebp.exe"
set "DWEBP_PATH_ARG=dwebp"
if exist "%CIMGTOOLS%\dwebp.exe" set "DWEBP_PATH_ARG=%%~dp0..\tools\dwebp.exe"
set "AVIFENC_PATH_ARG=avifenc"
if exist "%CIMGTOOLS%\avifenc.exe" set "AVIFENC_PATH_ARG=%%~dp0..\tools\avifenc.exe"
set "GIF2WEBP_PATH_ARG=gif2webp"
if exist "%CIMGTOOLS%\gif2webp.exe" set "GIF2WEBP_PATH_ARG=%%~dp0..\tools\gif2webp.exe"
set "FFMPEG_PATH_ARG=ffmpeg"
if exist "C:\ffmpeg\bin\ffmpeg.exe" set "FFMPEG_PATH_ARG=C:\ffmpeg\bin\ffmpeg.exe"

echo [2/4] Writing launcher script (%RUNNER%)...
(
    echo @echo off
    echo set IMAGE_SERVICE_PORT=8092
    echo set IMAGE_SERVICE_ALLOWED_ROOT=%%~dp0..\..\..\static
    echo set CWEBP_PATH=%CWEBP_PATH_ARG%
    echo set DWEBP_PATH=%DWEBP_PATH_ARG%
    echo set AVIFENC_PATH=%AVIFENC_PATH_ARG%
    echo set GIF2WEBP_PATH=%GIF2WEBP_PATH_ARG%
    echo set FFMPEG_PATH=%FFMPEG_PATH_ARG%
    echo "%%~dp0..\..\..\go-server\bin\watchdog.exe" -exe "%%~dp0pastellive-image-service.exe" -log "%%~dp0..\logs\service.log" -health "http://127.0.0.1:8092/health"
) > "%RUNNER%"
echo   Wrote %RUNNER%

echo [3/4] Registering scheduled task (PastelliveCImageServiceTest, port 8092)...
schtasks /end /tn "PastelliveCImageServiceTest" >nul 2>&1
schtasks /delete /tn "PastelliveCImageServiceTest" /f >nul 2>&1
schtasks /create /tn "PastelliveCImageServiceTest" /tr "\"%RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f
if errorlevel 1 (
    echo [Error] schtasks /create failed - see the error above. Task was NOT
    echo         registered. Common cause: not running this .bat as Administrator.
    pause
    endlocal & exit /b 1
)

echo [4/4] Starting task...
schtasks /run /tn "PastelliveCImageServiceTest"

echo Done. Check status with: schtasks /query /tn PastelliveCImageServiceTest /fo list
echo Logs: %CIMGDIR%\logs\service.log
echo Test it directly:
echo   curl http://127.0.0.1:8092/health
echo To stop:    schtasks /end /tn PastelliveCImageServiceTest
echo To remove:  schtasks /delete /tn PastelliveCImageServiceTest /f
echo.
echo This never touches JAVA_IMAGE_SERVICE_URL or the production task
echo (PastelliveCImageService, port 8091) - it's purely a side-by-side
echo sandbox for trying out a new build first.
pause
endlocal
exit /b 0
