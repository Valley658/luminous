# pastellive.co.kr 보안 점검 - 2026-09-25

이 문서는 2026-09-25에 진행한 인프라(Cloudflare/nginx/방화벽) + 애플리케이션
보안 점검 결과를 정리한 기록이다. 실제 운영 서버(Windows PC, nginx, Go
백엔드, Cloudflare 앞단)를 대상으로 한 점검이며, **이 세션에는 운영 서버에
대한 원격 실행 권한이 없어** 레포지토리 안의 설정 파일/코드만 직접 고쳤고,
방화벽·Cloudflare 대시보드 설정은 스크립트/절차만 준비해뒀다. 실제 적용은
서버를 관리하는 리도님이 해야 한다.

**중요한 전제 정정**: 요청서에는 "웹 애플리케이션은 Python 기반"이라고
되어 있었지만, 실제로는 **Go(go-server/)** 백엔드다. 예전 Python(Flask)
앱을 Go로 이전하면서, 템플릿만 Jinja2 호환 엔진(gonja)으로 그대로 유지한
상태라 템플릿 문법(`{{ }}`, `{% %}`)이 Python/Flask 시절 그대로 보여서
혼동이 있었을 것으로 보인다. 아래 점검은 전부 실제 코드(Go)를 기준으로
했다.

---

## 1. 발견된 문제 / 2. 위험도 / 3. 원인 (통합 표)

| # | 문제 | 위험도 | 원인 |
|---|------|--------|------|
| A | **템플릿 엔진 자동 이스케이프가 꺼져있음 → 저장형 XSS** | **Critical** | gonja(Jinja2 호환) 템플릿 엔진의 기본값이 `AutoEscape: false`. 닉네임 등 사용자 입력이 `{{ user.nickname }}` 형태로 그대로 HTML에 출력됨 |
| B | Origin IP:80에 Host 헤더만 맞추면 Cloudflare를 거치지 않고 직접 접근 가능 | Critical | 서버 방화벽이 80/443을 전체 인터넷에 열어두고 있고(Cloudflare IP로 제한 안 됨), nginx에도 이를 막는 수단이 없음 |
| C | nginx에 명시적 `default_server`가 없음 | High | 첫 번째 `server` 블록(`pastellive.co.kr`)이 알 수 없는 Host 요청까지 암묵적으로 받아버림 |
| D | Cloudflare↔Origin 간 HTTPS 502 간헐 발생 | High (운영 영향) | Origin에 443 리스너 자체가 없음(HTTP만 서비스) + 이 세션 시작 시점에 발견/수정한 포트 8081 충돌로 인한 서버 크래시루프가 원인이었을 가능성이 높음(이미 수정됨, 아래 6번 참고) |
| E | TLS 1.0/1.1 활성화, `Strict-Transport-Security: max-age=0` | Medium~High | 전부 Cloudflare 엣지 설정(대시보드) - Origin 코드/설정으로는 바꿀 수 없음 |
| F | 보안 헤더 누락(CSP, X-Frame-Options 등) | Medium | nginx/애플리케이션 어디에도 설정된 적 없음 |
| G | `/api/community/post` 비로그인 시 HTTP 200 + `success:false` | Low~Medium | 해당 핸들러만 다른 인증 필요 핸들러들과 다른 응답 패턴을 쓰고 있었음(다른 20건은 전부 정상적인 200 비즈니스 응답이거나 이미 401/403을 쓰고 있어서 문제 없음) |
| H | 외부 CDN 스크립트에 SRI(integrity) 없음 | Low | 처음부터 설정 안 함 |

**이미 안전한 것으로 확인된 부분(이번에 다시 점검함, 별도 수정 불필요)**:
- `.env`, `.git/`, `/config.json` 등은 nginx에 이미 `return 444`(연결 종료)
  규칙이 있어 요청서에서 관찰한 동작(일반 404와 다른 연결 종료)이 정상
  작동 중이었음 (`deploy/nginx/nginx.conf.template` 86번째 줄 근처)
