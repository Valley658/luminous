package handlers

import (
	"net/http"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func (a *App) ApiGetNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	rows, err := models.ListNotifications(a.DB, userID, 30)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	unread, err := models.CountUnreadNotifications(a.DB, userID)
	if err != nil {
		unread = 0
	}
	list := make([]map[string]any, len(rows))
	for i, n := range rows {
		list[i] = map[string]any{
			"id":             n.ID,
			"actor_nickname": n.ActorNickname.String,
			"type":           n.Type,
			"target_type":    n.TargetType,
			"target_id":      n.TargetID,
			"preview_text":   n.PreviewText.String,
			"is_read":        n.IsRead,
			"date":           formatDateShortLocal(n.CreatedAt),
		}
	}
	writeJSON(w, map[string]any{"success": true, "notifications": list, "unread_count": unread})
}

func (a *App) ApiMarkNotificationsReadHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	var body struct {
		ID *int64 `json:"id"`
	}
	_ = decodeJSONBody(r, &body)

	var err error
	if body.ID != nil {
		err = models.MarkNotificationRead(a.DB, userID, *body.ID)
	} else {
		err = models.MarkAllNotificationsRead(a.DB, userID)
	}
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}
