package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"pastellive/internal/data"
	"pastellive/internal/middleware"
	"pastellive/internal/video"
)

func isMobileRequest(r *http.Request) bool {
	if strings.HasPrefix(r.Host, "m.") {
		return true
	}
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	return strings.Contains(ua, "mobile") || strings.Contains(ua, "android") || strings.Contains(ua, "iphone")
}

func sessionUserID(r *http.Request) int64 {
	sess := middleware.GetSession(r)
	if sess == nil {
		return 0
	}
	return sess.GetInt64("user_id")
}

func isShortVideo(v video.Video) bool {
	return video.IsShortTitle(v)
}

func (a *App) getHomeShortsVideos(ctx context.Context, userID int64) []video.Video {
	a.VideoPool.RefreshIfEmpty(ctx, a.DB, a.Cfg.YoutubeAPIKey)
	pool := a.VideoPool.Get()
	shorts := make([]video.Video, 0, len(pool))
	for _, v := range pool {
		if isShortVideo(v) {
			shorts = append(shorts, v)
		}
	}
	return video.GetPersonalizedVideoOrder(a.DB, a.Cache, shorts, userID, 0.30)
}

func isMobileVideoTabItem(v video.Video) bool {
	title := strings.ToLower(v.Title)
	isShort := v.IsShort || strings.Contains(title, "shorts") || strings.Contains(v.Title, "쇼츠")
	musicKeywords := []string{"cover", "커버", "mv", "m/v", "노래", "music", "음원", "song", "ost"}
	isMusic := false
	for _, k := range musicKeywords {
		if strings.Contains(title, k) {
			isMusic = true
			break
		}
	}
	return !isShort && !isMusic
}

func requestContext(r *http.Request) map[string]any {
	return map[string]any{"path": r.URL.Path}
}

func (a *App) Index(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
	pool := a.VideoPool.Get()

	if isMobileRequest(r) {
		mobilePool := video.GetPersonalizedVideoOrder(a.DB, a.Cache, pool, userID, 0.30)

		mobileCards := make([]video.Video, 0, 10)
		for _, v := range mobilePool {
			if isMobileVideoTabItem(v) {
				mobileCards = append(mobileCards, v)
				if len(mobileCards) >= 10 {
					break
				}
			}
		}
		ids := make([]string, len(mobileCards))
		for i, v := range mobileCards {
			ids[i] = v.VideoID
		}
		previews := a.batchCommentPreviewsSiteOnly(ids)
		for i := range mobileCards {
			mobileCards[i].CommentPreview = previews[mobileCards[i].VideoID]
		}

		w.Header().Set("Vary", "User-Agent")
		err := a.Templates.Render(w, r, "m.html", map[string]any{
			"init_videos":             video.ToTemplateMaps(mobilePool),
			"init_mobile_video_cards": video.ToTemplateMaps(mobileCards),
			"request":                 requestContext(r),
		}, a.GenRepImageOverrides)
		if err != nil {
			log.Printf("m.html 렌더링 실패: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	longVideos := make([]video.Video, 0, len(pool))
	for _, v := range pool {
		if !isShortVideo(v) {
			longVideos = append(longVideos, v)
		}
	}
	longVideos = video.GetPersonalizedVideoOrder(a.DB, a.Cache, longVideos, userID, 0.30)
	if len(longVideos) > 24 {
		longVideos = longVideos[:24]
	}
	homeShorts := a.getHomeShortsVideos(r.Context(), userID)
	if len(homeShorts) > 15 {
		homeShorts = homeShorts[:15]
	}

	sidebarGroups := data.BuildSidebarGroups(a.GenRepImageOverrides)

	w.Header().Set("Vary", "User-Agent")
	err := a.Templates.Render(w, r, "index.html", map[string]any{
		"init_videos":              video.ToTemplateMaps(longVideos),
		"init_home_shorts":         video.ToTemplateMaps(homeShorts),
		"sidebar_members":          data.MembersToTemplateMaps(data.SIDEBAR_MEMBERS),
		"sidebar_generations":      data.GroupedGenerationsToTemplateMaps(sidebarGroups.Generations),
		"sidebar_ungrouped_top":    data.MembersToTemplateMaps(sidebarGroups.UngroupedTop),
		"sidebar_ungrouped_bottom": data.MembersToTemplateMaps(sidebarGroups.UngroupedBot),
		"request":                  requestContext(r),
	}, a.GenRepImageOverrides)
	if err != nil {
		log.Printf("index.html 렌더링 실패: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (a *App) ApiMShorts(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
	pool := a.VideoPool.Get()
	shorts := make([]video.Video, 0, len(pool))
	for _, v := range pool {
		if isShortVideo(v) {
			shorts = append(shorts, v)
		}
	}
	shorts = video.GetPersonalizedVideoOrder(a.DB, a.Cache, shorts, userID, 0.30)
	if len(shorts) > 10 {
		shorts = shorts[:10]
	}
	writeJSON(w, map[string]any{"success": true, "videos": shorts})
}

func (a *App) ApiRandomScroll(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)

	excluded := make(map[string]bool)
	if r.Method == http.MethodPost {
		var body struct {
			Exclude []string `json:"exclude"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, id := range body.Exclude {
			if id != "" {
				excluded[id] = true
			}
		}
	} else {
		raw := r.URL.Query().Get("exclude")
		if raw != "" {
			for _, id := range strings.Split(raw, ",") {
				if id != "" {
					excluded[id] = true
				}
			}
		}
	}

	pool := a.VideoPool.Get()
	longVideos := make([]video.Video, 0, len(pool))
	for _, v := range pool {
		if v.VideoID == "" || excluded[v.VideoID] {
			continue
		}
		if !isShortVideo(v) {
			longVideos = append(longVideos, v)
		}
	}
	longVideos = video.GetPersonalizedVideoOrder(a.DB, a.Cache, longVideos, userID, 0.30)
	remaining := len(longVideos)
	picked := longVideos
	if len(picked) > 12 {
		picked = picked[:12]
	}

	ids := make([]string, len(picked))
	for i, v := range picked {
		ids[i] = v.VideoID
	}
	previews := a.batchCommentPreviewsSiteOnly(ids)
	for i := range picked {
		picked[i].CommentPreview = previews[picked[i].VideoID]
	}

	var nextPageToken any
	if remaining > len(picked) {
		nextPageToken = "has_more_random"
	}
	writeJSON(w, map[string]any{"videos": picked, "nextPageToken": nextPageToken, "remaining": remaining})
}

func (a *App) ApiRecommendRandom(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
	pool := a.VideoPool.Get()
	recommendPool := video.GetPersonalizedVideoOrder(a.DB, a.Cache, pool, userID, 0.30)
	if len(recommendPool) > 50 {
		recommendPool = recommendPool[:50]
	}
	writeJSON(w, map[string]any{"videos": recommendPool, "nextPageToken": nil})
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}
