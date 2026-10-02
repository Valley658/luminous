#!/usr/bin/env bash
# 루미너스 관련 서비스 전부 시작 (예전 START_SERVICE.bat)
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"
for u in mysql nginx cloudflared "${LUMI_UNITS[@]}"; do
    if unit_exists "$u"; then sudo systemctl start "$u" && ok "$u 시작"; else warn "$u 가 등록 안 돼 있어요 (sudo deploy/linux/install_all.sh)"; fi
done
sudo systemctl start pastellive-backup.timer pastellive-autodeploy.path 2>/dev/null
say ""
say "로그: $APP_DIR/go-server/logs/service.log · /var/log/nginx/error.log · journalctl -u meilisearch"
