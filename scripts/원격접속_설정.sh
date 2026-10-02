#!/usr/bin/env bash
# 원격 접속 설정 (예전 RDP_수정.bat / rdp_fix.ps1 / pastellive_tools.ps1 1번 대신)
#  1) SSH 서버 켜기 - 다른 컴퓨터에서 터미널로 접속 (ssh 사용자@이PC)
#  2) Tailscale(선택) - 공유기 포트포워딩 없이 어디서든 안전하게 접속 (RDP 3389 를 인터넷에 열 필요 없음)
#  3) 화면 원격(RDP) - 우분투 데스크톱 기본 기능, Windows '원격 데스크톱 연결'로 접속 가능
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/_common.sh"

head_line "[1/3] SSH 서버"
sudo apt-get install -y openssh-server >/dev/null && sudo systemctl enable --now ssh && ok "SSH 켜짐 (포트 22)"
say "  다른 PC 에서: ssh $(whoami)@$(hostname -I | awk '{print $1}')"
say "  비밀번호 대신 키로 로그인하려면 접속할 PC 에서: ssh-copy-id $(whoami)@$(hostname -I | awk '{print $1}')"

head_line "[2/3] Tailscale (선택)"
read -r -p "  Tailscale 을 설치할까요? 밖에서 접속할 때 추천해요 (y/N): " a
if [[ "$a" =~ ^[Yy]$ ]]; then
    curl -fsSL https://tailscale.com/install.sh | sh && sudo tailscale up --ssh
    ok "Tailscale 연결됨. 접속할 기기에도 Tailscale 을 깔고 같은 계정으로 로그인하면 돼요."
    say "  이 PC 의 Tailscale 주소: $(tailscale ip -4 2>/dev/null | head -n1)"
fi

head_line "[3/3] 화면 원격 (RDP)"
say "  우분투 데스크톱에 들어 있는 원격 데스크톱(RDP)을 켜면 Windows 의 '원격 데스크톱 연결'로 화면에 접속할 수 있어요."
say "  설정 → 시스템 → 원격 데스크톱 → '원격 로그인' 켜기 → 사용자 이름/비밀번호 정하기"
say "  (로그인 화면부터 원격으로 쓰려면 '원격 로그인', 이미 로그인한 화면을 보려면 '데스크톱 공유')"
say ""
warn "공유기에서 3389(RDP)/22(SSH) 포트포워딩은 하지 마세요. 밖에서는 Tailscale 주소로 접속하는 게 안전해요."
say "  KT 공유기의 예전 RDP 포트포워딩 규칙도 지워 주세요 (172.30.1.254:8899 → 장치설정 → 포트포워딩)."
