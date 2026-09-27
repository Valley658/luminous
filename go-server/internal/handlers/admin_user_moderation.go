package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func parseTargetUserID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "targetUserID"), 10, 64)
}

// ApiAdminUserStaffHandler: 회원을 "운영진" 배지로 표시/해제한다. 실제 관리자
// 페이지 접근 권한(isAdmin)은 owner 계정 하나로 하드코딩돼 있어 여기서 건드리지
// 않음 - 이건 순수하게 회원 목록에 보여주는 라벨일 뿐이다.
func (a *App) ApiAdminUserStaffHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	userID, err := parseTargetUserID(r)
	if err != nil || userID <= 0 {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 회원 ID입니다.")
		return
	}
	var body struct {
		IsStaff   bool   `json:"is_staff"`
		StaffRole string `json:"staff_role"`
	}
	_ = decodeJSONBody(r, &body)
	if err := models.SetUserStaff(a.DB, userID, body.IsStaff, strings.TrimSpace(body.StaffRole)); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "저장 실패: "+err.Error())
		return
	}
	a.logAdminAction(r, "user_staff_update", "user", strconv.FormatInt(userID, 10), body.StaffRole)
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiAdminUserSuspendHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	userID, err := parseTargetUserID(r)
	if err != nil || userID <= 0 {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 회원 ID입니다.")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSONBody(r, &body)
	if err := models.SuspendUser(a.DB, userID, strings.TrimSpace(body.Reason)); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "정지 실패: "+err.Error())
		return
	}
	a.logAdminAction(r, "user_suspend", "user", strconv.FormatInt(userID, 10), body.Reason)
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiAdminUserUnsuspendHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	userID, err := parseTargetUserID(r)
	if err != nil || userID <= 0 {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 회원 ID입니다.")
		return
	}
	if err := models.UnsuspendUser(a.DB, userID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "정지 해제 실패: "+err.Error())
		return
	}
	a.logAdminAction(r, "user_unsuspend", "user", strconv.FormatInt(userID, 10), "")
	writeJSON(w, map[string]any{"success": true})
}

// ApiAdminUserSummaryHandler: 회원 상세 패널 - 이 회원의 활동 요약(팬아트/댓글/
// 게시글/신고당한 횟수/출석일수/포인트/로그인 횟수)을 한 번에 내려준다.
func (a *App) ApiAdminUserSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	userID, err := parseTargetUserID(r)
	if err != nil || userID <= 0 {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 회원 ID입니다.")
		return
	}
	summary := models.GetUserActivitySummary(a.DB, userID)
	writeJSON(w, map[string]any{"success": true, "summary": summary})
}
