#!/usr/bin/env bash
# =============================================================================
#  새 버전 빌드 + 적용 (예전 빌드배포.bat)
#   scripts/빌드배포.sh              JS 압축 + Go 서버 빌드 → 교체 (Go 코드를 고쳤을 때)
#   scripts/빌드배포.sh --all        + Rust 이미지 서비스, Zig phash 서비스도 빌드
#   옵션 --no-restart : 교체만 하고 재시작 신호는 안 보냄(설치 중에 씀)
#
#  교체 방식: 새 파일을 *.new 로 만든 뒤 mv 로 바꿔치기 → watchdog 가 실행 파일이
#  바뀐 걸 3초 안에 감지하고 서비스를 알아서 다시 띄운다(sudo 필요 없음).
#  이전 버전은 *.old 로 남겨 두니, 문제가 생기면 scripts/빌드배포.sh --rollback
#  템플릿(templates/*.html)만 고쳤으면 이 파일 대신 scripts/서버재시작.sh
# =============================================================================
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

ALL=0; NO_RESTART=0; ROLLBACK=0
for a in "$@"; do
    case "$a" in --all) ALL=1 ;; --no-restart) NO_RESTART=1 ;; --rollback) ROLLBACK=1 ;; esac
done

swap_in() {   # swap_in <실행파일 경로>   (<경로>.new → <경로>, 이전 것은 <경로>.old)
    local bin="$1"
    [[ -f "$bin.new" ]] || return 1
    chmod +x "$bin.new"
    [[ -f "$bin" ]] && cp -p "$bin" "$bin.old"
    mv -f "$bin.new" "$bin"
    touch "$bin"
    ok "교체: ${bin#"$APP_DIR"/} (이전 버전: $(basename "$bin").old)"
}

if (( ROLLBACK )); then
    head_line "이전 버전으로 되돌리기"
    for b in go-server/bin/pastellive-server services/c-image-service/bin/pastellive-image-service services/phash-service/bin/pastellive-phash-service; do
        if [[ -f "$APP_DIR/$b.old" ]]; then cp -p "$APP_DIR/$b.old" "$APP_DIR/$b.new" && swap_in "$APP_DIR/$b"; fi
    done
    exit 0
fi

head_line "[0] 사이트 JS 압축 빌드 (frontend/js → static/js)"
if command -v node >/dev/null && [[ -d "$APP_DIR/frontend/js" ]]; then
    bash "$APP_DIR/scripts/JS빌드.sh" || warn "JS 빌드 실패 - static/js 는 그대로 둠"
else
    say "  Node.js 또는 frontend/js 가 없어서 건너뜀 (static/js 는 그대로)."
fi

head_line "[1] Go 서버 빌드 (go-server/bin/pastellive-server)"
GO="$(command -v go || true)"
[[ -z "$GO" && -x /usr/local/go/bin/go ]] && GO=/usr/local/go/bin/go
if [[ -z "$GO" ]]; then
    err "go 명령이 없어요. 설치: sudo apt install golang-go  (go.mod 버전이 더 높으면 자동으로 맞는 버전을 받아요)"
    exit 1
fi
cd "$APP_DIR/go-server" || exit 1
export GOTOOLCHAIN=auto CGO_ENABLED=0
"$GO" build -trimpath -ldflags="-s -w" -o bin/pastellive-server.new ./cmd/server || { err "Go 서버 빌드 실패"; exit 1; }
"$GO" build -trimpath -ldflags="-s -w" -o bin/watchdog.new ./cmd/watchdog || { err "watchdog 빌드 실패"; exit 1; }
# watchdog 는 systemd 가 띄운 감시 프로세스라 파일만 바꿔 두면 다음 서비스 재시작 때 적용된다.
chmod +x bin/watchdog.new && mv -f bin/watchdog.new bin/watchdog
swap_in "$APP_DIR/go-server/bin/pastellive-server"

if (( ALL )); then
    head_line "[2] 이미지 서비스 빌드 (Rust)"
    if command -v cargo >/dev/null || [[ -x "$HOME/.cargo/bin/cargo" ]]; then
        CARGO="$(command -v cargo || echo "$HOME/.cargo/bin/cargo")"
        cd "$APP_DIR/services/c-image-service" || exit 1
        if "$CARGO" build --release --locked && cp target/release/pastellive-image-service bin/pastellive-image-service.new; then
            swap_in "$APP_DIR/services/c-image-service/bin/pastellive-image-service"
        else
            err "Rust 빌드 실패"
        fi
    else
        warn "cargo 가 없어서 건너뜀. 설치: curl https://sh.rustup.rs -sSf | sh"
    fi

    head_line "[3] phash 서비스 빌드 (Zig 0.13)"
    cd "$APP_DIR/services/phash-service" || exit 1
    ZIGVENV="$APP_DIR/tools/.zig-venv"
    if [[ ! -x "$ZIGVENV/bin/python" ]]; then python3 -m venv "$ZIGVENV" && "$ZIGVENV/bin/pip" -q install ziglang==0.13.0; fi
    if "$ZIGVENV/bin/python" -m ziglang build --release=fast -Dtarget=x86_64-linux-musl; then
        cp zig-out/bin/pastellive-phash-service bin/pastellive-phash-service.new && swap_in "$APP_DIR/services/phash-service/bin/pastellive-phash-service"
    else
        err "Zig 빌드 실패"
    fi
fi

say ""
if (( NO_RESTART )); then
    say "빌드 완료 (재시작은 안 함)."
else
    say "완료! watchdog 가 바뀐 실행 파일을 몇 초 안에 감지해서 서비스를 다시 띄워요."
    say "  확인: tail -f $APP_DIR/go-server/logs/service.log"
    say "  문제가 있으면: scripts/빌드배포.sh --rollback"
fi
