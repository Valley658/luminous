package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func (a *App) PlaylistsPageHandler(w http.ResponseWriter, r *http.Request) {
	a.Index(w, r)
}

func (a *App) BookmarksPageHandler(w http.ResponseWriter, r *http.Request) {
	a.Index(w, r)
}

func (a *App) ApiCheckBookmarkHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	videoID := r.URL.Query().Get("videoId")
	if userID == 0 || videoID == "" {
		writeJSON(w, map[string]any{"bookmarked": false})
		return
	}
	bookmarked, err := models.CheckBookmark(a.DB, userID, videoID)
	if err != nil {
		writeJSON(w, map[string]any{"bookmarked": false})
		return
	}
	writeJSON(w, map[string]any{"bookmarked": bookmarked})
}

func (a *App) ApiBookmarksHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			VideoID   string `json:"videoId"`
			Title     string `json:"title"`
			Thumbnail string `json:"thumbnail"`
		}
		_ = decodeJSONBody(r, &body)
		if body.VideoID == "" {
			httputil.JSONError(w, http.StatusBadRequest, "videoId가 없습니다.")
			return
		}
		title := body.Title
		if title == "" {
			title = "제목 없음"
		}
		bookmarked, err := models.ToggleBookmark(a.DB, userID, body.VideoID, title, body.Thumbnail, httputil.GetClientIP(r))
		if err != nil {
			httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
			return
		}
		message := "북마크에 추가되었습니다."
		if !bookmarked {
			message = "북마크가 취소되었습니다."
		}
		writeJSON(w, map[string]any{"success": true, "bookmarked": bookmarked, "message": message})
		return
	}

	bookmarks, err := models.GetBookmarks(a.DB, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	out := make([]map[string]any, len(bookmarks))
	for i, b := range bookmarks {
		out[i] = map[string]any{"videoId": b.VideoID, "title": b.Title, "thumbnail": b.Thumbnail, "date": b.Date}
	}
	writeJSON(w, map[string]any{"success": true, "bookmarks": out})
}

func (a *App) ApiPlaylistsHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Title   string `json:"title"`
			Privacy string `json:"privacy"`
		}
		_ = decodeJSONBody(r, &body)
		title := strings.TrimSpace(body.Title)
		if title == "" {
			httputil.JSONError(w, http.StatusBadRequest, "제목을 입력해주세요.")
			return
		}
		privacy := body.Privacy
		if privacy == "" {
			privacy = "private"
		}
		playlistID, err := models.CreatePlaylist(a.DB, userID, title, privacy, httputil.GetClientIP(r))
		if err != nil {
			httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
			return
		}
		writeJSON(w, map[string]any{"success": true, "playlist_id": playlistID, "privacy": privacy})
		return
	}

	playlists, err := models.GetPlaylists(a.DB, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	out := make([]map[string]any, len(playlists))
	for i, p := range playlists {
		out[i] = map[string]any{"playlist_id": p.PlaylistID, "title": p.Title, "privacy": p.Privacy, "created_at": p.CreatedAt, "video_count": p.VideoCount}
	}
	writeJSON(w, map[string]any{"success": true, "playlists": out})
}

// ApiAddVideoToPlaylistHandler adds a video to one of the current user's
// playlists (used by the "재생목록에 추가" add-to-playlist mode on the home
// page).
func (a *App) ApiAddVideoToPlaylistHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	playlistID := chi.URLParam(r, "playlistID")
	owner, err := models.GetPlaylistOwner(a.DB, playlistID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if owner == 0 || owner != userID {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	var body struct {
		VideoID   string `json:"videoId"`
		Title     string `json:"title"`
		Thumbnail string `json:"thumbnail"`
	}
	_ = decodeJSONBody(r, &body)
	if body.VideoID == "" {
		httputil.JSONError(w, http.StatusBadRequest, "videoId가 없습니다.")
		return
	}
	added, err := models.AddVideoToPlaylist(a.DB, playlistID, body.VideoID, body.Title, body.Thumbnail)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	message := "재생목록에 추가되었습니다."
	if !added {
		message = "이미 재생목록에 있는 영상입니다."
	}
	writeJSON(w, map[string]any{"success": true, "added": added, "message": message})
}
