package video

import (
	"context"
	"sort"
	"time"

	pdb "pastellive/internal/db"
)

func SortVideosByType(videos []Video, sortType string) []Video {
	out := make([]Video, len(videos))
	copy(out, videos)
	switch sortType {
	case "views":
		sort.SliceStable(out, func(i, j int) bool { return out[i].ViewCount > out[j].ViewCount })
	case "oldest":
		sort.SliceStable(out, func(i, j int) bool { return out[i].PublishedAt < out[j].PublishedAt })
	default:
		sort.SliceStable(out, func(i, j int) bool { return out[i].PublishedAt > out[j].PublishedAt })
	}
	return out
}

const oldestSortMaxPages = 40
const oldestSortMaxSeconds = 20

func CollectAllPagesForOldest(ctx context.Context, d *pdb.DB, targetID string, isPlaylist bool, apiKey string, filterFn func(Video) bool) (videos []Video, truncated, quotaLimited bool) {
	var all []Video
	seen := make(map[string]bool)
	pageToken := ""
	pages := 0
	start := time.Now()

	for pages < oldestSortMaxPages && time.Since(start) < oldestSortMaxSeconds*time.Second {
		page, next := FetchLatestVideosPage(ctx, targetID, isPlaylist, pageToken, apiKey)
		if len(page) == 0 {
			break
		}
		if filterFn != nil {
			ResolveShortsFlags(d, page)
			filtered := make([]Video, 0, len(page))
			for _, v := range page {
				if filterFn(v) {
					filtered = append(filtered, v)
				}
			}
			page = filtered
		}
		for _, v := range page {
			if v.VideoID != "" && !seen[v.VideoID] {
				seen[v.VideoID] = true
				all = append(all, v)
			}
		}
		pageToken = next
		pages++
		if pageToken == "" {
			break
		}
	}
	truncated = pageToken != ""
	quotaLimited = QuotaIsExceeded()
	return SortVideosByType(all, "oldest"), truncated, quotaLimited
}

func OldestSortExtraFields(truncated, quotaLimited bool) map[string]any {
	extra := map[string]any{}
	if quotaLimited {
		label, _ := QuotaResetETA()
		extra["oldest_limited"] = true
		extra["oldest_limited_reason"] = "quota"
		extra["quota_reset_kst_label"] = label
	} else if truncated {
		extra["oldest_limited"] = true
		extra["oldest_limited_reason"] = "too_many"
	}
	return extra
}
