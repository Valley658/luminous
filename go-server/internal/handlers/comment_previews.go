package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/video"
)

const commentPreviewBatchMax = 40
const commentPreviewCacheTTL = time.Hour
const commentPreviewYoutubeFallbackMaxPerRequest = 3

var commentPreviewYTInflight = struct {
	mu  sync.Mutex
	set map[string]bool
}{set: make(map[string]bool)}

func commentPreviewCacheKey(videoID string) string { return "comment_preview_" + videoID }

func dedupCapVideoIDs(videoIDs []string) []string {
	dedup := make([]string, 0, len(videoIDs))
	seen := make(map[string]bool)
	for _, id := range videoIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		dedup = append(dedup, id)
		if len(dedup) >= commentPreviewBatchMax {
			break
		}
	}
	return dedup
}

func (a *App) siteBestCommentPreviews(videoIDs []string) map[string]*video.CommentPreview {
	result := make(map[string]*video.CommentPreview)
	if len(videoIDs) == 0 {
		return result
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(videoIDs)), ",")
	args := make([]any, len(videoIDs))
	for i, id := range videoIDs {
		args[i] = id
	}

	type row struct {
		id       int64
		videoID  string
		nickname string
		content  string
	}
	rowsByVideo := make(map[string][]row)
	var commentIDs []int64

	rows, err := a.DB.Query("SELECT id, video_id, nickname, content FROM video_comments WHERE video_id IN ("+placeholders+") ORDER BY id DESC", args...)
	if err == nil {
		for rows.Next() {
			var r row
			if rows.Scan(&r.id, &r.videoID, &r.nickname, &r.content) == nil {
				rowsByVideo[r.videoID] = append(rowsByVideo[r.videoID], r)
				commentIDs = append(commentIDs, r.id)
			}
		}
		rows.Close()
	}

	likeCounts := make(map[int64]int64)
	if len(commentIDs) > 0 {
		cidPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(commentIDs)), ",")
		cidArgs := make([]any, len(commentIDs))
		for i, id := range commentIDs {
			cidArgs[i] = id
		}
		lrows, lerr := a.DB.Query("SELECT comment_id, COUNT(*) as cnt FROM comment_likes WHERE comment_id IN ("+cidPlaceholders+") AND reaction = 'like' GROUP BY comment_id", cidArgs...)
		if lerr == nil {
			for lrows.Next() {
				var cid, cnt int64
				if lrows.Scan(&cid, &cnt) == nil {
					likeCounts[cid] = cnt
				}
			}
			lrows.Close()
		}
	}

	for vid, rs := range rowsByVideo {
		best := rs[0]
		bestLikes := likeCounts[best.id]
		for _, r := range rs[1:] {
			if likeCounts[r.id] > bestLikes {
				best = r
				bestLikes = likeCounts[r.id]
			}
		}
		nickname := best.nickname
		if nickname == "" {
			nickname = "스텔리언"
		}
		result[vid] = &video.CommentPreview{Nickname: nickname, Content: best.content, Source: "site"}
	}
	return result
}

func (a *App) batchCommentPreviewsSiteOnly(videoIDs []string) map[string]*video.CommentPreview {
	return a.siteBestCommentPreviews(dedupCapVideoIDs(videoIDs))
}

func (a *App) fetchCommentPreviewYoutubeBG(videoID string) {
	defer func() {
		commentPreviewYTInflight.mu.Lock()
		delete(commentPreviewYTInflight.set, videoID)
		commentPreviewYTInflight.mu.Unlock()
	}()
	yt := video.FetchYouTubeComments(context.Background(), videoID, "", a.Cfg.YoutubeAPIKey, 5)
	var preview *video.CommentPreview
	if yt.Success && len(yt.Comments) > 0 {
		top := yt.Comments[0]
		nickname := top.Nickname
		if nickname == "" {
			nickname = "유튜브 이용자"
		}
		preview = &video.CommentPreview{Nickname: nickname, Content: top.Content, Source: "youtube"}
	}
	a.Cache.Set(commentPreviewCacheKey(videoID), preview, commentPreviewCacheTTL)
}

func (a *App) batchCommentPreviews(videoIDs []string, includeYoutubeFallback bool) map[string]*video.CommentPreview {
	dedup := dedupCapVideoIDs(videoIDs)
	comments := make(map[string]*video.CommentPreview)
	if len(dedup) == 0 {
		return comments
	}

	var missing []string
	for _, vid := range dedup {
		if cached, ok := a.Cache.Get(commentPreviewCacheKey(vid)); ok {
			if cp, ok2 := cached.(*video.CommentPreview); ok2 {
				comments[vid] = cp
			} else {
				comments[vid] = nil
			}
			continue
		}
		missing = append(missing, vid)
	}
	if len(missing) == 0 {
		return comments
	}

	siteBest := a.siteBestCommentPreviews(missing)
	youtubeFallbackSpawned := 0
	for _, vid := range missing {
		if cp, ok := siteBest[vid]; ok {
			comments[vid] = cp
			a.Cache.Set(commentPreviewCacheKey(vid), cp, commentPreviewCacheTTL)
			continue
		}
		comments[vid] = nil
		if !includeYoutubeFallback || youtubeFallbackSpawned >= commentPreviewYoutubeFallbackMaxPerRequest {
			continue
		}
		commentPreviewYTInflight.mu.Lock()
		alreadyRunning := commentPreviewYTInflight.set[vid]
		if !alreadyRunning {
			commentPreviewYTInflight.set[vid] = true
		}
		commentPreviewYTInflight.mu.Unlock()
		if !alreadyRunning {
			youtubeFallbackSpawned++
			go a.fetchCommentPreviewYoutubeBG(vid)
		}
	}
	return comments
}

func (a *App) ApiCommentPreviewHandler(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	if cached, ok := a.Cache.Get(commentPreviewCacheKey(videoID)); ok {
		if cp, ok2 := cached.(*video.CommentPreview); ok2 {
			writeJSON(w, map[string]any{"success": true, "comment": cp})
		} else {
			writeJSON(w, map[string]any{"success": true, "comment": nil})
		}
		return
	}

	site := a.siteBestCommentPreviews([]string{videoID})
	var preview *video.CommentPreview
	if cp, ok := site[videoID]; ok {
		preview = cp
	} else {
		yt := video.FetchYouTubeComments(r.Context(), videoID, "", a.Cfg.YoutubeAPIKey, 5)
		if yt.Success && len(yt.Comments) > 0 {
			top := yt.Comments[0]
			nickname := top.Nickname
			if nickname == "" {
				nickname = "유튜브 이용자"
			}
			preview = &video.CommentPreview{Nickname: nickname, Content: top.Content, Source: "youtube"}
		}
	}
	a.Cache.Set(commentPreviewCacheKey(videoID), preview, commentPreviewCacheTTL)
	writeJSON(w, map[string]any{"success": true, "comment": preview})
}

func (a *App) ApiCommentPreviewsBatchHandler(w http.ResponseWriter, r *http.Request) {
	rawIDs := r.URL.Query().Get("ids")
	var videoIDs []string
	for _, v := range strings.Split(rawIDs, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			videoIDs = append(videoIDs, v)
		}
	}
	comments := a.batchCommentPreviews(videoIDs, true)
	httputil.JSON(w, http.StatusOK, map[string]any{"success": true, "comments": comments})
}
