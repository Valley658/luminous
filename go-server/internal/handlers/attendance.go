package handlers

import (
	"net/http"
	"time"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func (a *App) ApiAttendanceStatusHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		writeJSON(w, map[string]any{"logged_in": false})
		return
	}
	today := models.KSTToday()
	attended, err := models.HasAttendedToday(a.DB, userID, today)
	if err != nil {
		writeJSON(w, map[string]any{"logged_in": true, "attended_today": false})
		return
	}
	writeJSON(w, map[string]any{"logged_in": true, "attended_today": attended})
}

func (a *App) ApiMarkAttendanceHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	todayTime := time.Now().In(models.KST)
	today := todayTime.Format("2006-01-02")
	yesterday := todayTime.AddDate(0, 0, -1).Format("2006-01-02")

	total, consecutive, alreadyDone, err := models.MarkAttendance(a.DB, userID, today, yesterday, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	if alreadyDone {
		writeJSON(w, map[string]any{"success": false, "message": "이미 오늘 출석을 완료했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true, "total": total, "consecutive": consecutive})
}
