#!/usr/bin/env bash
# frontend/js/*.js (원본) -> static/js/*.js (terser 압축, 사이트와 GitHub에 공개되는 파일)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"
cd "$APP_DIR/tools"
command -v node >/dev/null || { err "Node.js 가 없어요: sudo apt install nodejs npm"; exit 1; }
[[ -d node_modules/terser ]] || npm ci --silent
node build-js.js
say ""
say "완료! 사이트에서 Ctrl+F5 로 새로고침하면 바로 적용돼요. (서버 재시작 필요 없음)"
