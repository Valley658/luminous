#!/usr/bin/env bash
# MySQL 백업 (예전 backup_db.ps1)
#  - backups/db/<DB이름>_<날짜시간>.sql.gz 로 저장, 14일 지난 백업은 자동 삭제
#  - 매일 04:00 에 pastellive-backup.timer 가 자동 실행 (수동 실행도 가능)
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

BACKUP_DIR="$APP_DIR/backups/db"
LOG_FILE="$APP_DIR/logs/db_backup.log"
RETENTION_DAYS=14
mkdir -p "$BACKUP_DIR" "$APP_DIR/logs"
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" | tee -a "$LOG_FILE"; }

if [[ ! -f "$ENV_FILE" ]]; then log "[오류] .env 파일을 찾을 수 없어요: $ENV_FILE"; exit 1; fi
backend="$(env_get DB_BACKEND sqlite)"
if [[ "$backend" != "mysql" ]]; then log "[정보] DB_BACKEND=$backend (MySQL 아님) - 건너뜀"; exit 0; fi
command -v mysqldump >/dev/null || { log "[오류] mysqldump 가 없어요 (sudo apt install mysql-client)"; exit 1; }

host="$(env_get DB_HOST 127.0.0.1)"; port="$(env_get DB_PORT 3306)"
user="$(env_get DB_USER root)"; name="$(env_get DB_NAME pastellive_db)"
stamp="$(date +%Y%m%d_%H%M%S)"
out="$BACKUP_DIR/${name}_${stamp}.sql.gz"

if MYSQL_PWD="$(env_get DB_PASSWORD '')" mysqldump --host="$host" --port="$port" --user="$user" \
        --single-transaction --quick --routines --triggers --no-tablespaces \
        --default-character-set=utf8mb4 "$name" 2>"$out.err" | gzip -6 > "$out" \
   && [[ $(gzip -cd "$out" | head -c 100 | wc -c) -gt 0 ]]; then
    rm -f "$out.err"
    log "[$name] 백업 완료: $(basename "$out") ($(du -h "$out" | cut -f1))"
else
    log "[$name] [오류] 백업 실패: $(cat "$out.err" 2>/dev/null)"
    rm -f "$out" "$out.err"
    exit 1
fi

find "$BACKUP_DIR" -maxdepth 1 -type f \( -name '*.sql.gz' -o -name '*.zip' \) -mtime +"$RETENTION_DAYS" -print -delete \
    | while read -r f; do log "[정리] ${RETENTION_DAYS}일 넘은 백업 삭제: $(basename "$f")"; done
log "백업 작업 완료"
