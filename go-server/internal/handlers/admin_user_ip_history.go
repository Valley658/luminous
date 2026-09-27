package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

// ApiAdminUserIPHistoryHandler: 회원 관리 탭에서 특정 회원이 과거에 접속했던
// IP 이력을 최신순으로 보여준다 (user_login_log - 로그인 성공 시마다 기록됨).
func (a *App) ApiAdminUserIPHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "targetUserID"), 10, 64)
	if err != nil || userID <= 0 {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 회원 ID입니다.")
		return
	}
	history, err := models.GetUserLoginHistory(a.DB, userID, 50)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "조회 실패: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "history": history})
}
