#!/usr/bin/env bash
# Go 웹서버만 재시작 (예전 서버재시작.bat)
#  - templates/*.html 이나 static 파일을 고친 뒤 반영할 때
#  - nginx / Meilisearch / 이미지 서비스 등 나머지는 건드리지 않음 (전체는 start_service.sh / stop_service.sh)
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"
unit_exists pastellive || { err "pastellive 서비스가 아직 등록 안 됐어요: sudo deploy/linux/install_all.sh 7"; exit 1; }
sudo systemctl restart pastellive && ok "pastellive 재시작 신호를 보냈어요."
say "  몇 초 후 확인: tail -n 30 $APP_DIR/go-server/logs/service.log"
