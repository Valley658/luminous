#!/usr/bin/env bash
# =============================================================================
#  루미너스(pastellive) 우분투 설치/관리 도구  (예전 deploy/windows/install_all.bat 대신)
#
#  사용법:  sudo deploy/linux/install_all.sh            ← 메뉴
#           sudo deploy/linux/install_all.sh all        ← 전체 자동 설치/점검
#           sudo deploy/linux/install_all.sh <번호> [인자] ← 한 가지만
#
#  Windows 때와 달라진 점
#   - 작업 스케줄러/NSSM 대신 systemd 서비스로 등록 (재부팅하면 자동 시작)
#   - nginx/MySQL/ffmpeg/webp/avif 도구는 apt 패키지로 설치
#   - nsfw-service 의 내장 Windows 파이썬 대신 runtime/venv 파이썬 가상환경
#   - 실행 파일은 .exe 없이 리눅스용 바이너리(go-server/bin/pastellive-server 등)
# =============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../scripts/_common.sh
source "$SCRIPT_DIR/../../scripts/_common.sh"

if [[ $EUID -ne 0 ]]; then
    echo "관리자 권한이 필요해서 sudo 로 다시 실행할게요..."
    exec sudo -E bash "$0" "$@"
fi

# 서비스를 돌릴 일반 사용자(프로젝트 폴더 주인)
RUN_USER="${SUDO_USER:-$(stat -c %U "$APP_DIR")}"
if [[ "$RUN_USER" == "root" ]]; then RUN_USER="$(stat -c %U "$APP_DIR")"; fi
RUN_GROUP="$(id -gn "$RUN_USER")"
RUN_HOME="$(getent passwd "$RUN_USER" | cut -d: -f6)"

SYSTEMD_SRC="$SCRIPT_DIR/systemd"
MEILI_VERSION="v1.51.0"
NGINX_CERT_DIR="/etc/nginx/certs"
LOG_DIR="$APP_DIR/logs"
mkdir -p "$LOG_DIR"

as_user() { sudo -u "$RUN_USER" -H "$@"; }

render() {   # render <템플릿> <출력>
    sed -e "s#__APP_DIR__#$APP_DIR#g" -e "s#__USER__#$RUN_USER#g" -e "s#__GROUP__#$RUN_GROUP#g" "$1" > "$2"
}

fix_permissions() {
    # 스크립트/바이너리 실행 권한 (Windows에서 git으로 올리면 실행 권한이 빠질 수 있음)
    find "$APP_DIR/scripts" "$APP_DIR/deploy/linux" "$APP_DIR/services" -name '*.sh' -exec chmod +x {} + 2>/dev/null
    chmod +x "$APP_DIR/go-server/bin/pastellive-server" "$APP_DIR/go-server/bin/watchdog" \
             "$APP_DIR/services/c-image-service/bin/pastellive-image-service" \
             "$APP_DIR/services/phash-service/bin/pastellive-phash-service" 2>/dev/null
    mkdir -p "$APP_DIR/go-server/logs" "$APP_DIR/services/c-image-service/logs" \
             "$APP_DIR/services/nsfw-service/logs" "$APP_DIR/services/phash-service/logs" "$APP_DIR/backups/db"
    chown -R "$RUN_USER:$RUN_GROUP" "$APP_DIR/go-server/logs" "$APP_DIR/services/c-image-service/logs" \
             "$APP_DIR/services/nsfw-service/logs" "$APP_DIR/services/phash-service/logs" "$APP_DIR/backups" "$LOG_DIR"
    # nginx(www-data)가 홈 폴더 안의 static/ 을 읽을 수 있게 경로 통과 권한만 준다 (홈 폴더 내용 열람 권한은 아님)
    local d="$APP_DIR"
    while [[ "$d" != "/" ]]; do
        setfacl -m u:www-data:x "$d" 2>/dev/null || chmod o+x "$d"
        d="$(dirname "$d")"
    done
    setfacl -R -m u:www-data:rX "$APP_DIR/static" 2>/dev/null || chmod -R o+rX "$APP_DIR/static"
    setfacl -R -d -m u:www-data:rX "$APP_DIR/static" 2>/dev/null || true
}

