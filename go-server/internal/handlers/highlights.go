package handlers

import (
	"database/sql"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/data"
	"pastellive/internal/httputil"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

var highlightVideoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)

func (a *App) ApiHighlightTagHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인 후 하이라이트를 태그할 수 있습니다.")
		return
	}
	nickname := "익명스텔리언"
	if sess := middleware.GetSession(r); sess != nil {
		if n := sess.GetString("user_nickname"); n != "" {
			nickname = n
		}
	}

	var body struct {
		VideoID         any `json:"videoId"`
		Tag             any `json:"tag"`
		StartSeconds    any `json:"startSeconds"`
		DurationSeconds any `json:"durationSeconds"`
	}
	_ = decodeJSONBody(r, &body)

	sourceVideoID := strings.TrimSpace(anyToString(body.VideoID))
	tagText := strings.TrimSpace(anyToString(body.Tag))
	if len(tagText) > 200 {
		tagText = tagText[:200]
	}

	startSeconds, ok := anyToInt64(body.StartSeconds)
	if !ok {
		httputil.JSONError(w, http.StatusBadRequest, "시작 시간이 올바르지 않습니다.")
		return
	}
	durationSeconds, ok := anyToInt64(body.DurationSeconds)
	if !ok {
		durationSeconds = 15
	}

	if !highlightVideoIDPattern.MatchString(sourceVideoID) {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 영상 ID입니다.")
		return
	}
	if tagText == "" {
		httputil.JSONError(w, http.StatusBadRequest, "태그 내용을 입력해 주세요.")
		return
	}
	if startSeconds < 0 || startSeconds > 24*3600 {
		httputil.JSONError(w, http.StatusBadRequest, "시작 시간이 올바르지 않습니다.")
		return
	}
	if durationSeconds < 5 {
		durationSeconds = 5
	} else if durationSeconds > 30 {
		durationSeconds = 30
	}

	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
	var sourceTitle, memberName sql.NullString
	for _, v := range a.VideoPool.Get() {
		if v.VideoID == sourceVideoID {
			sourceTitle = strToNull(v.Title)
			memberName = strToNull(v.MemberName)
			break
		}
	}

	clipID, err := models.InsertHighlightClip(a.DB, sourceVideoID, sourceTitle, memberName, int(startSeconds), int(durationSeconds), tagText, httputil.GetClientIP(r), userID, nickname)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "태그 저장 실패: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"success": true, "clipId": clipID, "status": "pending",
		"message": "하이라이트 클립을 만들고 있어요. 완료되면 하이라이트 갤러리에 올라와요.",
	})
}

func anyToString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return ""
	}
}

func (a *App) ApiHighlightStatusHandler(w http.ResponseWriter, r *http.Request) {
	clipID, err := strconv.ParseInt(chi.URLParam(r, "clipID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "존재하지 않는 클립입니다.")
		return
	}
	clip, found, err := models.GetHighlightClipStatus(a.DB, clipID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	if !found {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 클립입니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "clip": clip})
}

func (a *App) ApiHighlightsListHandler(w http.ResponseWriter, r *http.Request) {
	member := strings.TrimSpace(r.URL.Query().Get("member"))
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	} else if limit > 60 {
		limit = 60
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	if offset < 0 {
		offset = 0
	}

	cacheKey := "api_highlights_list:" + member + ":" + strconv.Itoa(limit) + ":" + strconv.Itoa(offset)
	if cached, ok := a.Cache.Get(cacheKey); ok {
		writeJSON(w, cached)
		return
	}

	clips, err := models.ListHighlightClips(a.DB, member, limit, offset)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	result := map[string]any{"success": true, "clips": clips}
	a.Cache.Set(cacheKey, result, 60*time.Second)
	writeJSON(w, result)
}

func (a *App) HighlightsGalleryPageHandler(w http.ResponseWriter, r *http.Request) {
	err := a.Templates.Render(w, r, "highlights.html", map[string]any{
		"sidebar_members": data.MembersToTemplateMaps(data.SIDEBAR_MEMBERS),
		"request":         requestContext(r),
	}, a.GenRepImageOverrides)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
