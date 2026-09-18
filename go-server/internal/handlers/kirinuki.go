package handlers

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"pastellive/internal/models"
	"pastellive/internal/video"
)

const kirinukiDisabled = true
const kirinukiCacheKey = "kirinuki_videos_v1"
const kirinukiCacheTTL = 300 * time.Second

func (a *App) ApiKirinukiVideosHandler(w http.ResponseWriter, r *http.Request) {
	if kirinukiDisabled {
		writeJSON(w, map[string]any{"success": true, "videos": []video.Video{}})
		return
	}

	channels, err := models.GetKirinukiChannelIDs(a.DB)
	if err != nil {
		log.Printf("키리누키 채널 목록 조회 실패: %v", err)
		channels = nil
	}
	if len(channels) == 0 {
		writeJSON(w, map[string]any{"success": true, "videos": []video.Video{}})
		return
	}

	if cached, ok := a.Cache.Get(kirinukiCacheKey); ok {
		writeJSON(w, map[string]any{"success": true, "videos": cached})
		return
	}

	combined := fetchKirinukiVideos(r.Context(), channels, a.Cfg.YoutubeAPIKey)
	a.Cache.Set(kirinukiCacheKey, combined, kirinukiCacheTTL)
	writeJSON(w, map[string]any{"success": true, "videos": combined})
}

func fetchKirinukiVideos(ctx context.Context, channels []string, apiKey string) []video.Video {
	maxWorkers := len(channels)
	if maxWorkers > 8 {
		maxWorkers = 8
	}
	sem := make(chan struct{}, maxWorkers)
	results := make([][]video.Video, len(channels))
	var wg sync.WaitGroup
	for i, ch := range channels {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, channelID string) {
			defer wg.Done()
			defer func() { <-sem }()
			fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			results[idx] = video.FetchPlaylistOrChannel(fetchCtx, channelID, false, false, "", apiKey, 1)
		}(i, ch)
	}
	wg.Wait()

	combined := make([]video.Video, 0)
	seen := make(map[string]bool)
	for _, vids := range results {
		limit := 30
		if len(vids) < limit {
			limit = len(vids)
		}
		for _, v := range vids[:limit] {
			if v.VideoID == "" || seen[v.VideoID] {
				continue
			}
			seen[v.VideoID] = true
			combined = append(combined, v)
		}
	}
	rand.Shuffle(len(combined), func(i, j int) { combined[i], combined[j] = combined[j], combined[i] })
	return combined
}
