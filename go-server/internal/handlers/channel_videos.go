package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/data"
	"pastellive/internal/drive"
	"pastellive/internal/httputil"
	"pastellive/internal/models"
	"pastellive/internal/video"
)

const (
	kannaDriveVideoFolderID  = "1bnZXIH--kZxUSONIU3FlYNsAv2hmMDIV"
	kannaDriveShortsFolderID = "1eN6H2F0PTyxemZS2yF4GYcESDBP_n_Jm"
)

var kannaReplayPlaylists = []string{
	"PLO1bVFkgUJ3dRBH3aBATeXC_cCjIZqMRX",
	"PLO1bVFkgUJ3c6xz-mgmmD-nbE_baKCvZi",
	"PLO1bVFkgUJ3fa1di4P9Faq0gqZH-R4GWG",
}

func isShortsTitleVideo(v video.Video) bool {
	title := strings.ToLower(v.Title)
	return v.IsShort || strings.Contains(title, "#shorts") || strings.Contains(title, "shorts") ||
		strings.Contains(v.Title, "쇼츠") || strings.Contains(title, "#short") || strings.Contains(v.Title, "#쇼츠") ||
		(strings.Contains(v.Title, "#") && len([]rune(v.Title)) < 55)
}

func isCleanLongVideo(v video.Video) bool {
	title := strings.ToLower(v.Title)
	isShort := v.IsShort || strings.Contains(title, "#shorts") || strings.Contains(title, "shorts") ||
		strings.Contains(v.Title, "쇼츠") || strings.Contains(title, "#short") || strings.Contains(v.Title, "#쇼츠")
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

func (a *App) collectPlaylistsFully(ctx context.Context, playlistIDs []string) []video.Video {
	var combined []video.Video
	seen := make(map[string]bool)
	for _, plID := range playlistIDs {
		if plID == "" {
			continue
		}
		pageToken := ""
		for pages := 0; pages < 40; pages++ {
			vids, next := video.FetchLatestVideosPage(ctx, plID, true, pageToken, a.Cfg.YoutubeAPIKey)
			if len(vids) == 0 {
				break
			}
			for _, v := range vids {
				if v.VideoID != "" && !seen[v.VideoID] {
					seen[v.VideoID] = true
					combined = append(combined, v)
				}
			}
			pageToken = next
			if pageToken == "" {
				break
			}
		}
	}
	return combined
}

func (a *App) getVideosForMember(ctx context.Context, memberName, reqType, sortType, pageToken string) map[string]any {
	mp, found, err := models.GetMemberPlaylists(a.DB, memberName)
	if err != nil {
		return map[string]any{"error": "Database error", "_status": http.StatusInternalServerError}
	}

	var channelID string
	var musicPlaylists, shortsPlaylists, replayPlaylists []string
	if found {
		channelID = mp.ChannelID.String
		musicPlaylists, shortsPlaylists, replayPlaylists = mp.MusicPlaylists, mp.ShortsPlaylists, mp.ReplayPlaylists
	}
	hasMusic := len(musicPlaylists) > 0
	hasReplay := len(replayPlaylists) > 0

	if memberName == "칸나" {
		hasReplay = true
		switch reqType {
		case "video":
			vids, next := drive.FetchVideos(ctx, a.Cache, a.Cfg.GoogleDriveAPIKey, kannaDriveVideoFolderID, pageToken)
			return map[string]any{"videos": vids, "nextPageToken": next, "has_music": hasMusic, "has_replay": hasReplay}
		case "shorts":
			vids, next := drive.FetchVideos(ctx, a.Cache, a.Cfg.GoogleDriveAPIKey, kannaDriveShortsFolderID, pageToken)
			return map[string]any{"videos": vids, "nextPageToken": next, "has_music": hasMusic, "has_replay": hasReplay}
		case "replay":
			combined := a.collectPlaylistsFully(ctx, kannaReplayPlaylists)
			combined = video.SortVideosByType(combined, sortType)
			return map[string]any{"videos": combined, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}
		}
	}

	if !found && memberName != "칸나" {
		return map[string]any{"error": "Member not found", "_status": http.StatusNotFound}
	}

	switch reqType {
	case "shorts":
		validShortsPlaylists := shortsPlaylists
		switch {
		case len(validShortsPlaylists) == 1:
			if sortType == "oldest" {
				all, truncated, quotaLimited := video.CollectAllPagesForOldest(ctx, a.DB, validShortsPlaylists[0], true, a.Cfg.YoutubeAPIKey, nil)
				ids := make([]string, len(all))
				for i, v := range all {
					ids[i] = v.VideoID
				}
				video.SeedShortsCache(a.DB, ids)
				out := map[string]any{"videos": all, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}
				for k, v := range video.OldestSortExtraFields(truncated, quotaLimited) {
					out[k] = v
				}
				return out
			}
			vids, next := video.FetchLatestVideosPage(ctx, validShortsPlaylists[0], true, pageToken, a.Cfg.YoutubeAPIKey)
			if len(vids) > 0 {
				ids := make([]string, len(vids))
				for i, v := range vids {
					ids[i] = v.VideoID
				}
				video.SeedShortsCache(a.DB, ids)
				return map[string]any{"videos": video.SortVideosByType(vids, sortType), "nextPageToken": nullIfEmpty(next), "has_music": hasMusic, "has_replay": hasReplay}
			}
			return map[string]any{"videos": []video.Video{}, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}

		case len(validShortsPlaylists) > 1:
			combined := a.collectPlaylistsFully(ctx, validShortsPlaylists)
			ids := make([]string, len(combined))
			for i, v := range combined {
				ids[i] = v.VideoID
			}
			video.SeedShortsCache(a.DB, ids)
			combined = video.SortVideosByType(combined, sortType)
			return map[string]any{"videos": combined, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}

		default:
			var autoShortsPlaylistID string
			if strings.HasPrefix(channelID, "UC") {
				autoShortsPlaylistID = "UUSH" + channelID[2:]
			}
			if autoShortsPlaylistID != "" {
				if sortType == "oldest" {
					all, truncated, quotaLimited := video.CollectAllPagesForOldest(ctx, a.DB, autoShortsPlaylistID, true, a.Cfg.YoutubeAPIKey, nil)
					ids := make([]string, len(all))
					for i, v := range all {
						ids[i] = v.VideoID
					}
					video.SeedShortsCache(a.DB, ids)
					out := map[string]any{"videos": all, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}
					for k, v := range video.OldestSortExtraFields(truncated, quotaLimited) {
						out[k] = v
					}
					return out
				}
				vids, next := video.FetchLatestVideosPage(ctx, autoShortsPlaylistID, true, pageToken, a.Cfg.YoutubeAPIKey)
				if len(vids) > 0 {
					ids := make([]string, len(vids))
					for i, v := range vids {
						ids[i] = v.VideoID
					}
					video.SeedShortsCache(a.DB, ids)
					return map[string]any{"videos": video.SortVideosByType(vids, sortType), "nextPageToken": nullIfEmpty(next), "has_music": hasMusic, "has_replay": hasReplay}
				}
			}

			if sortType == "oldest" {
				filtered, truncated, quotaLimited := video.CollectAllPagesForOldest(ctx, a.DB, channelID, false, a.Cfg.YoutubeAPIKey, isShortsTitleVideo)
				ids := make([]string, len(filtered))
				for i, v := range filtered {
					ids[i] = v.VideoID
				}
				video.SeedShortsCache(a.DB, ids)
				out := map[string]any{"videos": filtered, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}
				for k, v := range video.OldestSortExtraFields(truncated, quotaLimited) {
					out[k] = v
				}
				return out
			}
			vids, next := video.FetchLatestVideosPage(ctx, channelID, false, pageToken, a.Cfg.YoutubeAPIKey)
			if len(vids) > 0 {
				video.ResolveShortsFlags(a.DB, vids)
				filtered := make([]video.Video, 0, len(vids))
				for _, v := range vids {
					if isShortsTitleVideo(v) {
						filtered = append(filtered, v)
					}
				}
				filtered = video.SortVideosByType(filtered, sortType)
				return map[string]any{"videos": filtered, "nextPageToken": nullIfEmpty(next), "has_music": hasMusic, "has_replay": hasReplay}
			}
			return map[string]any{"videos": []video.Video{}, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}
		}

	case "music":
		if !hasMusic {
			return map[string]any{"videos": []video.Video{}, "nextPageToken": nil, "has_music": false, "has_replay": hasReplay}
		}
		combined := a.collectPlaylistsFully(ctx, musicPlaylists)
		combined = video.SortVideosByType(combined, sortType)
		return map[string]any{"videos": combined, "nextPageToken": nil, "has_music": true, "has_replay": hasReplay}

	case "replay":
		if !hasReplay {
			return map[string]any{"videos": []video.Video{}, "nextPageToken": nil, "has_music": hasMusic, "has_replay": false}
		}
		combined := a.collectPlaylistsFully(ctx, replayPlaylists)
		combined = video.SortVideosByType(combined, sortType)
		return map[string]any{"videos": combined, "nextPageToken": nil, "has_music": hasMusic, "has_replay": true}

	default:
		if sortType == "oldest" {
			clean, truncated, quotaLimited := video.CollectAllPagesForOldest(ctx, a.DB, channelID, false, a.Cfg.YoutubeAPIKey, isCleanLongVideo)
			out := map[string]any{"videos": clean, "nextPageToken": nil, "has_music": hasMusic, "has_replay": hasReplay}
			for k, v := range video.OldestSortExtraFields(truncated, quotaLimited) {
				out[k] = v
			}
			return out
		}
		vids, next := video.FetchLatestVideosPage(ctx, channelID, false, pageToken, a.Cfg.YoutubeAPIKey)
		if len(vids) > 0 {
			video.ResolveShortsFlags(a.DB, vids)
			filtered := make([]video.Video, 0, len(vids))
			for _, v := range vids {
				if isCleanLongVideo(v) {
					filtered = append(filtered, v)
				}
			}
			vids = video.SortVideosByType(filtered, sortType)
		}
		return map[string]any{"videos": vids, "nextPageToken": nullIfEmpty(next), "has_music": hasMusic, "has_replay": hasReplay}
	}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *App) getGenerationVideos(ctx context.Context, genKey, sortType string) map[string]any {
	var gen *data.Generation
	for i := range data.MEMBER_GENERATIONS {
		if data.MEMBER_GENERATIONS[i].Key == genKey {
			gen = &data.MEMBER_GENERATIONS[i]
			break
		}
	}
	if gen == nil {
		return map[string]any{"error": "Generation not found", "_status": http.StatusNotFound}
	}

	var combined []video.Video
	seen := make(map[string]bool)
	anyReplay := false
	anyLimited := false
	limitedReason := ""
	quotaEtaLabel := ""

	for _, memberName := range gen.Members {
		memberResult := a.getVideosForMember(ctx, memberName, "video", sortType, "")
		if memberResult["_status"] != nil {
			continue
		}
		if hr, _ := memberResult["has_replay"].(bool); hr {
			anyReplay = true
		}
		if ol, _ := memberResult["oldest_limited"].(bool); ol {
			anyLimited = true
			if reason, _ := memberResult["oldest_limited_reason"].(string); reason == "quota" {
				limitedReason = "quota"
				if label, ok := memberResult["quota_reset_kst_label"].(string); ok {
					quotaEtaLabel = label
				}
			} else if limitedReason != "quota" {
				limitedReason = "too_many"
			}
		}
		vids, _ := memberResult["videos"].([]video.Video)
		for _, v := range vids {
			if v.VideoID != "" && !seen[v.VideoID] {
				seen[v.VideoID] = true
				v.MemberName = memberName
				combined = append(combined, v)
			}
		}
	}
	combined = video.SortVideosByType(combined, sortType)
	result := map[string]any{"videos": combined, "nextPageToken": nil, "has_music": false, "has_replay": anyReplay}
	if anyLimited {
		result["oldest_limited"] = true
		result["oldest_limited_reason"] = limitedReason
		if quotaEtaLabel != "" {
			result["quota_reset_kst_label"] = quotaEtaLabel
		}
	}
	return result
}

func (a *App) ApiGetVideosHandler(w http.ResponseWriter, r *http.Request) {
	memberName := strings.TrimSpace(chi.URLParam(r, "memberName"))
	reqType := r.URL.Query().Get("type")
	if reqType == "" {
		reqType = "video"
	}
	sortType := r.URL.Query().Get("sort")
	if sortType == "" {
		sortType = "latest"
	}
	pageToken := r.URL.Query().Get("pageToken")

	var result map[string]any
	if data.GenerationKeys[memberName] {
		result = a.getGenerationVideos(r.Context(), memberName, sortType)
	} else {
		result = a.getVideosForMember(r.Context(), memberName, reqType, sortType, pageToken)
	}

	if status, ok := result["_status"].(int); ok {
		delete(result, "_status")
		httputil.JSON(w, status, result)
		return
	}

	if vids, ok := result["videos"]; ok {
		empty := false
		switch v := vids.(type) {
		case []video.Video:
			empty = len(v) == 0
			if v == nil {
				result["videos"] = []video.Video{}
			}
		case []drive.Video:
			empty = len(v) == 0
			if v == nil {
				result["videos"] = []drive.Video{}
			}
		case nil:
			empty = true
			result["videos"] = []video.Video{}
		}
		if empty && result["error"] == nil && video.QuotaIsExceeded() {
			label, _ := video.QuotaResetETA()
			result["quota_exceeded"] = true
			result["quota_reset_kst_label"] = label
		}
	}
	writeJSON(w, result)
}
