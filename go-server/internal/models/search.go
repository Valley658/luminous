package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

type SearchVideoResult struct {
	Title      string
	Thumbnail  string
	VideoID    string
	IsShort    bool
	MemberName string
	ChannelID  string
	IsUserChan bool
}

func (v SearchVideoResult) ToMap() map[string]any {
	m := map[string]any{
		"title": v.Title, "thumbnail": v.Thumbnail, "videoId": v.VideoID, "id": v.VideoID,
		"is_short": v.IsShort, "member_name": v.MemberName,
	}
	if v.IsUserChan {
		m["channel_id"] = v.ChannelID
		m["is_user_channel"] = true
	}
	return m
}

func SearchUserChannelVideos(d *pdb.DB, likePattern string, limit int) ([]SearchVideoResult, error) {
	rows, err := d.Query(
		`SELECT cv.video_url AS video_id, cv.title, cv.thumbnail, u.nickname, u.channel_id
		FROM channel_videos cv JOIN users u ON cv.channel_user_id = u.id
		WHERE (cv.title LIKE ? OR cv.description LIKE ? OR u.nickname LIKE ?)
		AND (cv.visibility IS NULL OR cv.visibility = 'public')
		ORDER BY cv.created_at DESC LIMIT ?`,
		likePattern, likePattern, likePattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchVideoResult
	for rows.Next() {
		var videoID, title, thumbnail, nickname, channelID sql.NullString
		if err := rows.Scan(&videoID, &title, &thumbnail, &nickname, &channelID); err == nil {
			if !videoID.Valid || videoID.String == "" {
				continue
			}
			nick := nickname.String
			if nick == "" {
				nick = "유저"
			}
			out = append(out, SearchVideoResult{
				Title: title.String, Thumbnail: thumbnail.String, VideoID: videoID.String,
				MemberName: nick + " 채널", ChannelID: channelID.String, IsUserChan: true,
			})
		}
	}
	return out, rows.Err()
}

type SearchChannelResult struct {
	ChannelID   string
	Nickname    string
	Picture     string
	Description string
	VideoCount  int64
}

func SearchChannels(d *pdb.DB, likePattern string, limit int) ([]SearchChannelResult, error) {
	rows, err := d.Query(
		"SELECT id, nickname, picture, channel_id, channel_description FROM users WHERE channel_id IS NOT NULL AND nickname LIKE ? LIMIT ?",
		likePattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		ID          int64
		Nickname    sql.NullString
		Picture     sql.NullString
		ChannelID   sql.NullString
		Description sql.NullString
	}
	var rowsOut []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Nickname, &r.Picture, &r.ChannelID, &r.Description); err == nil && r.ChannelID.Valid && r.ChannelID.String != "" {
			rowsOut = append(rowsOut, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []SearchChannelResult
	for _, r := range rowsOut {
		var cnt int64
		_ = d.QueryRow("SELECT COUNT(*) FROM channel_videos WHERE channel_user_id = ? AND (visibility IS NULL OR visibility = 'public')", r.ID).Scan(&cnt)
		out = append(out, SearchChannelResult{
			ChannelID: r.ChannelID.String, Nickname: r.Nickname.String, Picture: r.Picture.String,
			Description: r.Description.String, VideoCount: cnt,
		})
	}
	return out, nil
}

type SearchCommunityResult struct {
	ID         int64
	MemberName sql.NullString
	Author     sql.NullString
	Content    sql.NullString
	Date       string
}

func SearchCommunityPosts(d *pdb.DB, rawQuery string, limit int) ([]SearchCommunityResult, error) {
	likePattern := "%" + rawQuery + "%"
	query := "SELECT id, member_name, nickname AS author, content, " + sqlDtFmt(d, "created_at") +
		" as date FROM community_posts WHERE content LIKE ? ORDER BY created_at DESC LIMIT ?"
	rows, err := d.Query(query, likePattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchCommunityResult
	for rows.Next() {
		var r SearchCommunityResult
		if err := rows.Scan(&r.ID, &r.MemberName, &r.Author, &r.Content, &r.Date); err == nil {
			r.Date = formatDateShort(r.Date)
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

type SearchFanartResult struct {
	ID           int64
	Title        sql.NullString
	Description  sql.NullString
	ImageURL     sql.NullString
	ThumbnailURL sql.NullString
	Nickname     sql.NullString
}

func SearchFanartGallery(d *pdb.DB, rawQuery string, limit int) ([]SearchFanartResult, error) {
	likePattern := "%" + rawQuery + "%"
	rows, err := d.Query(
		"SELECT id, title, description, image_url, thumbnail_url, nickname FROM fanart_gallery "+
			"WHERE status = 'active' AND (title LIKE ? OR description LIKE ?) ORDER BY id DESC LIMIT ?",
		likePattern, likePattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchFanartResult
	for rows.Next() {
		var r SearchFanartResult
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &r.ImageURL, &r.ThumbnailURL, &r.Nickname); err == nil {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}