- nginx `root`가 프로젝트 폴더 전체를 가리키지만, `/static/` 외의 경로는
  전부 Go 백엔드로 프록시되고 Go 쪽도 `/static/*`만 파일 서버로 노출하므로
  `.env`, DB 백업 zip, 스크립트 파일 등이 직접 서빙되지 않음
- 관리자 API 12개(`/api/admin/*`, `/api/admin/inquiries/*` 등) 전부
  `isAdmin(r)`으로 서버 측 재검증하고 있음 - 프론트엔드 상태만 믿는 곳 없음
- 커뮤니티 글 정렬(`sortBy`)은 화이트리스트 방식(고정 문자열 2개 중 선택)이라
  SQL Injection 불가능. 그 외 쿼리도 전부 `?` 파라미터 바인딩 사용, 문자열
  조합으로 SQL 만드는 곳 없음
- 비밀번호는 Argon2id 해시(레거시 계정은 PBKDF2/Werkzeug 해시 자동 마이그레이션),
  세션 쿠키는 `HttpOnly` + `Secure`(운영 환경) + `SameSite=Lax`
- CSRF(Origin/Referer 검증), IP 스푸핑 방지(X-Forwarded-For 마지막 값 사용),
  로그인/회원가입 전용 rate limit, IDOR(객체 소유자 확인) 패턴은 이번 세션
  앞부분의 코드 보안 감사에서 이미 점검 및 수정 완료(별도 커밋)
- 파일 업로드: 확장자 화이트리스트 + 실제 이미지 재인코딩(javaimage 서비스가
  리사이즈하면서 디코딩하므로 확장자만 바꾼 가짜 이미지는 여기서 걸러짐) +
  AI 모더레이션 + 서버가 타임스탬프 기반 새 파일명 생성(원본 파일명을 경로로
  안 씀)

---

## 4. 수정한 파일 / 5. 수정 내용 / 6. 수정 전후 차이

### `go-server/internal/gotemplates/gotemplates.go` (Critical 수정)
- **전**: `gonja.FromFile()`이 패키지 기본값(`AutoEscape: false`)을 그대로 사용
  → `{{ user.nickname }}` 등이 이스케이프 없이 그대로 출력됨
- **후**: `init()`에서 `gonja.DefaultConfig.AutoEscape = true` 설정
- **검증**: 별도 격리 환경에서 동일 라이브러리로 직접 재현 -
  수정 전: `<h2><script>alert(1)</script></h2>` (그대로 실행됨)
  수정 후: `<h2>&lt;script&gt;alert(1)&lt;/script&gt;</h2>` (텍스트로만 표시됨)
  `{{ x | tojson }}` 필터는 gonja 내부에서 이미 "safe" 처리가 돼 있어
  이 변경 후에도 깨지지 않음을 같이 확인함(`{"a":"b\"c"}` 그대로 출력).
  템플릿 전체에서 `| safe` 필터를 쓰는 곳이 없어서(grep 확인) 의도적으로
  raw HTML을 출력하던 곳도 없었음 - 깨질 화면 없음.

### `deploy/nginx/nginx.conf.template`
- 맨 위에 `listen 80 default_server; server_name _; return 444;` 블록 추가
  (알 수 없는 Host 요청 차단 - 이것만으로 Origin 우회가 막히는 건 아니고,
  아래 방화벽 스크립트와 함께 적용해야 의미 있음)
- `pastellive.co.kr`, `admin.pastellive.co.kr`, `api.pastellive.co.kr`,
  `world.pastellive.co.kr` 4개 서버 블록에 보안 헤더 추가:
  `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`,
  `Permissions-Policy`
