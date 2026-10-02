#!/usr/bin/env bash
# nsfw-service 실행 (systemd 의 pastellive-nsfw.service -> watchdog -> 이 스크립트)
# 파이썬 가상환경은 deploy/linux/install_all.sh 8 이 runtime/venv 에 만든다.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PY="$DIR/runtime/venv/bin/python"
if [[ ! -x "$PY" ]]; then
    echo "[오류] $PY 가 없어요. 먼저 실행: sudo deploy/linux/install_all.sh 8" >&2
    exit 1
fi
exec "$PY" "$DIR/src/moderation_server.py"