# -----------------------------------------------------------------------------
# 1. 기본 패키지
# -----------------------------------------------------------------------------
install_packages() {
    head_line "[1] 기본 패키지 설치 (apt)"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y \
        nginx mysql-server mysql-client \
        git git-lfs curl ca-certificates unzip gzip acl ufw logrotate \
        webp libavif-bin ffmpeg \
        python3 python3-venv python3-pip \
        nodejs npm \
        openssh-server
    as_user git lfs install >/dev/null 2>&1 || true
    fix_permissions
    ok "패키지 설치 완료 (nginx, MySQL, ffmpeg, cwebp/dwebp/gif2webp, avifenc, python3, node 등)"
}

# -----------------------------------------------------------------------------
# 2. MySQL: DB/계정 만들기 + 튜닝 (Windows의 9번 tune_mysql + 11번 앞부분)
# -----------------------------------------------------------------------------
setup_mysql() {
    head_line "[2] MySQL 설정 (DB/계정 + 튜닝)"
    systemctl enable --now mysql
    local name user pass
    name="$(env_get DB_NAME pastellive_db)"
    user="$(env_get DB_USER root)"
    pass="$(env_get DB_PASSWORD '')"
    if [[ -z "$pass" ]]; then
        warn ".env 에 DB_PASSWORD 가 비어 있어요. 비밀번호를 넣고 다시 실행해 주세요."
        return 1
    fi
    local esc_pass="${pass//\'/\'\'}"
    # root 접속: 처음엔 리눅스 root(auth_socket), 한 번 비밀번호로 바꾼 뒤엔 .env 비밀번호
    mysql_root() { mysql -u root "$@" 2>/dev/null || MYSQL_PWD="$pass" mysql -u root "$@"; }
    mysql_root -e "CREATE DATABASE IF NOT EXISTS \`$name\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;" || { err "DB 생성 실패"; return 1; }
    if [[ "$user" == "root" ]]; then
        # 우분투 MySQL 의 root 는 기본이 '리눅스 root 계정으로만 로그인(auth_socket)' 이라
        # Go 서버가 비밀번호로 접속하지 못한다. .env 의 비밀번호로 접속할 수 있게 바꾼다.
        warn ".env 의 DB_USER 가 root 예요. root 를 비밀번호 방식으로 바꿀게요 (전용 계정을 쓰는 게 더 안전해요: DB_USER=pastellive)."
        mysql_root -e "ALTER USER 'root'@'localhost' IDENTIFIED WITH caching_sha2_password BY '$esc_pass'; FLUSH PRIVILEGES;" \
            || { err "root 비밀번호 설정 실패"; return 1; }
    else
        mysql_root -e "CREATE USER IF NOT EXISTS '$user'@'localhost' IDENTIFIED BY '$esc_pass';
                       ALTER USER '$user'@'localhost' IDENTIFIED BY '$esc_pass';
                       GRANT ALL PRIVILEGES ON \`$name\`.* TO '$user'@'localhost'; FLUSH PRIVILEGES;" \
            || { err "계정 생성 실패"; return 1; }
    fi
    ok "DB '$name' / 계정 '$user' 준비됨"

    # 튜닝: 버퍼 풀 = RAM의 40% (데스크톱으로도 쓰니 Windows 때 60%보다 조금 낮게)
    local mem_mb buf_mb
    mem_mb=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
    buf_mb=$(( mem_mb * 40 / 100 ))
    (( buf_mb < 256 )) && buf_mb=256
    sed "s#__BUFFER_MB__#$buf_mb#" "$SCRIPT_DIR/mysql-pastellive.cnf" > /etc/mysql/mysql.conf.d/zz-pastellive.cnf
    systemctl restart mysql && ok "MySQL 튜닝 적용: innodb_buffer_pool_size=${buf_mb}M, 느린 쿼리 로그(1초 이상)"
}

# -----------------------------------------------------------------------------
# 3. DB 복원 (Windows 백업 .zip / .sql / .sql.gz 모두 가능)
# -----------------------------------------------------------------------------
restore_db() {
    head_line "[3] DB 복원"
    bash "$APP_DIR/scripts/restore_db.sh" "${1:-}"
}

# -----------------------------------------------------------------------------
# 4. nginx
# -----------------------------------------------------------------------------
setup_nginx() {
    head_line "[4] nginx 설정"
    mkdir -p "$NGINX_CERT_DIR" /var/cache/nginx/pastellive /usr/share/nginx/html
    chown www-data:www-data /var/cache/nginx/pastellive
    cp "$APP_DIR/deploy/nginx/50x.html" /usr/share/nginx/html/50x.html
    if [[ ! -s "$NGINX_CERT_DIR/pastellive_origin.pem" || ! -s "$NGINX_CERT_DIR/pastellive_origin.key" ]]; then
        warn "Cloudflare Origin 인증서가 없어요: $NGINX_CERT_DIR/pastellive_origin.pem / .key"
        warn "Windows 백업(C:\\nginx\\certs)의 두 파일을 그 자리에 복사하는 게 정답이에요. 일단 임시 자체 서명 인증서로 띄울게요."
        openssl req -x509 -nodes -newkey rsa:2048 -days 365 -subj "/CN=pastellive.co.kr" \
            -keyout "$NGINX_CERT_DIR/pastellive_origin.key" -out "$NGINX_CERT_DIR/pastellive_origin.pem" >/dev/null 2>&1
    fi
    chmod 600 "$NGINX_CERT_DIR/pastellive_origin.key"
    [[ -f /etc/nginx/nginx.conf && ! -f /etc/nginx/nginx.conf.ubuntu-default ]] && cp /etc/nginx/nginx.conf /etc/nginx/nginx.conf.ubuntu-default
    sed -e "s#__STATIC_DIR__#$APP_DIR/static#g" -e "s#__PROJECT_DIR__#$APP_DIR#g" \
        "$APP_DIR/deploy/nginx/nginx.conf.template" > /etc/nginx/nginx.conf.pastellive.new
    rm -f /etc/nginx/sites-enabled/default
    mv /etc/nginx/nginx.conf.pastellive.new /etc/nginx/nginx.conf
    fix_permissions
    if nginx -t; then
        systemctl enable nginx >/dev/null 2>&1
        if systemctl reload nginx 2>/dev/null || systemctl restart nginx; then
            ok "nginx 적용 완료 (설정 바꾼 뒤에는: sudo nginx -t && sudo systemctl reload nginx)"
        else
            err "nginx 재시작 실패 - journalctl -u nginx -n 30 으로 확인하세요."
            return 1
        fi
    else
        err "nginx 설정 검사 실패 - 위 메시지를 확인하세요. 원래 설정: /etc/nginx/nginx.conf.ubuntu-default"
        return 1
    fi
}

# -----------------------------------------------------------------------------
# 5. Meilisearch
# -----------------------------------------------------------------------------
setup_meilisearch() {
    head_line "[5] Meilisearch ($MEILI_VERSION)"
    if [[ ! -x /usr/local/bin/meilisearch ]] || ! /usr/local/bin/meilisearch --version 2>/dev/null | grep -q "${MEILI_VERSION#v}"; then
        curl -fL --progress-bar -o /tmp/meilisearch \
            "https://github.com/meilisearch/meilisearch/releases/download/$MEILI_VERSION/meilisearch-linux-amd64" \
            || { err "다운로드 실패"; return 1; }
        install -m 755 /tmp/meilisearch /usr/local/bin/meilisearch && rm -f /tmp/meilisearch
    fi
    local key; key="$(env_get MEILISEARCH_KEY '')"
    if [[ -z "$key" ]]; then
        key="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
        warn ".env 에 MEILISEARCH_KEY 가 없어서 새로 만들었어요. .env 에 추가할게요."
        printf '\nMEILISEARCH_KEY=%s\n' "$key" >> "$ENV_FILE"
    fi
    mkdir -p /etc/pastellive /var/lib/meilisearch/data /var/lib/meilisearch/dumps
    printf 'MEILI_MASTER_KEY=%s\n' "$key" > /etc/pastellive/meilisearch.env
    chmod 600 /etc/pastellive/meilisearch.env
    chown -R "$RUN_USER:$RUN_GROUP" /var/lib/meilisearch
    render "$SYSTEMD_SRC/meilisearch.service" /etc/systemd/system/meilisearch.service
    systemctl daemon-reload
    systemctl enable --now meilisearch && systemctl restart meilisearch
    ok "Meilisearch 실행 중 (127.0.0.1:7700). 검색 색인은 Go 서버가 켜질 때 DB에서 다시 만들어요."
}

# -----------------------------------------------------------------------------
# 6. Cloudflare Tunnel
# -----------------------------------------------------------------------------
setup_cloudflared() {
    head_line "[6] Cloudflare Tunnel (cloudflared)"
    local token="${1:-}"
    if ! command -v cloudflared >/dev/null; then
        if ! curl -fL --progress-bar -o /tmp/cloudflared.deb \
                https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb \
           || ! dpkg -i /tmp/cloudflared.deb; then
            err "cloudflared 설치 실패"; return 1
        fi
        rm -f /tmp/cloudflared.deb
    fi
    if [[ -z "$token" ]]; then
        read -r -p "터널 토큰 (Zero Trust 대시보드 → Networks → Tunnels → 이 터널 → 설치 명령의 토큰, 나중에 하려면 Enter): " token
    fi
    if [[ -z "$token" ]]; then
        warn "토큰이 없어서 서비스 등록은 건너뛰었어요. 나중에: sudo deploy/linux/install_all.sh 6 <토큰>"
        return 0
    fi
    cloudflared service uninstall >/dev/null 2>&1 || true
    cloudflared service install "$token" && systemctl enable --now cloudflared
    ok "cloudflared 등록 완료. Windows PC 쪽 cloudflared 는 꺼 주세요(같은 터널을 두 곳에서 돌리면 요청이 나뉘어요)."
}

# -----------------------------------------------------------------------------
# 7. 루미너스 서비스 등록 (Go 서버 / 이미지 / nsfw / phash + 자동 백업 / 새 빌드 자동 적용)
# -----------------------------------------------------------------------------
install_services() {
    head_line "[7] 루미너스 서비스 등록 (systemd)"
    fix_permissions
    local missing=0 f
    for f in go-server/bin/pastellive-server go-server/bin/watchdog \
             services/c-image-service/bin/pastellive-image-service services/phash-service/bin/pastellive-phash-service; do
        [[ -x "$APP_DIR/$f" ]] || { err "$f 가 없어요 (sudo deploy/linux/install_all.sh 9 로 빌드)"; missing=1; }
    done
    (( missing )) && return 1
    [[ -x "$APP_DIR/services/nsfw-service/runtime/venv/bin/python" ]] || warn "nsfw 파이썬 환경이 아직 없어요 → 8번을 먼저 실행하면 검수 기능도 켜져요."
    for f in pastellive pastellive-image pastellive-nsfw pastellive-phash pastellive-backup; do
        render "$SYSTEMD_SRC/$f.service" "/etc/systemd/system/$f.service"
    done
    render "$SYSTEMD_SRC/pastellive-backup.timer" /etc/systemd/system/pastellive-backup.timer
    render "$SYSTEMD_SRC/pastellive-autodeploy.path" /etc/systemd/system/pastellive-autodeploy.path
    render "$SYSTEMD_SRC/pastellive-autodeploy.service" /etc/systemd/system/pastellive-autodeploy.service
    systemctl daemon-reload
    systemctl enable --now pastellive-image pastellive-phash pastellive-nsfw pastellive
    systemctl enable --now pastellive-backup.timer pastellive-autodeploy.path
    ok "서비스 등록 완료 - 재부팅해도 자동으로 켜져요."
    say "  상태 보기: systemctl status pastellive    로그: tail -f $APP_DIR/go-server/logs/service.log"
}

# -----------------------------------------------------------------------------
# 8. nsfw-service 파이썬 가상환경
# -----------------------------------------------------------------------------
setup_nsfw_venv() {
    head_line "[8] nsfw-service 파이썬 환경 (runtime/venv)"
    local svc="$APP_DIR/services/nsfw-service"
    as_user mkdir -p "$svc/runtime"
    if [[ ! -x "$svc/runtime/venv/bin/python" ]]; then
        as_user python3 -m venv "$svc/runtime/venv" || { err "venv 생성 실패 (python3-venv 설치 확인)"; return 1; }
    fi
    as_user "$svc/runtime/venv/bin/pip" install --upgrade pip >/dev/null
    as_user "$svc/runtime/venv/bin/pip" install -r "$svc/requirements.txt" || { err "pip 설치 실패"; return 1; }
    ok "nudenet 설치 완료 ($svc/runtime/venv)"
    systemctl is-enabled pastellive-nsfw >/dev/null 2>&1 && systemctl restart pastellive-nsfw
}

# -----------------------------------------------------------------------------
# 9. 소스에서 다시 빌드 (Go 서버/watchdog, Rust 이미지 서비스, Zig phash)
# -----------------------------------------------------------------------------
build_all() {
    head_line "[9] 소스에서 빌드"
    as_user bash "$APP_DIR/scripts/빌드배포.sh" --all --no-restart
}

# -----------------------------------------------------------------------------
# 10. 방화벽 (ufw)
# -----------------------------------------------------------------------------
setup_firewall() {
    head_line "[10] 방화벽 (ufw)"
    # Cloudflare Tunnel 은 PC 에서 바깥으로 나가는 연결만 쓰므로 들어오는 포트는 하나도 필요 없다.
    # SSH 만 공유기 내부망(172.30.1.0/24)과 Tailscale 에서 허용한다.
    ufw --force reset >/dev/null
    ufw default deny incoming
    ufw default allow outgoing
    ufw allow from 172.30.1.0/24 to any port 22 proto tcp comment 'SSH (집 내부망)'
    ufw allow in on tailscale0 comment 'Tailscale' 2>/dev/null || true
    if [[ "${1:-}" == "cloudflare-ips" ]]; then
        # 터널 대신 공유기 포트포워딩(80/443)으로 받을 때만: Cloudflare IP 에서 오는 것만 허용
        local ip
        for ip in $(curl -fsS https://www.cloudflare.com/ips-v4) $(curl -fsS https://www.cloudflare.com/ips-v6); do
            ufw allow proto tcp from "$ip" to any port 80,443 comment 'Cloudflare' >/dev/null
        done
    fi
    ufw --force enable
    ufw status verbose
    ok "방화벽 켜짐 - 외부에서 들어오는 연결은 막고(터널은 영향 없음), 내부망 SSH 만 허용"
}

# -----------------------------------------------------------------------------
# 11. 절전/잠자기 끄기 (Windows 의 '야간 절전 제거' 대신)
# -----------------------------------------------------------------------------
disable_sleep() {
    head_line "[11] 절전·잠자기 끄기 (서버라서 항상 켜 둠)"
    systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target >/dev/null
    if command -v gsettings >/dev/null; then
        as_user dbus-launch gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type 'nothing' 2>/dev/null || true
        as_user dbus-launch gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-timeout 0 2>/dev/null || true
    fi
    ok "잠자기/최대 절전 비활성화 (화면만 꺼지고 서버는 계속 돌아요)"
}

# -----------------------------------------------------------------------------
# 12. 진단 (Windows 7번 diagnose_localhost)
# -----------------------------------------------------------------------------
diagnose() {
    head_line "[12] 진단"
    local u port name
    for u in mysql nginx meilisearch cloudflared pastellive pastellive-image pastellive-nsfw pastellive-phash pastellive-backup.timer pastellive-autodeploy.path; do
        printf '  %-28s %s\n' "$u" "$(systemctl is-active "$u" 2>/dev/null)"
    done
    echo
    for port in 3306:MySQL 7700:Meilisearch 8081:Go서버 8091:이미지 8095:nsfw 8096:phash 80:nginx 443:nginx-https; do
        name="${port#*:}"; port="${port%%:*}"
        if ss -ltn "sport = :$port" | grep -q LISTEN; then printf '  포트 %-5s %-12s 열림\n' "$port" "$name"
        else printf '  %s포트 %-5s %-12s 닫힘%s\n' "$C_WARN" "$port" "$name" "$C_END"; fi
    done
    echo
    printf '  Go 서버 직접:  '; curl -s -o /dev/null -w '%{http_code} (%{time_total}s)\n' --max-time 5 http://127.0.0.1:8081/ || echo 실패
    printf '  nginx 경유:    '; curl -s -o /dev/null -w '%{http_code} (%{time_total}s)\n' --max-time 5 -A Mozilla -H 'Host: pastellive.co.kr' http://127.0.0.1/ || echo 실패
    echo
    say "  최근 서버 로그: tail -n 50 $APP_DIR/go-server/logs/service.log"
    say "  nginx 오류:     sudo tail -n 50 /var/log/nginx/error.log"
    say "  서비스 기록:    journalctl -u pastellive -n 50"
}

# -----------------------------------------------------------------------------
# 13. 로그 정리 (logrotate)
# -----------------------------------------------------------------------------
setup_logrotate() {
    head_line "[13] 로그 정리 (logrotate)"
    render "$SCRIPT_DIR/logrotate-pastellive" /etc/logrotate.d/pastellive
    if logrotate -d /etc/logrotate.d/pastellive >/dev/null 2>&1; then
        ok "/etc/logrotate.d/pastellive 설치 (매주, 20MB 넘으면 바로)"
    else
        warn "logrotate 설정 검사에서 경고가 있어요: sudo logrotate -d /etc/logrotate.d/pastellive"
    fi
}

# -----------------------------------------------------------------------------
everything() {
    head_line "전체 자동 설치/점검 시작 ($APP_DIR, 실행 사용자: $RUN_USER)"
    local logf
    logf="$LOG_DIR/install_all_$(date +%Y%m%d_%H%M%S).log"
    {
        install_packages
        setup_mysql
        setup_nginx
        setup_meilisearch
        setup_nsfw_venv
        install_services
        disable_sleep
        setup_logrotate
        if ! systemctl is-active --quiet cloudflared; then setup_cloudflared "${1:-}"; fi
        diagnose
    } 2>&1 | tee "$logf"
    say ""
    say "전체 로그: $logf"
    say "DB 를 Windows 백업에서 옮겨야 하면: sudo deploy/linux/install_all.sh 3 <백업파일(.zip/.sql/.sql.gz)>"
}

menu() {
    while true; do
        cat <<EOF

  루미너스 우분투 설치/관리 도구   ($APP_DIR, 사용자 $RUN_USER)
  ------------------------------------------------------------
   [1]  기본 패키지 설치 (nginx, MySQL, ffmpeg, webp/avif, python, node)
   [2]  MySQL 설정 (DB/계정 만들기 + 튜닝)
   [3]  DB 복원 (Windows 백업 .zip / .sql / .sql.gz)
   [4]  nginx 설정 적용
   [5]  Meilisearch 설치/시작
   [6]  Cloudflare Tunnel 설치 (토큰 필요)
   [7]  루미너스 서비스 등록/시작 (Go 서버, 이미지, nsfw, phash, 자동 백업, 새 빌드 자동 적용)
   [8]  nsfw-service 파이썬 환경 만들기
   [9]  소스에서 다시 빌드 (Go / Rust / Zig)
   [10] 방화벽(ufw) 켜기
   [11] 절전·잠자기 끄기
   [12] 진단 (서비스/포트/응답 확인)
   [13] 로그 정리(logrotate) 설정
   [all] 전체 자동 설치 (1,2,4,5,8,7,11,13 + 터널)
   [0]  종료
EOF
        read -r -p "번호를 입력하세요: " c
        case "$c" in 0|q) return 0 ;; *) run_choice "$c" ;; esac
    done
}

run_choice() {
    local c="$1"; shift || true
    case "$c" in
        1) install_packages ;;
        2) setup_mysql ;;
        3) restore_db "${1:-}" ;;
        4) setup_nginx ;;
        5) setup_meilisearch ;;
        6) setup_cloudflared "${1:-}" ;;
        7) install_services ;;
        8) setup_nsfw_venv ;;
        9) build_all ;;
        10) setup_firewall "${1:-}" ;;
        11) disable_sleep ;;
        12) diagnose ;;
        13) setup_logrotate ;;
        all) everything "${1:-}" ;;
        *) err "알 수 없는 번호: $c" ;;
    esac
}

if [[ $# -eq 0 ]]; then menu; else run_choice "$@"; fi
