#!/usr/bin/env bash
# DB 복원 (예전 install_all.bat 11번)
#   scripts/restore_db.sh <백업파일>     .sql / .sql.gz / .zip(Windows backup_db.ps1 결과) 모두 가능
#   scripts/restore_db.sh                파일을 안 주면 backups/db 에서 가장 최근 백업을 씀
# 이미 데이터(members 테이블)가 있으면 덮어쓰기 전에 한 번 더 물어본다.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

file="${1:-}"
if [[ -z "$file" ]]; then
    file="$(ls -1t "$APP_DIR"/backups/db/*.sql.gz "$APP_DIR"/backups/db/*.zip "$APP_DIR"/backups/db/*.sql "$APP_DIR"/DB/auto_backup_*.sql 2>/dev/null | head -n 1)"
    [[ -z "$file" ]] && { err "복원할 백업 파일이 없어요. 사용법: $0 <백업파일>"; exit 1; }
fi
[[ -f "$file" ]] || { err "파일이 없어요: $file"; exit 1; }

host="$(env_get DB_HOST 127.0.0.1)"; port="$(env_get DB_PORT 3306)"
user="$(env_get DB_USER root)"; name="$(env_get DB_NAME pastellive_db)"
MYSQL_PWD="$(env_get DB_PASSWORD '')"
export MYSQL_PWD
my() { mysql --default-character-set=utf8mb4 -h "$host" -P "$port" -u "$user" "$@"; }

my -e "CREATE DATABASE IF NOT EXISTS \`$name\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;" || { err "DB 접속 실패 (.env 의 DB_USER/DB_PASSWORD 확인, 먼저 install_all.sh 2)"; exit 1; }
if my "$name" -e "SELECT 1 FROM members LIMIT 1;" >/dev/null 2>&1; then
    warn "'$name' 에 이미 데이터가 있어요. 복원하면 같은 이름의 테이블이 백업 내용으로 바뀌어요."
    read -r -p "  계속할까요? (yes 입력): " a
    [[ "$a" == "yes" ]] || { say "  취소했어요."; exit 0; }
fi

say "복원 중: $file → $name"
case "$file" in
    *.zip)    unzip -p "$file" '*.sql' ;;
    *.gz)     gzip -cd "$file" ;;
    *)        cat "$file" ;;
esac | sed '1s/^\xEF\xBB\xBF//' | my "$name" || { err "복원 실패"; exit 1; }

if my "$name" -e "SELECT COUNT(*) AS members FROM members;" ; then ok "복원 완료"; else err "복원은 끝났지만 members 테이블 확인 실패"; exit 1; fi
