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
