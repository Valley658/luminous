package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

// ApiAdminSearchTrendsListHandler: 관리자 대시보드 "인기 검색어" 카드에서
// TOP 10만 보던 걸 넘어, 특정 검색어를 찾아보거나 전체 목록을 볼 수 있게 함.
func (a *App) ApiAdminSearchTrendsListHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	entries, err := models.ListSearchTrends(a.DB, q, limit)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "조회 실패: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "trends": entries})
}

func (a *App) ApiAdminSearchTrendsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	var body struct {
		Keyword string `json:"keyword"`
	}
	_ = decodeJSONBody(r, &body)
	keyword := strings.TrimSpace(body.Keyword)
	if keyword == "" {
		httputil.JSONError(w, http.StatusBadRequest, "keyword가 필요합니다.")
		return
	}
	if err := models.DeleteSearchTrend(a.DB, keyword); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "삭제 실패: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiAdminSearchTrendsDeleteAllHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	if err := models.DeleteAllSearchTrends(a.DB); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "전체 삭제 실패: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true})
}
