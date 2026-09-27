package handlers

import (
	"net/http"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

// ApiAdminTrafficHandler: 관리자 대시보드 "트래픽" 탭 - 예전 "트래픽 제한"
// 탭을 대체하는 실제 방문자/페이지뷰 검사 데이터. 기존 rate-limit 정보(요청
// 제한 현황)도 그대로 같이 내려줘서, 이 기능이 꺼져 있어도(RATE_LIMIT_ENABLED
// =false) 트래픽 자체는 항상 보이게 한다.
func (a *App) ApiAdminTrafficHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}

	daily, err := models.DailyPageViews(a.DB, 14)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "일별 트래픽 조회 실패: "+err.Error())
		return
	}
	hourly, err := models.HourlyPageViewsToday(a.DB)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "시간대별 트래픽 조회 실패: "+err.Error())
		return
	}
	onlineNow, err := models.OnlineNowCount(a.DB, 5)
	if err != nil {
		onlineNow = 0
	}
	topPaths, err := models.TopPathsToday(a.DB, 10)
	if err != nil {
		topPaths = nil
	}

	resp := map[string]any{
		"success":    true,
		"daily":      daily,
		"hourly":     hourly,
		"online_now": onlineNow,
		"top_paths":  topPaths,
	}

	if a.RateLimiter != nil {
		enabled, windowSec, maxRequests, banMinutes := a.RateLimiter.Config()
		resp["rate_limit_enabled"] = enabled
		resp["rate_limit_window_sec"] = windowSec
		resp["rate_limit_max_requests"] = maxRequests
		resp["rate_limit_ban_minutes"] = banMinutes
		resp["rate_limit_ips"] = a.RateLimiter.Snapshot()
	} else {
		resp["rate_limit_enabled"] = false
		resp["rate_limit_ips"] = []any{}
	}

	writeJSON(w, resp)
}
