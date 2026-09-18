package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

func (a *App) ApiGetSchedulesHandler(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start")
	var startDate time.Time
	if startStr != "" {
		parsed, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			httputil.JSONError(w, http.StatusBadRequest, "날짜 형식이 올바르지 않습니다 (YYYY-MM-DD).")
			return
		}
		startDate = parsed
	} else {
		today := time.Now()

		pyWeekday := (int(today.Weekday()) + 6) % 7
		offset := (pyWeekday + 1) % 7
		startDate = today.AddDate(0, 0, -offset)
	}
	endDate := startDate.AddDate(0, 0, 6)

	schedules, err := models.GetSchedules(a.DB, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{
		"success":   true,
		"start":     startDate.Format("2006-01-02"),
		"end":       endDate.Format("2006-01-02"),
		"schedules": schedules,
	})
}

type scheduleRequestBody struct {
	MemberName string `json:"member_name"`
	EventDate  string `json:"event_date"`
	EventTime  string `json:"event_time"`
	Title      string `json:"title"`
	IsDayOff   bool   `json:"is_day_off"`
}

func validateSchedulePayload(body scheduleRequestBody) string {
	if len(body.MemberName) == 0 || len(body.MemberName) > 50 {
		return "멤버를 선택해주세요."
	}
	if body.EventDate == "" {
		return "날짜를 선택해주세요."
	}
	if _, err := time.Parse("2006-01-02", body.EventDate); err != nil {
		return "날짜 형식이 올바르지 않습니다."
	}
	if body.EventTime != "" {
		if _, err := time.Parse("15:04", body.EventTime); err != nil {
			return "시간 형식이 올바르지 않습니다 (HH:MM)."
		}
	}
	if !body.IsDayOff && body.Title == "" {
		return "방송 제목을 입력해주세요."
	}
	if len(body.Title) > 255 {
		return "제목이 너무 깁니다 (255자 이하)."
	}
	return ""
}

func resolveScheduleTitle(body scheduleRequestBody) string {
	if body.Title != "" {
		return body.Title
	}
	if body.IsDayOff {
		return "휴방"
	}
	return ""
}

func (a *App) ApiCreateScheduleHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	var body scheduleRequestBody
	_ = decodeJSONBody(r, &body)
	if msg := validateSchedulePayload(body); msg != "" {
		httputil.JSONError(w, http.StatusBadRequest, msg)
		return
	}
	var eventTime sql.NullString
	if body.EventTime != "" {
		eventTime = sql.NullString{String: body.EventTime, Valid: true}
	}
	newID, err := models.CreateSchedule(a.DB, body.MemberName, body.EventDate, eventTime, resolveScheduleTitle(body), body.IsDayOff, userID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "id": newID})
}

func (a *App) ApiUpdateScheduleHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	scheduleID, err := strconv.ParseInt(chi.URLParam(r, "scheduleID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	var body scheduleRequestBody
	_ = decodeJSONBody(r, &body)
	if msg := validateSchedulePayload(body); msg != "" {
		httputil.JSONError(w, http.StatusBadRequest, msg)
		return
	}
	exists, err := models.ScheduleExists(a.DB, scheduleID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if !exists {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 일정입니다.")
		return
	}
	var eventTime sql.NullString
	if body.EventTime != "" {
		eventTime = sql.NullString{String: body.EventTime, Valid: true}
	}
	if err := models.UpdateSchedule(a.DB, scheduleID, body.MemberName, body.EventDate, eventTime, resolveScheduleTitle(body), body.IsDayOff, userID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiDeleteScheduleHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	scheduleID, err := strconv.ParseInt(chi.URLParam(r, "scheduleID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	affected, err := models.DeleteSchedule(a.DB, scheduleID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if affected == 0 {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 일정입니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}
