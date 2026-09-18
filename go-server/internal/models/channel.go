package models

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	pdb "pastellive/internal/db"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func InitChannelTables(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS channel_videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_user_id INT NOT NULL,
			title VARCHAR(200) NOT NULL,
			description TEXT,
			video_url TEXT NOT NULL,
			thumbnail TEXT,
			view_count INT DEFAULT 0,
			ip_address VARCHAR(45),
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_created ON channel_videos (channel_user_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS channel_subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subscriber_user_id INT NOT NULL,
			channel_user_id INT NOT NULL,
			ip_address VARCHAR(45),
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS unique_subscription ON channel_subscriptions (subscriber_user_id, channel_user_id)`,
	}
	for _, s := range stmts {
		_, _ = d.Exec(s)
	}

	for _, s := range []string{
		`ALTER TABLE channel_videos ADD COLUMN tags VARCHAR(500)`,
		`ALTER TABLE channel_videos ADD COLUMN video_link VARCHAR(255)`,
		`ALTER TABLE channel_videos ADD COLUMN visibility VARCHAR(20) DEFAULT 'public'`,
	} {
		_, _ = d.Exec(s)
	}
	return nil
}

func GetUserIDByChannelID(d *pdb.DB, channelID string) (int64, bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM users WHERE channel_id = ?", channelID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func GetOrCreateChannelID(d *pdb.DB, secretKey string, userID int64) (string, error) {
	var existing sql.NullString
	err := d.QueryRow("SELECT channel_id FROM users WHERE id = ?", userID).Scan(&existing)
	if err != nil {
		return "", err
	}
	if existing.Valid && existing.String != "" {
		return existing.String, nil
	}
	channelID := computeChannelID(secretKey, userID)
	if _, err := d.Exec("UPDATE users SET channel_id = ? WHERE id = ?", channelID, userID); err != nil {
		return "", err
	}
	return channelID, nil
}

func computeChannelID(secretKey string, userID int64) string {
	return "UC" + sha256Hex(fmt.Sprintf("channel%d%s", userID, secretKey))[:22]
}

type ChannelOwner struct {
	ID                 int64
	Nickname           sql.NullString
	Picture            sql.NullString
	CreatedAt          sql.NullString
	ChannelDescription sql.NullString
	ChannelLink        sql.NullString
	ChannelCountry     sql.NullString
}

func GetChannelOwnerByChannelID(d *pdb.DB, channelID string) (*ChannelOwner, error) {
	var o ChannelOwner
	err := d.QueryRow(
		"SELECT id, nickname, picture, created_at, channel_description, channel_link, channel_country FROM users WHERE channel_id = ?",
		channelID,
	).Scan(&o.ID, &o.Nickname, &o.Picture, &o.CreatedAt, &o.ChannelDescription, &o.ChannelLink, &o.ChannelCountry)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func CountSubscribers(d *pdb.DB, channelUserID int64) (int64, error) {
	var cnt int64
	err := d.QueryRow("SELECT COUNT(*) FROM channel_subscriptions WHERE channel_user_id = ?", channelUserID).Scan(&cnt)
	return cnt, err
}

func CountChannelVideosAndViews(d *pdb.DB, channelUserID int64) (count int64, views int64, err error) {
	err = d.QueryRow("SELECT COUNT(*) as cnt, COALESCE(SUM(view_count), 0) as views FROM channel_videos WHERE channel_user_id = ?", channelUserID).
		Scan(&count, &views)
	return
}

func IsSubscribed(d *pdb.DB, viewerID, channelUserID int64) (bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM channel_subscriptions WHERE subscriber_user_id = ? AND channel_user_id = ?", viewerID, channelUserID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

type ChannelVideo struct {
	ID          int64
	Title       string
	Description sql.NullString
	VideoURL    string
	Thumbnail   sql.NullString
	ViewCount   int64
	CreatedAt   sql.NullString
}

func ListChannelVideos(d *pdb.DB, channelUserID int64, limit int) ([]ChannelVideo, error) {
	query := "SELECT id, title, description, video_url, thumbnail, view_count, created_at FROM channel_videos WHERE channel_user_id = ? ORDER BY created_at DESC"
	args := []any{channelUserID}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelVideo
	for rows.Next() {
		var v ChannelVideo
		if err := rows.Scan(&v.ID, &v.Title, &v.Description, &v.VideoURL, &v.Thumbnail, &v.ViewCount, &v.CreatedAt); err == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func DeleteChannelVideo(d *pdb.DB, videoID, channelUserID int64) error {
	_, err := d.Exec("DELETE FROM channel_videos WHERE id = ? AND channel_user_id = ?", videoID, channelUserID)
	return err
}

type CommunityPost struct {
	ID         int64
	MemberName sql.NullString
	Title      sql.NullString
	Content    sql.NullString
	ImageURL   sql.NullString
	VideoURL   sql.NullString
	CreatedAt  sql.NullString
}

func ListCommunityPostsByUser(d *pdb.DB, userID int64, limit int) ([]map[string]any, error) {
	rows, err := d.Query(
		"SELECT id, member_name, title, content, image_url, video_url, created_at FROM community_posts WHERE user_id = ? ORDER BY created_at DESC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var p CommunityPost
		if err := rows.Scan(&p.ID, &p.MemberName, &p.Title, &p.Content, &p.ImageURL, &p.VideoURL, &p.CreatedAt); err != nil {
			continue
		}
		var images []string
		if p.ImageURL.Valid && p.ImageURL.String != "" {
			_ = json.Unmarshal([]byte(p.ImageURL.String), &images)
		}
		out = append(out, map[string]any{
			"id": p.ID, "member_name": p.MemberName.String, "title": p.Title.String,
			"content": p.Content.String, "images": images, "video_url": p.VideoURL.String,
			"created_at": p.CreatedAt.String,
		})
	}
	return out, rows.Err()
}

func ToggleSubscription(d *pdb.DB, subscriberID, channelUserID int64, ip string) (subscribed bool, err error) {
	var existingID int64
	err = d.QueryRow("SELECT id FROM channel_subscriptions WHERE subscriber_user_id = ? AND channel_user_id = ?", subscriberID, channelUserID).Scan(&existingID)
	if err == nil {
		if _, derr := d.Exec("DELETE FROM channel_subscriptions WHERE id = ?", existingID); derr != nil {
			return false, derr
		}
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, err = d.Exec("INSERT INTO channel_subscriptions (subscriber_user_id, channel_user_id, ip_address) VALUES (?, ?, ?)", subscriberID, channelUserID, ip)
	if err != nil {
		return false, err
	}
	return true, nil
}