- `pastellive.co.kr` 블록에만 `Content-Security-Policy-Report-Only` 추가
  (강제 차단 아님 - 위반 사항만 `/api/csp-report`로 로그 수집, 기존에
  이미 있던 엔드포인트를 그대로 씀). YouTube 임베드, cdnjs/jsdelivr,
  구글/그라바타 이미지, 구글 드라이브 미리보기 iframe을 전부 허용 목록에
  넣어뒀음(실제 템플릿에서 쓰는 외부 리소스를 전부 grep해서 확인).
  COOP/CORP/COEP는 디스코드 로그인 등과의 호환성을 확인 못 해서 넣지 않음.
- `restore.pastellive.co.kr`(마인크래프트 관련), `nginx.linux.conf.template`은
  건드리지 않음 - 전자는 웹앱과 무관, 후자는 어떤 스크립트에서도 참조하지
  않는 미사용 파일로 확인됨(레포 전체 grep)

### `go-server/internal/handlers/community.go`
- `CreateCommunityPostHandler`에서 비로그인/세션만료 시 `writeJSON`(항상
  200)으로 응답하던 것을 `httputil.JSONError(w, http.StatusUnauthorized, ...)`
  (401)로 변경. 응답 JSON 형식(`success`, `message`)은 동일하게 유지해서
  프론트엔드가 깨질 일은 없음(`fetch().then(r=>r.json())`은 상태 코드와
  무관하게 본문을 읽음).

### 새 파일: `scripts/setup_cloudflare_firewall.ps1` / `.bat`, `scripts/disable_cloudflare_firewall.ps1` / `.bat`
- Cloudflare 공식 IP 대역(`https://www.cloudflare.com/ips-v4`, `ips-v6`)을
  매번 새로 받아와서 Windows 방화벽에 "TCP 80/443은 그 대역에서만" 허용
  규칙을 만듦. 80/443 이외 포트는 절대 건드리지 않고, 원격 접속(RDP 등)
  규칙도 그대로 둠. 기본은 **미리보기(dry-run)**만 하고, `-Apply`를 붙여야
  실제 적용됨. 기존에 80/443을 열어주던 규칙은 삭제가 아니라 비활성화만
  해서 `disable_cloudflare_firewall.ps1`로 정확히 되돌릴 수 있게 함.
  **이 스크립트는 아직 실행하지 않았음** - 아래 "8. Cloudflare에서 직접
  해야 하는 일" / "9. 서버에서 추가로 해야 할 작업" 참고.

### `pastellive-server.new.exe`
- 위 Go 코드 변경사항을 반영해서 새로 빌드해둠(기존 자동배포 작업이 2분
  안에 자동 적용).

---

## 7. 아직 남은 위험

- **Origin 직접 접근**: 방화벽 스크립트를 아직 실행(`-Apply`)하지 않았으므로
  여전히 Cloudflare를 우회해서 Origin IP:80에 직접 접근 가능한 상태다.
  실행 전까지는 이번 점검 전과 동일하게 위험이 남아있음.
- **Cloudflare↔Origin 구간 평문 통신**: Origin에 443/TLS가 없어서(Flexible
  모드로 추정) Cloudflare와 Origin 사이 구간은 암호화되지 않는다. 방화벽으로
  그 구간 접근을 Cloudflare IP로 제한하면 위험은 크게 줄지만, Full(Strict)
  전환만큼 근본적이진 않음(아래 8번 참고).
- **TLS 1.0/1.1, HSTS**: Cloudflare 대시보드 설정이라 이 세션에서 변경 불가.
- **CSP는 아직 Report-Only**: 강제 차단이 아니라서, 이 자체로는 XSS를 막지
  못한다(위 A번 gonja 수정이 실질적 방어). 몇 주 로그를 보고 강제 적용으로
  전환 권장.
- **SRI 미적용**: cdnjs/jsdelivr가 위/변조되면 그대로 실행됨(가능성은 낮지만
  0은 아님).
