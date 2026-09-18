package models

import (
	"context"
	"database/sql"
	"log"
	"sync"

	pdb "pastellive/internal/db"
	"pastellive/internal/video"
)

func InitMemberVideoArchiveTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS member_video_archive (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		video_id VARCHAR(20) NOT NULL UNIQUE,
		member_name VARCHAR(100),
		channel_id VARCHAR(50),
		title VARCHAR(500),
		thumbnail VARCHAR(500),
		is_short TINYINT(1) DEFAULT 0,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_member_name ON member_video_archive (member_name)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_channel_id ON member_video_archive (channel_id)`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS member_archive_sync_state (
		channel_id VARCHAR(50) PRIMARY KEY,
		last_full_sync_at DATETIME NULL
	)`)
	return nil
}

type ArchiveVideo struct {
	VideoID    string
	MemberName sql.NullString
	Title      sql.NullString
	Thumbnail  sql.NullString
	IsShort    bool
}

func (v ArchiveVideo) ToTemplateMap() map[string]any {
	return map[string]any{
		"videoId": v.VideoID, "id": v.VideoID, "title": v.Title.String, "thumbnail": v.Thumbnail.String,
		"is_short": v.IsShort, "member_name": v.MemberName.String,
	}
}

func GetArchiveVideosByMember(d *pdb.DB, memberName string) ([]ArchiveVideo, error) {
	rows, err := d.Query("SELECT video_id, title, thumbnail, is_short FROM member_video_archive WHERE member_name = ?", memberName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveVideo
	for rows.Next() {
		var v ArchiveVideo
		var isShort int
		if err := rows.Scan(&v.VideoID, &v.Title, &v.Thumbnail, &isShort); err == nil {
			v.IsShort = isShort != 0
			v.MemberName = sql.NullString{String: memberName, Valid: true}
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func SearchArchiveVideos(d *pdb.DB, likePattern string, limit int) ([]ArchiveVideo, error) {
	rows, err := d.Query(
		"SELECT video_id, member_name, title, thumbnail, is_short FROM member_video_archive WHERE title LIKE ? OR member_name LIKE ? LIMIT ?",
		likePattern, likePattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveVideo
	for rows.Next() {
		var v ArchiveVideo
		var isShort int
		if err := rows.Scan(&v.VideoID, &v.MemberName, &v.Title, &v.Thumbnail, &isShort); err == nil {
			v.IsShort = isShort != 0
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func GetAllArchiveVideos(d *pdb.DB) ([]ArchiveVideo, error) {
	rows, err := d.Query("SELECT video_id, member_name, title, thumbnail, is_short FROM member_video_archive")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveVideo
	for rows.Next() {
		var v ArchiveVideo
		var isShort int
		if err := rows.Scan(&v.VideoID, &v.MemberName, &v.Title, &v.Thumbnail, &isShort); err == nil {
			v.IsShort = isShort != 0
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func FindMemberByLowerName(d *pdb.DB, lowerQuery string) (memberName, channelID string, found bool, err error) {
	var ch sql.NullString
	qerr := d.QueryRow("SELECT member_name, channel_id FROM members WHERE LOWER(member_name) = ?", lowerQuery).Scan(&memberName, &ch)
	if qerr == sql.ErrNoRows {
		return "", "", false, nil
	}
	if qerr != nil {
		return "", "", false, qerr
	}
	return memberName, ch.String, true, nil
}

func archiveVideoExists(d *pdb.DB, videoID string) (bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM member_video_archive WHERE video_id = ?", videoID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func upsertArchiveSyncState(d *pdb.DB, channelID string) error {
	nowExpr := d.NowExpr()
	if d.Backend == "mysql" {
		_, err := d.Exec("INSERT INTO member_archive_sync_state (channel_id, last_full_sync_at) VALUES (?, "+nowExpr+") ON DUPLICATE KEY UPDATE last_full_sync_at = "+nowExpr, channelID)
		return err
	}
	_, err := d.Exec("INSERT INTO member_archive_sync_state (channel_id, last_full_sync_at) VALUES (?, "+nowExpr+") ON CONFLICT(channel_id) DO UPDATE SET last_full_sync_at = "+nowExpr, channelID)
	return err
}

func SyncMemberChannelArchive(ctx context.Context, d *pdb.DB, memberName, channelID, apiKey string, full bool, maxPages int) int {
	if channelID == "" || len(channelID) < 2 || channelID[:2] != "UC" {
		return 0
	}
	totalNew := 0
	pageToken := ""
	for i := 0; i < maxPages; i++ {
		videos, next, err := video.FetchArchivePage(ctx, channelID, pageToken, apiKey)
		if err != nil || len(videos) == 0 {
			break
		}
		hitKnown := false
		for _, v := range videos {
			exists, err := archiveVideoExists(d, v.VideoID)
			if err != nil {
				continue
			}
			if exists {
				hitKnown = true
				continue
			}
			insertVerb := "INSERT OR IGNORE INTO"
			if d.Backend == "mysql" {
				insertVerb = "INSERT IGNORE INTO"
			}
			_, err = d.Exec(
				insertVerb+" member_video_archive (video_id, member_name, channel_id, title, thumbnail, is_short) VALUES (?, ?, ?, ?, ?, ?)",
				v.VideoID, memberName, channelID, v.Title, v.Thumbnail, boolToInt(v.IsShort),
			)
			if err == nil {
				totalNew++
			}
		}
		if !full && hitKnown {
			break
		}
		pageToken = next
		if pageToken == "" {
			break
		}
	}
	if err := upsertArchiveSyncState(d, channelID); err != nil {
		log.Printf("%s 채널 아카이브 동기화 상태 저장 실패: %v", memberName, err)
	}
	if totalNew > 0 {
		log.Printf("%s 채널 아카이브 동기화: 새 영상 %d개 추가", memberName, totalNew)
	}
	return totalNew
}

type memberChannelPair struct {
	MemberName string
	ChannelID  string
}

func SyncAllMembersVideoArchive(ctx context.Context, d *pdb.DB, apiKey string) {
	rows, err := d.Query("SELECT member_name, channel_id FROM members WHERE channel_id IS NOT NULL AND channel_id != 'UC_KANNA_PLACEHOLDER'")
	if err != nil {
		log.Printf("멤버 아카이브 동기화: 대상 조회 실패 - %v", err)
		return
	}
	var members []memberChannelPair
	for rows.Next() {
		var m memberChannelPair
		var ch sql.NullString
		if err := rows.Scan(&m.MemberName, &ch); err == nil {
			m.ChannelID = ch.String
			members = append(members, m)
		}
	}
	rows.Close()

	alreadySynced, err := GetSyncedChannelIDs(d)
	if err != nil {
		log.Printf("멤버 아카이브 동기화: 동기화 상태 조회 실패 - %v", err)
		alreadySynced = map[string]bool{}
	}

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, m := range members {
		wg.Add(1)
		sem <- struct{}{}
		go func(mm memberChannelPair) {
			defer wg.Done()
			defer func() { <-sem }()
			isFirstTime := !alreadySynced[mm.ChannelID]
			SyncMemberChannelArchive(ctx, d, mm.MemberName, mm.ChannelID, apiKey, isFirstTime, 400)
		}(m)
	}
	wg.Wait()
	log.Printf("멤버 채널 아카이브 동기화 완료: 총 %d개 채널 확인", len(members))
}

func GetSyncedChannelIDs(d *pdb.DB) (map[string]bool, error) {
	rows, err := d.Query("SELECT channel_id FROM member_archive_sync_state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var ch string
		if err := rows.Scan(&ch); err == nil {
			out[ch] = true
		}
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
