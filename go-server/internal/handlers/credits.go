package handlers

// credits.go - 크레딧(출처) 페이지와 친절한 404 페이지
//
// /credits : static/images/pngimg/credits.json 에 적힌 PNGimg 이미지 출처를 모아 보여 준다.
//            이미지를 새로 추가하면 credits.json 에 한 줄만 추가하면 된다(서버 재시작 불필요).
// 404      : 없는 주소로 오면 길 잃은 문어 그림과 함께 홈으로 안내한다(/api/ 는 JSON 유지).

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pastellive/internal/httputil"
)

type creditItem struct {
	File       string `json:"file"`
	UsedFor    string `json:"used_for"`
	Source     string `json:"source"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
}

func (a *App) loadCredits() []map[string]any {
	raw, err := os.ReadFile(filepath.Join(a.Cfg.StaticDir, "images", "pngimg", "credits.json"))
	if err != nil {
		return nil
	}
	var items []creditItem
	if err := json.Unmarshal(raw, &items); err != nil {
		log.Printf("credits.json 읽기 실패(JSON 문법 확인): %v", err)
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, c := range items {
		out = append(out, map[string]any{
			"file": c.File, "used_for": c.UsedFor, "source": c.Source,
			"license": c.License, "license_url": c.LicenseURL,
		})
	}
	return out
}

// GET /credits
func (a *App) CreditsPageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := map[string]any{"request": requestContext(r), "credits": a.loadCredits()}
	if err := a.Templates.Render(w, r, "credits.html", ctx, a.GenRepImageOverrides); err != nil {
		log.Printf("크레딧 페이지 렌더 실패: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

const notFoundPage = `<!DOCTYPE html>
<html lang="ko">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta name="robots" content="noindex">
<title>페이지를 찾을 수 없어요 - 루미너스</title>
<style>
  body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
         background: #0f0f0f; color: #fff; font-family: 'Pretendard', -apple-system, "Malgun Gothic", sans-serif; }
  .box { text-align: center; padding: 40px 24px; max-width: 420px; }
  img { width: 180px; height: auto; margin: 0 auto 18px; display: block; animation: sway 3.2s ease-in-out infinite; }
  @keyframes sway { 0%, 100% { transform: rotate(-4deg); } 50% { transform: rotate(4deg); } }
  @media (prefers-reduced-motion: reduce) { img { animation: none; } }
  .code { color: #38bdf8; font-weight: 900; letter-spacing: 2px; font-size: 14px; margin: 0 0 6px; }
  h1 { font-size: 20px; margin: 0 0 10px; }
  p { font-size: 14px; color: #aaa; line-height: 1.6; margin: 0 0 22px; }
  a { display: inline-block; padding: 11px 22px; border-radius: 999px; background: #38bdf8; color: #0f0f0f;
      font-weight: 800; text-decoration: none; }
  a:hover { background: #7dd3fc; }
  .credit { margin-top: 26px; font-size: 11px; color: #555; }
  .credit a { all: unset; color: #666; text-decoration: underline; cursor: pointer; }
</style>
</head>
<body>
  <div class="box">
    <img src="/static/images/pngimg/octopus_lost.webp" alt="">
    <p class="code">404</p>
    <h1>여긴 아무것도 없어!</h1>
    <p>주소가 바뀌었거나 지워진 페이지 같아.<br>홈에서 다시 찾아볼래?</p>
    <a href="/">루미너스 홈으로</a>
    <div class="credit">그림: pngimg.com (CC BY-NC 4.0) · <a href="/credits">크레딧</a></div>
  </div>
</body>
</html>`

// NotFoundHandler 는 chi 라우터의 NotFound 로 등록된다.
func (a *App) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		httputil.JSONError(w, http.StatusNotFound, "없는 API예요.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(notFoundPage))
}