- **admin.pastellive.co.kr / api.pastellive.co.kr / world.pastellive.co.kr
  서브도메인의 HTTPS 상태 미확인**: HSTS에 `includeSubDomains`를 켜기 전에
  이 서브도메인들도 전부 HTTPS로 문제없이 접속되는지 반드시 먼저 확인해야
  함(안 그러면 그 서브도메인들이 통째로 접속 불가가 될 수 있음).

---

## 8. Cloudflare Dashboard에서 직접 변경해야 하는 설정

Cloudflare API 연동이 없어서 이 세션에서는 대시보드 설정을 확인/변경할 수
없었다. 아래를 **순서대로** 진행 권장:

1. **SSL/TLS → Overview**: 현재 모드 확인. Origin에 443이 없으므로
   지금은 **Flexible**일 가능성이 높음. 이게 502의 원인은 아니었을 가능성이
   크지만(아래 9번 참고), 그래도 Full(Strict)로 넘어가는 걸 최종 목표로
   권장.
2. **SSL/TLS → Edge Certificates → Minimum TLS Version**: `TLS 1.2`로 변경
   (TLS 1.0/1.1 비활성화).
3. **SSL/TLS → Edge Certificates → HSTS**: 지금 `max-age=0`이라 사실상
   꺼진 상태. 처음엔 `max-age=300`~`3600` 정도의 **짧은 값**으로, **includeSubDomains/preload는 끄고** 켜서 문제 없는지 며칠 지켜본 뒤,
   점진적으로 `max-age`를 늘리고(예: 6개월 → 1년), 모든 서브도메인이
   HTTPS로 정상 동작함을 확인한 뒤에만 `includeSubDomains`를, 그 다음에
   `preload`를 추가하는 순서로.
