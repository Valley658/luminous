package handlers

import (
	"log"
	"net/http"

	"pastellive/internal/httputil"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
	"pastellive/internal/video"
)

func (a *App) ProfilePageHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {

		if sess := middleware.GetSession(r); sess != nil {
			sess.Clear()
		}
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if err := a.Templates.Render(w, r, "profile.html", map[string]any{
		"user": user.ToTemplateMap(),
	}, a.GenRepImageOverrides); err != nil {
		log.Printf("profile.html 렌더링 실패: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (a *App) ApiProfileMyVideosHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	videos, err := models.GetMyProfileVideos(a.DB, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	out := make([]map[string]any, len(videos))
	for i, v := range videos {
		out[i] = map[string]any{
			"id": v.ID, "title": v.Title, "description": v.Description, "video_url": v.VideoURL,
			"thumbnail": v.Thumbnail, "view_count": v.ViewCount, "date": v.Date,
		}
	}
	writeJSON(w, map[string]any{"success": true, "videos": out})
}

func (a *App) ApiProfileLikedVideosHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	videoIDs, err := models.GetMyLikedVideoIDs(a.DB, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	if len(videoIDs) == 0 {
		writeJSON(w, map[string]any{"success": true, "videos": []any{}})
		return
	}

	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
	poolByID := make(map[string]video.Video)
	for _, v := range a.VideoPool.Get() {
		if v.VideoID != "" {
			poolByID[v.VideoID] = v
		}
	}

	out := make([]map[string]any, len(videoIDs))
	for i, vid := range videoIDs {
		info, ok := poolByID[vid]
		title, thumbnail, memberName := "", "", ""
		if ok {
			title = info.Title
			thumbnail = info.Thumbnail
			memberName = info.MemberName
		}
		if thumbnail == "" {
			thumbnail = "https://i.ytimg.com/vi/" + vid + "/hqdefault.jpg"
		}
		out[i] = map[string]any{"videoId": vid, "title": title, "thumbnail": thumbnail, "member_name": memberName}
	}
	writeJSON(w, map[string]any{"success": true, "videos": out})
}

func (a *App) ApiProfileMyPostsHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	posts, err := models.GetMyProfilePosts(a.DB, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	out := make([]map[string]any, len(posts))
	for i, p := range posts {
		out[i] = map[string]any{
			"id": p.ID, "member_name": p.MemberName, "title": p.Title, "content": p.Content,
			"image_url": p.ImageURL, "video_url": p.VideoURL, "date": p.Date,
			"likes_count": p.LikesCount, "comments_count": p.CommentsCount,
		}
	}
	writeJSON(w, map[string]any{"success": true, "posts": out})
}
