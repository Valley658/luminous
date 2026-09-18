package video

import (
	"net/http"
	"strings"
	"sync"
	"time"

	pdb "pastellive/internal/db"
)

func InitVideoShortsCacheTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS video_shorts_cache (
		video_id VARCHAR(20) PRIMARY KEY,
		is_short TINYINT(1) NOT NULL,
		checked_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	return err
}

var shortsCheckClient = &http.Client{
	Timeout: 4 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func fetchIsShortFromYoutube(videoID string) *bool {
	url := "https://www.youtube.com/shorts/" + videoID
	falseVal, trueVal := false, true

	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0")
		if resp, err := shortsCheckClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				return &falseVal
			}
			if resp.StatusCode == 200 {
				return &trueVal
			}
		}
	}

	req, err = http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := shortsCheckClient.Do(req)
	if err != nil {
		return nil
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return &falseVal
	}
	if resp.StatusCode == 200 {
		return &trueVal
	}
	return nil
}

func checkAndCacheShorts(d *pdb.DB, videoIDs []string) {
	type result struct {
		id string
		v  *bool
	}
	results := make(chan result, len(videoIDs))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for _, vid := range videoIDs {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			results <- result{id: id, v: fetchIsShortFromYoutube(id)}
		}(vid)
	}
	wg.Wait()
	close(results)

	fresh := make(map[string]bool)
	for r := range results {
		if r.v != nil {
			fresh[r.id] = *r.v
		}
	}
	if len(fresh) == 0 {
		return
	}
	upsertSQL := "INSERT INTO video_shorts_cache (video_id, is_short) VALUES (?, ?) ON CONFLICT(video_id) DO UPDATE SET is_short = excluded.is_short"
	if d.Backend == "mysql" {
		upsertSQL = "INSERT INTO video_shorts_cache (video_id, is_short) VALUES (?, ?) ON DUPLICATE KEY UPDATE is_short = VALUES(is_short)"
	}
	for vid, isShort := range fresh {
		v := 0
		if isShort {
			v = 1
		}
		_, _ = d.Exec(upsertSQL, vid, v)
	}
}

func ResolveShortsFlags(d *pdb.DB, videos []Video) {
	if len(videos) == 0 {
		return
	}
	ids := make([]string, 0, len(videos))
	for _, v := range videos {
		if v.VideoID != "" {
			ids = append(ids, v.VideoID)
		}
	}
	if len(ids) == 0 {
		return
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	cached := make(map[string]bool)
	if rows, err := d.Query("SELECT video_id, is_short FROM video_shorts_cache WHERE video_id IN ("+placeholders+")", args...); err == nil {
		for rows.Next() {
			var vid string
			var isShort int
			if rows.Scan(&vid, &isShort) == nil {
				cached[vid] = isShort != 0
			}
		}
		rows.Close()
	}

	var uncached []string
	seenUncached := make(map[string]bool)
	for _, id := range ids {
		if _, ok := cached[id]; !ok && !seenUncached[id] {
			seenUncached[id] = true
			uncached = append(uncached, id)
		}
	}
	if len(uncached) > 0 {
		go checkAndCacheShorts(d, uncached)
	}

	for i := range videos {
		if v, ok := cached[videos[i].VideoID]; ok {
			videos[i].IsShort = v
		}
	}
}

func SeedShortsCache(d *pdb.DB, videoIDs []string) {
	ids := make([]string, 0, len(videoIDs))
	for _, id := range videoIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	upsertSQL := "INSERT INTO video_shorts_cache (video_id, is_short) VALUES (?, 1) ON CONFLICT(video_id) DO UPDATE SET is_short = 1"
	if d.Backend == "mysql" {
		upsertSQL = "INSERT INTO video_shorts_cache (video_id, is_short) VALUES (?, 1) ON DUPLICATE KEY UPDATE is_short = 1"
	}
	for _, id := range ids {
		_, _ = d.Exec(upsertSQL, id)
	}
}
