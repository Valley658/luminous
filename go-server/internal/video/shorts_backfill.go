package video

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"sync"

	pdb "pastellive/internal/db"
)

type shortsBackfillRow struct {
	MemberName      string
	ChannelID       sql.NullString
	ShortsPlaylists sql.NullString
}

func parseShortsPlaylists(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil
	}
	trimmed := make([]string, 0, len(out))
	for _, s := range out {
		if s = strings.TrimSpace(s); s != "" {
			trimmed = append(trimmed, s)
		}
	}
	return trimmed
}

func BackfillAllShortsCache(ctx context.Context, d *pdb.DB, apiKey string, maxPagesPerChannel int) {
	rows, err := d.Query("SELECT member_name, channel_id, shorts_playlists FROM members WHERE channel_id IS NOT NULL AND channel_id != 'UC_KANNA_PLACEHOLDER'")
	if err != nil {
		log.Printf("쇼츠 캐시 백필: 대상 조회 실패 - %v", err)
		return
	}
	var members []shortsBackfillRow
	for rows.Next() {
		var m shortsBackfillRow
		if err := rows.Scan(&m.MemberName, &m.ChannelID, &m.ShortsPlaylists); err == nil {
			members = append(members, m)
		}
	}
	rows.Close()

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, m := range members {
		wg.Add(1)
		sem <- struct{}{}
		go func(mm shortsBackfillRow) {
			defer wg.Done()
			defer func() { <-sem }()
			backfillOneMemberShorts(ctx, d, mm, apiKey, maxPagesPerChannel)
		}(m)
	}
	wg.Wait()
	log.Printf("쇼츠 캐시 백필 완료: 총 %d개 채널 확인", len(members))
}

func backfillOneMemberShorts(ctx context.Context, d *pdb.DB, m shortsBackfillRow, apiKey string, maxPages int) {
	channelID := m.ChannelID.String
	if len(channelID) < 2 || channelID[:2] != "UC" {
		return
	}
	playlists := parseShortsPlaylists(m.ShortsPlaylists)
	if len(playlists) == 0 {
		playlists = []string{"UUSH" + channelID[2:]}
	}
	totalSeeded := 0
	for _, plID := range playlists {
		pageToken := ""
		for i := 0; i < maxPages; i++ {
			videos, next := FetchLatestVideosPage(ctx, plID, true, pageToken, apiKey)
			if len(videos) == 0 {
				break
			}
			ids := make([]string, 0, len(videos))
			for _, v := range videos {
				if v.VideoID != "" {
					ids = append(ids, v.VideoID)
				}
			}
			SeedShortsCache(d, ids)
			totalSeeded += len(ids)
			pageToken = next
			if pageToken == "" {
				break
			}
		}
	}
	if totalSeeded > 0 {
		log.Printf("쇼츠 캐시 백필: %s - %d개 확인", m.MemberName, totalSeeded)
	}
}
