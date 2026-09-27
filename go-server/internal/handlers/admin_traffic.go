package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

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
	byIP, err := models.TrafficByIPToday(a.DB, 50)
	if err != nil {
		byIP = nil
	}
	bannedIPs, err := models.ListBannedIPs(a.DB)
	if err != nil {
		bannedIPs = nil
	}

	resp := map[string]any{
		"success":    true,
		"daily":      daily,
		"hourly":     hourly,
		"online_now": onlineNow,
		"top_paths":  topPaths,
		"by_ip":      byIP,
		"banned_ips": bannedIPs,
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

// ApiAdminTrafficIPDetailHandler: 특정 IP를 눌렀을 때(와이어샤크의 패킷
// 상세보기처럼) 오늘 그 IP가 실제로 요청한 경로들을 시간순으로 보여준다.
func (a *App) ApiAdminTrafficIPDetailHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	ip := strings.TrimSpace(chi.URLParam(r, "ip"))
	if ip == "" {
		httputil.JSONError(w, http.StatusBadRequest, "IP가 필요합니다.")
		return
	}
	views, err := models.RecentPageViewsForIP(a.DB, ip, 200)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "조회 실패: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "ip": ip, "views": views, "banned": a.BanList != nil && a.BanList.IsBanned(ip)})
}

// ApiAdminIPBanHandler: 관리자가 IP를 수동으로 영구 차단한다 (rate limiter의
// 자동/일시 차단과 별개 - "이 IP는 무조건 막아").
func (a *App) ApiAdminIPBanHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	var body struct {
		IP     string `json:"ip"`
		Reason string `json:"reason"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	ip := strings.TrimSpace(body.IP)
	if ip == "" {
		httputil.JSONError(w, http.StatusBadRequest, "IP가 필요합니다.")
		return
	}
	if err := a.BanList.Ban(a.DB, ip, strings.TrimSpace(body.Reason)); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "차단 실패: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiAdminIPUnbanHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	var body struct {
		IP string `json:"ip"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	ip := strings.TrimSpace(body.IP)
	if ip == "" {
		httputil.JSONError(w, http.StatusBadRequest, "IP가 필요합니다.")
		return
	}
	if err := a.BanList.Unban(a.DB, ip); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "차단 해제 실패: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true})
}
