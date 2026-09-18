package handlers

import (
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/video"
)

func (a *App) ApiYoutubeCommentsHandler(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	pageToken := r.URL.Query().Get("pageToken")
	result := video.FetchYouTubeComments(r.Context(), videoID, pageToken, a.Cfg.YoutubeAPIKey, 30)
	writeJSON(w, result)
}

var storyboardVideoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)

func (a *App) ApiVideoStoryboardHandler(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	if !storyboardVideoIDPattern.MatchString(videoID) {
		httputil.JSON(w, http.StatusBadRequest, map[string]any{"success": false})
		return
	}
	cacheKey := "storyboard_" + videoID
	if cached, ok := a.Cache.Get(cacheKey); ok {
		writeJSON(w, cached)
		return
	}
	result, status := video.FetchVideoStoryboard(r.Context(), videoID)
	if result.Success {
		a.Cache.Set(cacheKey, result, time.Hour)
		writeJSON(w, result)
		return
	}
	httputil.JSON(w, status, result)
}
