package handlers

import (
	"log"
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

	// [참여 유도: 포인트] 출석마다 기본 점수 + 7일 연속 출석 보너스.
	earnedPoints := models.PointsAttendance
	if consecutive > 0 && consecutive%7 == 0 {
		earnedPoints += models.PointsAttendanceWeek
	}
	if err := models.AwardPoints(a.DB, userID, "attendance", earnedPoints); err != nil {
		log.Printf("출석 포인트 적립 실패(user_id=%d): %v", userID, err)
	}

	writeJSON(w, map[string]any{"success": true, "total": total, "consecutive": consecutive, "points_earned": earnedPoints})
}
