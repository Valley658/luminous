package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var (
	watchFullMatchPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)
	watchSearchPattern    = regexp.MustCompile(`[a-zA-Z0-9_-]{11}`)
)

func (a *App) WatchVideoHandler(w http.ResponseWriter, r *http.Request) {
	targetVideoID := strings.TrimSpace(chi.URLParam(r, "videoID"))
	isDrive := false
	if watchFullMatchPattern.MatchString(targetVideoID) {
		isDrive = false
	} else if len(targetVideoID) >= 20 {
		isDrive = true
	} else if m := watchSearchPattern.FindString(targetVideoID); m != "" {
		targetVideoID = m
		isDrive = false
	}

	isMobile := isMobileRequest(r)
	isShorts := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type"))) == "shorts"
	watchTemplate := "watch.html"
	if isShorts {
		watchTemplate = "watch_shorts.html"
	} else if isMobile {
		watchTemplate = "watch_mobile.html"
	}

	memberName := strings.TrimSpace(r.URL.Query().Get("member"))
	if strings.Contains(memberName, "comments") || strings.Contains(r.URL.Path+"?"+r.URL.RawQuery, "/comments") {
		http.Redirect(w, r, "/comments", http.StatusFound)
		return
	}

	videoTitle := ""
	for _, v := range a.VideoPool.Get() {
		if v.VideoID == targetVideoID {
			videoTitle = v.Title
			break
		}
	}
	pageTitle := "루미너스"
	videoDescription := "루미너스에서 스텔라이브 팬 영상을 시청해보세요."
	if videoTitle != "" {
		pageTitle = videoTitle + " - 루미너스"
		videoDescription = videoTitle + " - 루미너스에서 이 영상을 시청해보세요."
	}

	ctx := map[string]any{
		"video_id":          targetVideoID,
		"is_drive":          isDrive,
		"request":           requestContext(r),
		"page_title":        pageTitle,
		"video_description": videoDescription,
	}

	if memberName == "" {
		if err := a.Templates.Render(w, r, watchTemplate, ctx, a.GenRepImageOverrides); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	var cnt int64
	err := a.DB.QueryRow("SELECT COUNT(*) FROM members WHERE member_name = ?", memberName).Scan(&cnt)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if cnt == 0 {
		http.NotFound(w, r)
		return
	}
	if err := a.Templates.Render(w, r, watchTemplate, ctx, a.GenRepImageOverrides); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (a *App) WatchVideoCommentsRedirectHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/comments", http.StatusFound)
}

func (a *App) ApiGetHomeShortsHandler(w http.ResponseWriter, r *http.Request) {
	shorts := a.getHomeShortsVideos(r.Context(), sessionUserID(r))
	if len(shorts) > 15 {
		shorts = shorts[:15]
	}
	writeJSON(w, map[string]any{"videos": shorts})
}

func (a *App) SentryVerifyHandler(w http.ResponseWriter, r *http.Request) {
	panic("__sentry_verify__: 의도된 테스트용 패닉")
}
