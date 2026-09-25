package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func (a *App) ReportsPageHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/stats", http.StatusFound)
}

func (a *App) ApiGetReportsHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	statusFilter := r.URL.Query().Get("status")
	if statusFilter == "" {
		statusFilter = "pending"
	}
	rows, err := models.ListFanartReports(a.DB, statusFilter)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	reports := make([]map[string]any, len(rows))
	for i, row := range rows {
		reports[i] = row.ToMap()
	}
	writeJSON(w, map[string]any{"success": true, "reports": reports})
}

func (a *App) ApiResolveReportHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "reportID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	if _, err := models.ResolveFanartReport(a.DB, reportID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	a.logAdminAction(r, "report_resolve", "fanart_report", strconv.FormatInt(reportID, 10), "")
	writeJSON(w, map[string]any{"success": true})
}

// ApiGetReportFlagsHandler는 [신고 처리 자동화]로 자동 플래그된(반복적으로
// 신고당해 게시물이 삭제된) 사용자 목록을 돌려준다(관리자 전용).
func (a *App) ApiGetReportFlagsHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	statusFilter := r.URL.Query().Get("status")
	if statusFilter == "" {
		statusFilter = "open"
	}
	rows, err := models.ListReportFlags(a.DB, statusFilter)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	flags := make([]map[string]any, len(rows))
	for i, row := range rows {
		flags[i] = row.ToMap()
	}
	writeJSON(w, map[string]any{"success": true, "flags": flags})
}

// ApiResolveReportFlagHandler는 관리자가 검토를 마친 자동 플래그를
// "resolved"로 표시한다(계정에는 아무 조치도 하지 않음 - 별도로 처리해야 함).
func (a *App) ApiResolveReportFlagHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	targetUserID, err := strconv.ParseInt(chi.URLParam(r, "targetUserID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	if _, err := models.ResolveReportFlag(a.DB, targetUserID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	a.logAdminAction(r, "report_flag_resolve", "user", strconv.FormatInt(targetUserID, 10), "")
	writeJSON(w, map[string]any{"success": true})
}
