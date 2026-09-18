package handlers

import (
	"net/http"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func (a *App) ApiAddHistoryHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "Not logged in")
		return
	}
	var body struct {
		VideoID   string `json:"videoId"`
		Title     string `json:"title"`
		Thumbnail string `json:"thumbnail"`
	}
	_ = decodeJSONBody(r, &body)
	if body.VideoID == "" {
		httputil.JSON(w, http.StatusBadRequest, map[string]any{"success": false})
		return
	}
	title := body.Title
	if title == "" {
		title = "제목 없음"
	}
	if err := models.AddHistory(a.DB, userID, body.VideoID, title, body.Thumbnail, httputil.GetClientIP(r)); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiGetHistoryHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "Not logged in")
		return
	}
	items, err := models.GetHistory(a.DB, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	videos := make([]map[string]any, len(items))
	for i, h := range items {
		videos[i] = map[string]any{"videoId": h.VideoID, "title": h.Title, "thumbnail": h.Thumbnail}
	}
	writeJSON(w, map[string]any{"success": true, "videos": videos})
}

func (a *App) ApiDeleteHistoryItemsHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "Not logged in")
		return
	}
	var body struct {
		VideoIDs []string `json:"videoIds"`
	}
	_ = decodeJSONBody(r, &body)
	if len(body.VideoIDs) == 0 {
		httputil.JSONError(w, http.StatusBadRequest, "삭제할 항목이 없습니다.")
		return
	}
	if err := models.DeleteHistoryItems(a.DB, userID, body.VideoIDs); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiClearHistoryHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "Not logged in")
		return
	}
	if err := models.ClearHistory(a.DB, userID); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true})
}
