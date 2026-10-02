#!/usr/bin/env bash
# 새 빌드 자동 적용 (예전 watch_and_apply.ps1)
#  bin 폴더에 *.new 파일(새 빌드)이 생기면 systemd 의 pastellive-autodeploy.path 가 이 스크립트를 실행한다.
#  *.new → 실제 실행 파일로 바꾸고 이전 것은 *.old 로 남긴다. 재시작은 watchdog 가 실행 파일 변경을 보고 알아서 한다.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"
LOG_FILE="$APP_DIR/logs/auto_deploy.log"
mkdir -p "$APP_DIR/logs"
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOG_FILE"; }

sleep 2   # 복사가 끝날 때까지 잠깐 기다림
for bin in "$APP_DIR/go-server/bin/pastellive-server" \
           "$APP_DIR/services/c-image-service/bin/pastellive-image-service" \
           "$APP_DIR/services/phash-service/bin/pastellive-phash-service"; do
    [[ -f "$bin.new" ]] || continue
    name="$(basename "$bin")"
    log "[$name] 새 빌드 발견 - 적용 시작"
    chmod +x "$bin.new"
    [[ -f "$bin" ]] && cp -p "$bin" "$bin.old"
    if mv -f "$bin.new" "$bin"; then
        touch "$bin"
        log "[$name] 적용 완료 (이전 빌드는 $name.old)"
    else
        log "[$name] [오류] 교체 실패"
    fi
done
