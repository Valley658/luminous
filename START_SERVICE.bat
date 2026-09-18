@echo off
setlocal enabledelayedexpansion

echo Starting ALL pastellive-related services

echo [1/7] Starting PastelliveApp...
schtasks /query /tn "PastelliveApp" >nul 2>&1
if errorlevel 1 goto app_missing
schtasks /run /tn "PastelliveApp"
goto app_done
:app_missing
echo   [Error] Task "PastelliveApp" is not registered yet.
echo   Run deploy\windows\install_all.bat 14 once to set it up (Go cutover).
:app_done

echo [2/6] Starting PastelliveDiscordBot...
schtasks /query /tn "PastelliveDiscordBot" >nul 2>&1
if errorlevel 1 goto bot_missing
schtasks /run /tn "PastelliveDiscordBot"
goto bot_done
:bot_missing
echo   [Error] Task "PastelliveDiscordBot" is not registered yet.
echo   Run deploy\windows\install_all.bat once to set it up.
:bot_done

echo [3/6] Starting nginx...
schtasks /query /tn "NginxStartup" >nul 2>&1
if errorlevel 1 goto nginx_missing
schtasks /run /tn "NginxStartup"
schtasks /run /tn "NginxWatchdog" >nul 2>&1
goto nginx_done
:nginx_missing
echo   [Error] Task "NginxStartup" is not registered yet.
echo   Run deploy\windows\install_all.bat 3 once to set it up.
:nginx_done

echo [4/6] Starting Meilisearch...
schtasks /query /tn "Meilisearch" >nul 2>&1
if errorlevel 1 goto meili_missing
schtasks /run /tn "Meilisearch"
goto meili_done
:meili_missing
echo   [Error] Task "Meilisearch" is not registered yet.
echo   Run deploy\windows\install_all.bat 4 once to set it up.
:meili_done

echo [5/6] Starting Cloudflared tunnel service...
sc query cloudflared >nul 2>&1
if errorlevel 1 goto cf_missing
net start cloudflared >nul 2>&1
goto cf_done
:cf_missing
echo   [Info] Cloudflared is not installed as a service here, skipping.
echo   Run deploy\windows\install_all.bat 5 ^<TOKEN^> once to set it up.
:cf_done

echo [6/6] Starting C Image Service...
schtasks /query /tn "PastelliveCImageService" >nul 2>&1
if errorlevel 1 goto cimg_missing
schtasks /run /tn "PastelliveCImageService"
goto cimg_done
:cimg_missing
echo   [Info] Task "PastelliveCImageService" is not registered yet (optional service).
echo   Run deploy\windows\install_all.bat 17 once to set it up.
echo   (java-image-service is the old/disabled fallback - only start it manually
echo   via deploy\windows\_install_java_image_service.bat if rolling back.)
:cimg_done

echo Started (a few seconds needed to come up). Check logs:
echo   logs\service.log
echo   logs\discord_bot.log
echo   C:\nginx\logs\error.log
echo   C:\meilisearch\logs\meilisearch.log
echo   services\c-image-service\logs\service.log
pause
