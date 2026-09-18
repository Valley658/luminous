// lumi-admin은 리도님이 루미(AI 마스코트)에게 멤버별 상세 정보(소개/취향/기타
// 사실)를 직접 적어넣을 수 있게 해주는 아주 작은 로컬 전용 웹페이지다.
//
// 중요: 이 프로그램은 반드시 127.0.0.1(이 컴퓨터 자기 자신)에서만 열려야
// 하고, 절대로 Cloudflare Tunnel의 Public Hostname 라우트에 연결하면 안
// 된다 - 연결하는 순간 인터넷 어디서나 pastellive.co.kr을 통해 이 편집
// 페이지에 접근할 수 있게 되어버린다. main.go(메인 사이트)는 이미
// 127.0.0.1:8080에서만 듣고 그 포트만 터널에 연결돼있는데, 이 프로그램은
// 그것과 완전히 다른 포트(기본 8199)를 쓰고, 어떤 Cloudflare 라우트에도
// 절대 연결하지 않는 것으로 "이 PC 밖에서는 접근 불가"를 보장한다. 혹시
// 몰라 코드에서도 127.0.0.1/::1이 아닌 접속은 거부한다(이중 안전장치).
package main

import (
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"pastellive/internal/data"
	"pastellive/internal/lumiprofiles"
)

func main() {
	addr := os.Getenv("LUMI_ADMIN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8199"
	}
	path := lumiprofiles.DefaultPath()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleIndex(w, r, path)
	})
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		handleSave(w, r, path)
	})

	log.Printf("루미 데이터 편집 페이지 시작: http://%s  (데이터 파일: %s)", addr, path)
	log.Printf("주의: 이 주소는 이 컴퓨터에서만 열립니다 - Cloudflare 등 외부 터널에 절대 연결하지 마세요.")
	if err := http.ListenAndServe(addr, loopbackOnly(mux)); err != nil {
		log.Fatalf("서버 시작 실패: %v", err)
	}
}

// loopbackOnly는 127.0.0.1/::1이 아닌 곳에서 온 요청을 전부 거부하는
// 이중 안전장치 미들웨어. addr 자체가 127.0.0.1에 바인딩돼있어서 원래도
// 외부에서 직접 연결할 수는 없지만, 나중에 누군가 실수로 -addr을
// 0.0.0.0으로 바꾸거나 다른 프로그램이 포트포워딩을 걸어도 이 검사가
// 한 번 더 막아준다.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "이 페이지는 이 컴퓨터에서만 열 수 있어요.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func memberList() []data.Member {
	out := make([]data.Member, 0, len(data.SIDEBAR_MEMBERS))
	for _, m := range data.SIDEBAR_MEMBERS {
		if m.Name == "스텔라이브" {
			continue // 로고 항목, 실제 멤버 아님
		}
		out = append(out, m)
	}
	return out
}

type rowView struct {
	Name     string
	FullName string
	Profile  lumiprofiles.Profile
}

func handleIndex(w http.ResponseWriter, r *http.Request, path string) {
	profiles, err := lumiprofiles.Load(path)
	if err != nil {
		http.Error(w, "데이터 파일을 읽는 중 오류: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]rowView, 0, len(data.SIDEBAR_MEMBERS))
	for _, m := range memberList() {
		full := data.MEMBER_FULL_NAMES[m.Name]
		if full == "" {
			full = m.Name
		}
		rows = append(rows, rowView{Name: m.Name, FullName: full, Profile: profiles[m.Name]})
	}
	saved := r.URL.Query().Get("saved") == "1"
	if err := pageTpl.Execute(w, map[string]any{
		"Rows":  rows,
		"Saved": saved,
		"Path":  path,
	}); err != nil {
		log.Printf("템플릿 렌더링 오류: %v", err)
	}
}

func handleSave(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "폼을 읽는 중 오류: "+err.Error(), http.StatusBadRequest)
		return
	}
	profiles, err := lumiprofiles.Load(path)
	if err != nil {
		http.Error(w, "기존 데이터를 읽는 중 오류: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, m := range memberList() {
		p := lumiprofiles.Profile{
			Bio:      strings.TrimSpace(r.FormValue("bio_" + m.Name)),
			Likes:    strings.TrimSpace(r.FormValue("likes_" + m.Name)),
			Dislikes: strings.TrimSpace(r.FormValue("dislikes_" + m.Name)),
			Extra:    strings.TrimSpace(r.FormValue("extra_" + m.Name)),
		}
		if p.IsEmpty() {
			delete(profiles, m.Name)
		} else {
			profiles[m.Name] = p
		}
	}
	if err := lumiprofiles.Save(path, profiles); err != nil {
		http.Error(w, "저장 중 오류: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/?saved=1", http.StatusSeeOther)
}

var pageTpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<title>루미 데이터 편집 (로컬 전용)</title>
<style>
  body { font-family: -apple-system, "Malgun Gothic", sans-serif; background:#111318; color:#e8e8ec; max-width: 900px; margin: 0 auto; padding: 24px 20px 80px; }
  h1 { font-size: 20px; }
  .warn { background:#3a2a12; border:1px solid #7a5a1e; color:#f0c674; padding:10px 14px; border-radius:8px; font-size:13px; margin-bottom:20px; }
  .saved { background:#123a1e; border:1px solid #2f7a4a; color:#7af0a0; padding:10px 14px; border-radius:8px; font-size:13px; margin-bottom:20px; }
  .path { color:#888; font-size:12px; margin-bottom: 24px; word-break: break-all; }
  fieldset { border:1px solid #2a2d36; border-radius:10px; margin-bottom:16px; padding:14px 16px; }
  legend { padding:0 8px; font-weight:600; }
  label { display:block; font-size:12px; color:#aaa; margin:10px 0 4px; }
  textarea { width:100%; box-sizing:border-box; background:#1a1c22; border:1px solid #333844; color:#e8e8ec; border-radius:6px; padding:8px 10px; font-size:13px; resize:vertical; font-family:inherit; }
  button { margin-top:20px; background:#2563eb; color:#fff; border:none; border-radius:8px; padding:12px 24px; font-size:14px; cursor:pointer; }
  button:hover { background:#1d4ed8; }
</style>
</head>
<body>
<h1>루미 데이터 편집 (로컬 전용)</h1>
<div class="warn">이 페이지는 이 컴퓨터에서만 열려요. 여기서 저장한 내용은 루미(AI 채팅)가 멤버 관련 질문에 답할 때 그대로 참고합니다.</div>
{{if .Saved}}<div class="saved">저장했어요!</div>{{end}}
<div class="path">데이터 파일: {{.Path}}</div>
<form method="post" action="/save">
{{range .Rows}}
  <fieldset>
    <legend>{{.Name}} ({{.FullName}})</legend>
    <label>소개 / 성격 / 컨셉</label>
    <textarea name="bio_{{.Name}}" rows="2">{{.Profile.Bio}}</textarea>
    <label>좋아하는 것 / 취향</label>
    <textarea name="likes_{{.Name}}" rows="2">{{.Profile.Likes}}</textarea>
    <label>싫어하는 것 / 어려워하는 것</label>
    <textarea name="dislikes_{{.Name}}" rows="2">{{.Profile.Dislikes}}</textarea>
    <label>기타 사실 (유행어, 별명, 특징 등 자유롭게)</label>
    <textarea name="extra_{{.Name}}" rows="3">{{.Profile.Extra}}</textarea>
  </fieldset>
{{end}}
  <button type="submit">저장</button>
</form>
</body>
</html>
`))