4. **Origin CA 인증서 발급(권장, Full(Strict)로 가려면 필수)**:
   SSL/TLS → Origin Server → Create Certificate. 발급된 인증서(.pem)와
   개인키(.key)를 서버에 저장(예: `C:\nginx\ssl\`), nginx.conf.template에
   `listen 443 ssl;` 블록을 추가하고 위 인증서 경로를 지정 - **이건 인증서를
   실제로 발급받은 뒤 요청해주시면 그때 nginx 설정을 마저 만들어드릴게요.**
   지금은 인증서가 없어서 443 블록을 미리 넣어두지 않았음(빈 설정으로
   nginx를 재시작하면 오히려 서비스가 죽을 수 있어서).

---

## 9. 서버에서 추가로 해야 할 작업

1. **방화벽 스크립트 실행** (가장 중요, Origin 우회 문제의 실제 해결책):
   ```
   cd C:\Users\user\Desktop\luminous
   powershell -ExecutionPolicy Bypass -File scripts\setup_cloudflare_firewall.ps1
   ```
   미리보기 결과를 확인한 뒤 문제없어 보이면:
   ```
   powershell -ExecutionPolicy Bypass -File scripts\setup_cloudflare_firewall.ps1 -Apply
   ```
   적용 직후 **반드시 스마트폰 데이터(와이파이 끄고) 같은 이 네트워크 밖의
   회선**에서 `https://pastellive.co.kr` 접속을 확인해주세요. 문제가 생기면:
   ```
   powershell -ExecutionPolicy Bypass -File scripts\disable_cloudflare_firewall.ps1
   ```
2. **nginx 설정 재생성 + 무중단 반영**:
   ```
   deploy\windows\install_all.bat  (메뉴에서 [3] nginx 서비스 선택)
   ```
   또는 conf만 다시 만들고 reload만 하고 싶으면:
   ```
   C:\nginx\nginx.exe -p C:\nginx -c C:\nginx\conf\nginx.conf -t
   C:\nginx\nginx.exe -p C:\nginx -s reload
   ```
3. **502 문제**: 이 세션 맨 처음에 발견/수정한 포트 8081 충돌로 인한
   서버 크래시루프가 원인이었을 가능성이 높습니다(이미 그때 수정됨,
   `scripts\fix_port_conflict.bat`). 방화벽 스크립트 적용 후에도 502가
   계속되면 그때 다시 원인을 찾아드릴게요.
4. **`pastellive-server.new.exe`가 자동배포로 적용됐는지 확인**:
   ```
   schtasks /query /tn PastelliveApp
   type logs\auto_deploy.log
   ```

---

## 10. Kali Linux에서 재검증할 명령

```bash
# --- Origin 직접 접근 (방화벽 스크립트 적용 후 재검증) ---
# Origin_IP는 실제 서버 공인 IP로 교체. 적용 전엔 200이 나왔던 게,
# 적용 후엔 타임아웃/연결거부로 바뀌어야 정상.
curl -sS -o /dev/null -w "%{http_code}\n" --connect-timeout 5 \
  -H "Host: pastellive.co.kr" http://ORIGIN_IP/api/me

# 엉뚱한 Host로 접근했을 때 444(연결 종료)로 떨어지는지 확인
curl -sS -o /dev/null -w "%{http_code}\n" --connect-timeout 5 \
  -H "Host: this-is-not-a-real-host.example" http://ORIGIN_IP/

# --- TLS 버전/설정 확인 (Cloudflare 대시보드 변경 후) ---
nmap --script ssl-enum-ciphers -p 443 pastellive.co.kr
openssl s_client -connect pastellive.co.kr:443 -tls1 </dev/null 2>&1 | grep -i "no protocols\|handshake"
openssl s_client -connect pastellive.co.kr:443 -tls1_1 </dev/null 2>&1 | grep -i "no protocols\|handshake"
openssl s_client -connect pastellive.co.kr:443 -tls1_2 </dev/null 2>&1 | grep -i "handshake\|subject"

# --- 보안 헤더 확인 ---
curl -sS -D - -o /dev/null https://pastellive.co.kr/ | grep -Ei \
  "strict-transport-security|content-security-policy|x-frame-options|x-content-type-options|referrer-policy|permissions-policy"

# --- XSS 수정 확인 (실제로는 본인 계정 닉네임으로 직접 테스트 권장,
#     타인 계정/데이터로 테스트하지 말 것) ---
# 닉네임을 <script>alert(1)</script> 등으로 바꾼 뒤 프로필 페이지 HTML
# 소스에서 그대로 &lt;script&gt;... 로 이스케이프돼 나오는지 확인
curl -sS https://pastellive.co.kr/profile -H "Cookie: session=..." | grep -o '<h2 class="profile-main-name">[^<]*'

# --- 민감 파일/상태 코드 일관성 재확인 ---
curl -sS -o /dev/null -w "%{http_code}\n" https://pastellive.co.kr/.env
curl -sS -o /dev/null -w "%{http_code}\n" https://pastellive.co.kr/.git/HEAD
curl -sS -o /dev/null -w "%{http_code}\n" -X POST https://pastellive.co.kr/api/community/post
```

---

## 참고: 이번 점검에서 하지 않은 것 (요청 범위 밖 또는 이 세션 권한 밖)

- 실제 사용자 계정/데이터를 이용한 침투 테스트는 하지 않았음(코드 정적
  분석 + 격리된 환경에서의 라이브러리 동작 재현만 진행)
- Cloudflare 대시보드, DNS, WAF 규칙은 조회/변경 권한이 없어 직접 확인하지
  못했음(위 8번 절차대로 직접 확인 필요)
- Origin Full(Strict) TLS 전환은 Cloudflare Origin CA 인증서 발급이 선행돼야
  해서 이번엔 nginx 443 설정을 실제로 넣지 않음
- SRI(integrity 속성)는 코드에 아직 추가하지 않음 - 원하시면 다음 작업으로
  cdnjs/jsdelivr 리소스 전체에 대해 정확한 버전 고정 + integrity 해시를
  넣어드릴게요
