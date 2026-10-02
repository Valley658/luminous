#!/usr/bin/env bash
# 루미너스 리눅스 스크립트 공용 함수 (다른 스크립트가 source 해서 씀)
# - APP_DIR: 프로젝트 루트(이 파일 기준 한 단계 위)
# - env_get KEY [기본값]: .env 에서 값 읽기(따옴표 제거)

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env}"

if [[ -t 1 ]]; then
    C_OK=$'\e[32m'; C_WARN=$'\e[33m'; C_ERR=$'\e[31m'; C_HEAD=$'\e[36m'; C_DIM=$'\e[2m'; C_END=$'\e[0m'
else
    C_OK=""; C_WARN=""; C_ERR=""; C_HEAD=""; C_DIM=""; C_END=""
fi
say()  { printf '%s\n' "$*"; }
ok()   { printf '%s  [완료] %s%s\n' "$C_OK" "$*" "$C_END"; }
warn() { printf '%s  [경고] %s%s\n' "$C_WARN" "$*" "$C_END"; }
err()  { printf '%s  [오류] %s%s\n' "$C_ERR" "$*" "$C_END" >&2; }
head_line() { printf '\n%s== %s ==%s\n' "$C_HEAD" "$*" "$C_END"; }

env_get() {
    local key="$1" def="${2:-}" line val
    if [[ -f "$ENV_FILE" ]]; then
        line="$(grep -E "^[[:space:]]*${key}[[:space:]]*=" "$ENV_FILE" | tail -n 1 || true)"
        if [[ -n "$line" ]]; then
            val="${line#*=}"
            val="${val%$'\r'}"
            val="$(printf '%s' "$val" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
            if [[ "$val" =~ ^\"(.*)\"$ || "$val" =~ ^\'(.*)\'$ ]]; then val="${BASH_REMATCH[1]}"; fi
            printf '%s' "$val"
            return 0
        fi
    fi
    printf '%s' "$def"
}

# 루미너스 systemd 서비스 목록 (시작 순서)
LUMI_UNITS=(meilisearch pastellive-image pastellive-phash pastellive-nsfw pastellive)

need_root() {
    if [[ $EUID -ne 0 ]]; then
        exec sudo -E bash "$0" "$@"
    fi
}

unit_exists() { systemctl list-unit-files "$1.service" --no-legend 2>/dev/null | grep -q "^$1.service"; }
