#!/usr/bin/env bash
# 전체 자동 설치 바로가기 (= scripts/setup_luminous.sh)
exec bash "$(dirname "${BASH_SOURCE[0]}")/setup_luminous.sh" "$@"
