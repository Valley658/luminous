package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func (a *App) ChannelPage(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	_, ok, err := models.GetUserIDByChannelID(a.DB, channelID)
	if err != nil || !ok {
		http.NotFound(w, r)
		return
	}

	if err := a.Templates.Render(w, r, "channel.html", map[string]any{
		"channel_id": channelID, "request": requestContext(r),
	}, a.GenRepImageOverrides); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (a *App) ApiGetChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	viewerID := sessionUserID(r)

	owner, err := models.GetChannelOwnerByChannelID(a.DB, channelID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "채널 정보를 불러오지 못했습니다.")
		return
	}
	if owner == nil {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 채널입니다.")
		return
	}

	subscriberCount, err := models.CountSubscribers(a.DB, owner.ID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "채널 정보를 불러오지 못했습니다.")
		return
	}
	videoCount, viewCount, err := models.CountChannelVideosAndViews(a.DB, owner.ID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "채널 정보를 불러오지 못했습니다.")
		return
	}
	isSubscribed := false
	if viewerID != 0 {
		isSubscribed, _ = models.IsSubscribed(a.DB, viewerID, owner.ID)
	}
	videos, err := models.ListChannelVideos(a.DB, owner.ID, 0)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "채널 정보를 불러오지 못했습니다.")
		return
	}
	videoMaps := make([]map[string]any, len(videos))
	for i, v := range videos {
		videoMaps[i] = map[string]any{
			"id": v.ID, "title": v.Title, "description": v.Description.String,
			"video_url": v.VideoURL, "thumbnail": v.Thumbnail.String,
			"view_count": v.ViewCount, "created_at": v.CreatedAt.String,
		}
	}
	posts, err := models.ListCommunityPostsByUser(a.DB, owner.ID, 50)
	if err != nil {
		posts = []map[string]any{}
	}

	joinedAt := any(nil)
	if owner.CreatedAt.Valid && len(owner.CreatedAt.String) >= 10 {
		joinedAt = owner.CreatedAt.String[:10]
	}

	writeJSON(w, map[string]any{
		"success":       true,
		"channel_id":    channelID,
		"is_owner":      viewerID != 0 && viewerID == owner.ID,
		"is_subscribed": isSubscribed,
		"channel": map[string]any{
			"nickname": owner.Nickname.String, "picture": owner.Picture.String,
			"joined_at": joinedAt, "description": owner.ChannelDescription.String,
			"link": owner.ChannelLink.String, "country": owner.ChannelCountry.String,
			"subscriber_count": subscriberCount, "video_count": videoCount, "view_count": viewCount,
		},
		"videos": videoMaps,
		"posts":  posts,
	})
}

func (a *App) ApiChannelSubscribe(w http.ResponseWriter, r *http.Request) {
	subscriberID := sessionUserID(r)
	if subscriberID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	var body struct {
		ChannelID string `json:"channel_id"`
	}
	_ = decodeJSONBody(r, &body)
	if body.ChannelID == "" {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}

	channelUserID, ok, err := models.GetUserIDByChannelID(a.DB, body.ChannelID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if !ok {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 채널입니다.")
		return
	}
	if channelUserID == subscriberID {
		httputil.JSONError(w, http.StatusBadRequest, "본인 채널은 구독할 수 없습니다.")
		return
	}

	subscribed, err := models.ToggleSubscription(a.DB, subscriberID, channelUserID, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	subscriberCount, _ := models.CountSubscribers(a.DB, channelUserID)
	writeJSON(w, map[string]any{"success": true, "subscribed": subscribed, "subscriber_count": subscriberCount})
}
