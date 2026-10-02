# 루미너스 (pastellive.co.kr) — Claude 작업 안내

스텔라이브 멤버 비공식 팬 사이트. **한국어 전용** 사이트(영어/일본어 지원 없음).
운영 서버는 이 저장소를 받아 둔 **Ubuntu Desktop PC** 한 대이고, 바깥 연결은 Cloudflare Tunnel 로만 들어온다.

## 구조

| 경로 | 내용 |
|---|---|
| `go-server/` | 메인 웹서버 (Go, chi 라우터, gonja=Jinja 템플릿). 진입점 `cmd/server/main.go`, 핸들러 `internal/handlers/` |
| `go-server/cmd/watchdog/` | 서비스 감시 프로세스 (헬스체크 실패·실행 파일 교체 시 재시작, service.log 10MB 순환) |
| `templates/*.html` | 서버 렌더링 템플릿 (`{{ }}`, `{% %}`, `{# #}` 는 Jinja 문법이니 JS 안에 쓰지 말 것) |
| `static/` | 정적 파일. `static/js/*.js` 는 **빌드 결과**(terser 압축) — 직접 고치지 말 것 |
| `frontend/js/` | JS 원본 (**GitHub에 안 올라감**, `.gitignore`) → `scripts/JS빌드.sh` 로 `static/js` 생성 |
| `services/c-image-service/` | 이미지 변환 (Rust, :8091, cwebp/avifenc/ffmpeg 호출) |
| `services/nsfw-service/` | 업로드 자동 검수 (Python + NudeNet, :8095, venv 는 `runtime/venv`) |
| `services/phash-service/` | 재업로드 탐지 (Zig 0.13, :8096) |
| `deploy/linux/` | 설치 도구 `install_all.sh`, systemd 유닛, logrotate, MySQL 설정 |
| `deploy/nginx/nginx.conf.template` | nginx 설정 원본 (`install_all.sh 4` 가 `/etc/nginx/nginx.conf` 로 적용) |
| `scripts/` | 운영 스크립트 (빌드배포, 서버재시작, 백업/복원, 서비스 시작/중지) |

## 서비스 (systemd)

`pastellive`(Go :8081) · `pastellive-image` · `pastellive-nsfw` · `pastellive-phash` · `meilisearch`(:7700) · `nginx`(:80/443) · `cloudflared` · `mysql`(:3306, DB `pastellive_db`, MySQL 8)
타이머/경로: `pastellive-backup.timer`(매일 04:00 DB 백업), `pastellive-autodeploy.path`(bin 에 `*.new` 생기면 교체)

## 자주 하는 작업

- Go 코드 수정 후: `bash scripts/빌드배포.sh` → `.new` 로 빌드 후 교체, watchdog 가 몇 초 안에 재시작. 되돌리기 `--rollback`
- 템플릿만 수정: `bash scripts/서버재시작.sh`
- JS 원본 수정: `bash scripts/JS빌드.sh`
- 상태 점검: `sudo bash deploy/linux/install_all.sh 12` · 로그 `go-server/logs/service.log`, `/var/log/nginx/timing.log`(rt= 응답시간)
- nginx 설정 수정: 템플릿을 고친 뒤 `sudo bash deploy/linux/install_all.sh 4`

## 규칙

- 커밋 전 `gofmt -l go-server` 가 비어 있어야 함 (CI: gofmt / go vet / go build / go test / shellcheck)
- 비밀값은 `.env`, `.secret_key` 에만. **DB 데이터·덤프(`*.sql`, `*.db`), 인증서, `.env` 는 절대 커밋 금지** (`.gitignore` 로 막혀 있음)
- 바이너리(`go-server/bin/pastellive-server`, `watchdog`, 서비스 bin)는 Git LFS
- `/code` 페이지(코드 탐색기)는 서버의 git 저장소에서 GitHub에 push 된 커밋(`refs/remotes/github/master`)을 읽음 → 원격 이름은 `github`
- 정적 파일 캐시: `?v=<해시>`(`versioned_static()`) 붙은 요청은 1년 캐시. 새 정적 파일 링크는 꼭 `versioned_static()` 사용
- 이 프로젝트는 리눅스 전용. `.bat`/`.ps1`/`.exe` 를 새로 만들지 말 것
- 사용자에게 보여주는 문구는 쉬운 한국어로
