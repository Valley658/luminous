package handlers

import (
	"net/http"
	"strings"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

// trafficIgnoredPrefixes: 이 접두사로 시작하는 경로는 "페이지 조회"로 치지
// 않는다 - API 호출, 정적 파일, 서비스워커/사이트맵 등 사람이 실제로 눈으로
// 보는 페이지가 아닌 요청들. 관리자 대시보드 자신의 폴링(/admin, /api/admin)도
// 제외해서 관리자 새로고침이 트래픽 통계를 오염시키지 않게 한다.
var trafficIgnoredPrefixes = []string{
	"/api/", "/static/", "/admin", "/sw.js", "/robots.txt", "/sitemap.xml",
	"/favicon.ico", "/__sentry_verify__", "/logout", "/login/discord",
}

func isTrackablePageView(path string) bool {
	for _, p := range trafficIgnoredPrefixes {
		if strings.HasPrefix(path, p) {
			return false
		}
	}
	return true
}

// TrackPageView: 실제 페이지 GET 요청을 page_views 테이블에 비동기로 기록하는
// 미들웨어. 관리자 대시보드의 "트래픽" 탭(일별/시간대별 방문자·페이지뷰 추이,
// 실시간 접속자 수)의 데이터 소스. DB insert가 응답 지연에 영향을 주지 않도록
// 고루틴으로 던지고 실패해도(트래픽 로깅 실패로 실제 페이지 응답이 막히면
// 안 되니) 조용히 로그만 남긴다.
func (a *App) TrackPageView(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && isTrackablePageView(r.URL.Path) {
			path := r.URL.Path
			ip := httputil.GetClientIP(r)
			db := a.DB
			go func() {
				_ = models.LogPageView(db, path, ip)
			}()
		}
		next.ServeHTTP(w, r)
	})
}
