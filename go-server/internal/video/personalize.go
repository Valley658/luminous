package video

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"pastellive/internal/cache"
	pdb "pastellive/internal/db"
)

func GetPersonalizedVideoOrder(db *pdb.DB, c *cache.Store, pool []Video, userID int64, discoveryRatio float64) []Video {
	poolCopy := make([]Video, len(pool))
	copy(poolCopy, pool)

	if userID == 0 {
		rand.Shuffle(len(poolCopy), func(i, j int) { poolCopy[i], poolCopy[j] = poolCopy[j], poolCopy[i] })
		return DiversifyByMember(poolCopy)
	}

	watchedVideoIDs := fetchWatchedVideoIDs(db, userID, 200)
	watchedSet := make(map[string]bool, len(watchedVideoIDs))
	for _, id := range watchedVideoIDs {
		watchedSet[id] = true
	}

	memberByVideoID := make(map[string]string, len(poolCopy))
	for _, v := range poolCopy {
		if v.VideoID != "" {
			memberByVideoID[v.VideoID] = v.MemberName
		}
	}

	memberScore := make(map[string]float64)
	for rank, vid := range watchedVideoIDs {
		m := memberByVideoID[vid]
		if m == "" {
			continue
		}
		memberScore[m] += 1.0 / float64(rank+1)
	}

	bookmarkedVideoIDs := fetchBookmarkedVideoIDs(db, userID, 100)
	for _, vid := range bookmarkedVideoIDs {
		if m := memberByVideoID[vid]; m != "" {
			memberScore[m] += 1.5
		}
	}

	maxScore := 1.0
	for _, s := range memberScore {
		if s > maxScore {
			maxScore = s
		}
	}

	collaborativeBoost := fetchCollaborativeVideoBoost(db, c, userID, watchedVideoIDs, watchedSet)
	trendingBoost := fetchTrendingVideoBoost(db, c)

	affinityWeight := func(v Video) float64 {
		weight := 1.0 + 8.0*(memberScore[v.MemberName]/maxScore)
		if b, ok := collaborativeBoost[v.VideoID]; ok {
			if b > 10 {
				b = 10
			}
			weight += b * 0.4
		}
		if b, ok := trendingBoost[v.VideoID]; ok {
			if b > 20 {
				b = 20
			}
			weight += b * 0.05
		}
		if watchedSet[v.VideoID] {
			weight *= 0.25
		}
		return weight
	}

	type scoredVideo struct {
		v   Video
		key float64
	}
	scored := make([]scoredVideo, len(poolCopy))
	for i, v := range poolCopy {
		w := affinityWeight(v)
		if w < 0.0001 {
			w = 0.0001
		}
		scored[i] = scoredVideo{v: v, key: math.Pow(rand.Float64(), 1.0/w)}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].key > scored[j].key })

	result := make([]Video, len(scored))
	for i, s := range scored {
		result[i] = s.v
	}

	n := len(result)
	discoveryCount := int(float64(n) * discoveryRatio)
	if discoveryCount > 0 && n > discoveryCount {
		discoveryPool := make([]Video, len(poolCopy))
		copy(discoveryPool, poolCopy)
		rand.Shuffle(len(discoveryPool), func(i, j int) { discoveryPool[i], discoveryPool[j] = discoveryPool[j], discoveryPool[i] })

		top := result[:n-discoveryCount]
		topIDs := make(map[string]bool, len(top))
		for _, v := range top {
			topIDs[v.VideoID] = true
		}
		fillers := make([]Video, 0, discoveryCount)
		for _, v := range discoveryPool {
			if !topIDs[v.VideoID] {
				fillers = append(fillers, v)
				if len(fillers) >= discoveryCount {
					break
				}
			}
		}
		merged := make([]Video, len(top))
		copy(merged, top)
		for i, f := range fillers {
			insertAt := len(merged)
			denom := len(merged) / max(1, len(fillers))
			if denom < 1 {
				denom = 1
			}
			at := (i + 1) * denom
			if at < insertAt {
				insertAt = at
			}
			merged = append(merged, Video{})
			copy(merged[insertAt+1:], merged[insertAt:])
			merged[insertAt] = f
		}
		result = merged
	}

	return DiversifyByMember(result)
}

func DiversifyByMember(videos []Video) []Video {
	buckets := make(map[string][]Video)
	order := make([]string, 0)
	for _, v := range videos {
		m := v.MemberName
		if _, ok := buckets[m]; !ok {
			order = append(order, m)
		}
		buckets[m] = append(buckets[m], v)
	}
	result := make([]Video, 0, len(videos))
	for len(order) > 0 {
		next := order[:0:0]
		for _, m := range order {
			b := buckets[m]
			if len(b) > 0 {
				result = append(result, b[0])
				buckets[m] = b[1:]
			}
			if len(buckets[m]) > 0 {
				next = append(next, m)
			}
		}
		order = next
	}
	return result
}

