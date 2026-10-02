#!/usr/bin/env bash
# 새 우분투 PC 에 루미너스를 처음 설치할 때 (예전 setup_luminous.ps1 + 전체자동설치.bat)
#   1) 이 폴더(luminous)에 .env 와 .secret_key 를 Windows 백업에서 복사해 둔 뒤
#   2) bash scripts/setup_luminous.sh
set -uo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ ! -f "$DIR/.env" ]]; then
    echo "[오류] $DIR/.env 가 없어요. Windows PC 의 luminous\\.env 를 복사해 주세요."
    echo "       (처음부터 새로 만들려면: cp .env.example .env 후 값 채우기)"
    exit 1
fi
exec sudo -E bash "$DIR/deploy/linux/install_all.sh" all "${1:-}"
