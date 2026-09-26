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

// LatestPageQuotaFields: "최신순"(기본) 페이지네이션 응답에 붙이는 할당량 안내용
// 추가 필드. "오래된순"용 OldestSortExtraFields와 달리 이쪽은 상태가 truncated로
// 구분되지 않고(페이지당 그냥 다음 페이지가 없을 뿐) 할당량 소진 여부만 알면
// 되므로 훨씬 단순하다. 프론트엔드는 quota_limited가 true일 때 "일부 영상만
// 보이고 있다"는 안내를 띄운다.
func LatestPageQuotaFields() map[string]any {
	if !QuotaIsExceeded() {
		return map[string]any{}
	}
	label, _ := QuotaResetETA()
	return map[string]any{
		"quota_limited":         true,
		"quota_reset_kst_label": label,
	}
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
