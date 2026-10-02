#!/usr/bin/env bash
# 루미너스 관련 서비스 전부 중지 (예전 STOP_SERVICE.bat)
#  Cloudflare Tunnel(cloudflared)과 MySQL 은 일부러 그대로 둔다. 꼭 끄려면:
#    sudo systemctl stop cloudflared    /   sudo systemctl stop mysql
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"
sudo systemctl stop pastellive-autodeploy.path 2>/dev/null
for (( i=${#LUMI_UNITS[@]}-1; i>=0; i-- )); do
    u="${LUMI_UNITS[$i]}"
    unit_exists "$u" && sudo systemctl stop "$u" && ok "$u 중지"
done
sudo systemctl stop nginx && ok "nginx 중지"
say ""
say "다시 켜기: scripts/start_service.sh   (재부팅해도 자동으로 다시 켜져요)"
