package models

import (
	pdb "pastellive/internal/db"
)

type ProfileVideo struct {
	ID          int64
	Title       string
	Description string
	VideoURL    string
	Thumbnail   string
	ViewCount   int64
	Date        string
}

func GetMyProfileVideos(d *pdb.DB, userID int64) ([]ProfileVideo, error) {
	query := `SELECT id, title, description, video_url, thumbnail, view_count, ` + sqlDtFmtDateOnly(d, "created_at") + ` as date
		FROM channel_videos WHERE channel_user_id = ? ORDER BY created_at DESC`
	rows, err := d.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProfileVideo
	for rows.Next() {
		var v ProfileVideo
		var description, videoURL, thumbnail *string
		if err := rows.Scan(&v.ID, &v.Title, &description, &videoURL, &thumbnail, &v.ViewCount, &v.Date); err == nil {
			if description != nil {
				v.Description = *description
			}
			if videoURL != nil {
				v.VideoURL = *videoURL
			}
			if thumbnail != nil {
				v.Thumbnail = *thumbnail
			}
			v.Date = formatDateOnly(v.Date)
			out = append(out, v)
		}
	}
	if out == nil {
		out = []ProfileVideo{}
	}
	return out, rows.Err()
}

func GetMyLikedVideoIDs(d *pdb.DB, userID int64) ([]string, error) {
	rows, err := d.Query("SELECT target_id FROM community_likes WHERE target_type='video' AND reaction='like' AND user_id=? ORDER BY id DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

type ProfilePost struct {
	ID            int64
	MemberName    string
	Title         string
	Content       string
	ImageURL      string
	VideoURL      string
	Date          string
	LikesCount    int64
	CommentsCount int64
}

func GetMyProfilePosts(d *pdb.DB, userID int64) ([]ProfilePost, error) {
	query := `SELECT id, member_name, title, content, image_url, video_url, ` + sqlDtFmt(d, "created_at") + ` as date,
		(SELECT COUNT(*) FROM community_likes WHERE target_type='post' AND target_id=community_posts.id AND reaction='like') as likes_count,
		(SELECT COUNT(*) FROM community_comments WHERE post_id = community_posts.id) as comments_count
		FROM community_posts WHERE user_id = ? ORDER BY created_at DESC`
	rows, err := d.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProfilePost
	for rows.Next() {
		var p ProfilePost
		var title, imageURL, videoURL *string
		if err := rows.Scan(&p.ID, &p.MemberName, &title, &p.Content, &imageURL, &videoURL, &p.Date, &p.LikesCount, &p.CommentsCount); err == nil {
			if title != nil {
				p.Title = *title
			}
			if imageURL != nil {
				p.ImageURL = *imageURL
			}
			if videoURL != nil {
				p.VideoURL = *videoURL
			}
			p.Date = formatDateShort(p.Date)
			out = append(out, p)
		}
	}
	if out == nil {
		out = []ProfilePost{}
	}
	return out, rows.Err()
}