func fetchWatchedVideoIDs(db *pdb.DB, userID int64, limit int) []string {
	rows, err := db.Query("SELECT video_id FROM watch_history WHERE user_id = ? ORDER BY watched_at DESC LIMIT ?", userID, limit)
	if err != nil {
		log.Printf("홈 화면 개인화 시청기록 조회 실패(user_id=%d): %v", userID, err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var vid string
		if rows.Scan(&vid) == nil {
			out = append(out, vid)
		}
	}
	return out
}

func fetchBookmarkedVideoIDs(db *pdb.DB, userID int64, limit int) []string {
	rows, err := db.Query("SELECT video_id FROM user_bookmarks WHERE user_id = ? ORDER BY id DESC LIMIT ?", userID, limit)
	if err != nil {
		log.Printf("북마크 채널 가중치 조회 실패(user_id=%d): %v", userID, err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var vid string
		if rows.Scan(&vid) == nil {
			out = append(out, vid)
		}
	}
	return out
}

func fetchTrendingVideoBoost(db *pdb.DB, c *cache.Store) map[string]float64 {
	const cacheKey = "trending_video_boost_v1"
	if cached, ok := c.Get(cacheKey); ok {
		if m, ok := cached.(map[string]float64); ok {
			return m
		}
	}
	cutoff := "datetime('now','-3 days')"
	if db.Backend == "mysql" {
		cutoff = "DATE_SUB(NOW(), INTERVAL 3 DAY)"
	}
	boost := make(map[string]float64)
	rows, err := db.Query("SELECT video_id, COUNT(*) as c FROM watch_history WHERE watched_at >= " + cutoff + " GROUP BY video_id ORDER BY c DESC LIMIT 150")
	if err != nil {
		log.Printf("트렌딩 영상 집계 실패: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var vid string
			var cnt int64
			if rows.Scan(&vid, &cnt) == nil {
				boost[vid] = float64(cnt)
			}
		}
	}
	c.Set(cacheKey, boost, 180*time.Second)
	return boost
}

func fetchCollaborativeVideoBoost(db *pdb.DB, c *cache.Store, userID int64, recentVideoIDs []string, watchedSet map[string]bool) map[string]float64 {
	cacheKey := fmt.Sprintf("collab_video_boost_v1_%d", userID)
	if cached, ok := c.Get(cacheKey); ok {
		if m, ok := cached.(map[string]float64); ok {
			return m
		}
	}
	boost := make(map[string]float64)
	sampleIDs := recentVideoIDs
	if len(sampleIDs) > 50 {
		sampleIDs = sampleIDs[:50]
	}
	if len(sampleIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(sampleIDs)), ",")
		args := make([]any, 0, len(sampleIDs)+1)
		for _, id := range sampleIDs {
			args = append(args, id)
		}
		args = append(args, userID)
		query := fmt.Sprintf("SELECT DISTINCT user_id FROM watch_history WHERE video_id IN (%s) AND user_id != ? LIMIT 300", placeholders)
		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("협업 필터링 집계 실패(user_id=%d): %v", userID, err)
		} else {
			var similarUserIDs []int64
			for rows.Next() {
				var uid int64
				if rows.Scan(&uid) == nil {
					similarUserIDs = append(similarUserIDs, uid)
				}
			}
			rows.Close()
			if len(similarUserIDs) > 0 {
				uPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(similarUserIDs)), ",")
				uArgs := make([]any, 0, len(similarUserIDs))
				for _, uid := range similarUserIDs {
					uArgs = append(uArgs, uid)
				}
				cutoff := "datetime('now','-14 days')"
				if db.Backend == "mysql" {
					cutoff = "DATE_SUB(NOW(), INTERVAL 14 DAY)"
				}
				uQuery := fmt.Sprintf("SELECT video_id, COUNT(DISTINCT user_id) as c FROM watch_history WHERE user_id IN (%s) AND watched_at >= "+cutoff+" GROUP BY video_id ORDER BY c DESC LIMIT 200", uPlaceholders)
				rows2, err2 := db.Query(uQuery, uArgs...)
				if err2 != nil {
					log.Printf("협업 필터링 집계 실패(user_id=%d): %v", userID, err2)
				} else {
					defer rows2.Close()
					for rows2.Next() {
						var vid string
						var cnt int64
						if rows2.Scan(&vid, &cnt) == nil && !watchedSet[vid] {
							boost[vid] = float64(cnt)
						}
					}
				}
			}
		}
	}
	c.Set(cacheKey, boost, 90*time.Second)
	return boost
}
