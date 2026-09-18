package handlers

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pastellive/internal/models"
	"pastellive/internal/video"
)

func runEvery(interval time.Duration, immediate bool, fn func()) {
	go func() {
		if immediate {
			safeRun(fn)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			safeRun(fn)
		}
	}()
}

func runDailyAt(hour, minute int, fn func()) {
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			time.Sleep(next.Sub(now))
			safeRun(fn)
		}
	}()
}

func safeRun(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("스케줄러 작업 중 패닉 발생(치명적이지 않음, 다음 주기에 재시도): %v", r)
		}
	}()
	fn()
}

func (a *App) StartBackgroundScheduler() {
	if !a.Cfg.SchedulerEnabled {
		log.Printf("SCHEDULER_ENABLED=false: 이 프로세스는 무거운 스케줄러 없이 요청 처리만 담당합니다.")
		return
	}

	runEvery(30*time.Minute, false, func() {
		a.VideoPool.Refresh(context.Background(), a.DB, a.Cfg.YoutubeAPIKey)
	})
	runEvery(20*time.Second, true, a.refreshAndBroadcastLiveStatus)
	runEvery(4*24*time.Hour, false, func() {
		video.ResubscribeAllChannels(context.Background(), a.DB, a.Cfg.WebSubCallbackBase, a.Cfg.WebSubSecret)
	})
	runEvery(3*time.Hour, true, func() {
		models.SyncAllMembersVideoArchive(context.Background(), a.DB, a.Cfg.YoutubeAPIKey)
	})
	runEvery(6*time.Hour, true, func() {
		video.BackfillAllShortsCache(context.Background(), a.DB, a.Cfg.YoutubeAPIKey, 8)
	})
	runEvery(2*time.Minute, true, a.prewarmTrendingSearchCache)

	if !kirinukiDisabled {
		runEvery(4*time.Minute, true, func() {
			channels, err := models.GetKirinukiChannelIDs(a.DB)
			if err == nil && len(channels) > 0 {
				combined := fetchKirinukiVideos(context.Background(), channels, a.Cfg.YoutubeAPIKey)
				if len(combined) > 0 {
					a.Cache.Set(kirinukiCacheKey, combined, kirinukiCacheTTL)
				}
			}
		})
	}
	if a.Meili.Available() {
		runEvery(15*time.Minute, true, a.SyncMeilisearchIndex)
	}
	runEvery(24*time.Hour, true, func() {
		if n, err := models.CleanupStaleSearchTrends(a.DB); err == nil && n > 0 {
			log.Printf("search_trends 7일 지난 검색어 정리됨: %d개", n)
		}
	})
	runDailyAt(23, 10, a.runWebpBackfillJob)
	runDailyAt(23, 35, a.cleanupOrphanFanartImages)

	log.Printf("백그라운드 스케줄러 시작됨: 영상 풀 자동 갱신(30분 간격) 등 %d개 주기 작업 등록", 11)
}

func (a *App) refreshAndBroadcastLiveStatus() {
	status := computeLiveStatus(liveStatusHTTPClient)
	a.Cache.Set(liveStatusCacheKey, status, liveStatusCacheTTL)
}

func (a *App) prewarmTrendingSearchCache() {
	keywords, err := models.GetTrendingKeywords(a.DB, 15)
	if err != nil {
		log.Printf("인기 검색어 캐시 예열: 목록 조회 실패: %v", err)
		return
	}
	warmed := 0
	for _, kw := range keywords {
		func() {
			defer func() { _ = recover() }()
			a.executeSearchCore(context.Background(), kw)
			warmed++
		}()
	}
	if warmed > 0 {
		log.Printf("인기 검색어 %d개 캐시 예열 완료 (검색량 집계에는 영향 없음)", warmed)
	}
}

func (a *App) cleanupOrphanFanartImages() {
	imagesDir := filepath.Join(a.Cfg.StaticDir, "images")
	info, err := os.Stat(imagesDir)
	if err != nil || !info.IsDir() {
		return
	}

	rows, err := a.DB.Query("SELECT image_url FROM fanart_gallery")
	if err != nil {
		return
	}
	expected := make(map[string]bool)
	for rows.Next() {
		var imageURL string
		if err := rows.Scan(&imageURL); err != nil {
			continue
		}
		const prefix = "/static/images/"
		if !strings.HasPrefix(imageURL, prefix) {
			continue
		}
		filename := imageURL[strings.LastIndex(imageURL, "/")+1:]
		expected[filename] = true
		name := filename
		ext := ""
		if dot := strings.LastIndex(filename, "."); dot >= 0 {
			name = filename[:dot]
			ext = filename[dot+1:]
		}
		if !strings.EqualFold(ext, "gif") && !strings.HasSuffix(name, "_thumb") {
			expected[name+"_thumb.webp"] = true
		}
	}
	rows.Close()

	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		return
	}
	deleted := 0
	var freedBytes int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if expected[e.Name()] {
			continue
		}
		fp := filepath.Join(imagesDir, e.Name())
		if fi, err := os.Stat(fp); err == nil {
			freedBytes += fi.Size()
		}
		if err := os.Remove(fp); err == nil {
			deleted++
		}
	}
	if deleted > 0 {
		log.Printf("팬아트 고아 파일 자동 정리: %d개 삭제, %.2fMB 확보", deleted, float64(freedBytes)/1024/1024)
	}
}
