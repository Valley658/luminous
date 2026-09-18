package models

import (
	"database/sql"
	"sort"
	"strings"

	pdb "pastellive/internal/db"
)

func InsertVideoComment(d *pdb.DB, videoID, nickname, content string, picture sql.NullString, userID int64, ip string, imageURL sql.NullString) (int64, error) {
	res, err := d.Exec(
		"INSERT INTO video_comments (video_id, nickname, content, picture, user_id, ip_address, image_url) VALUES (?, ?, ?, ?, ?, ?, ?)",
		videoID, nickname, content, picture, userID, ip, imageURL,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateVideoCommentImage(d *pdb.DB, commentID int64, imageURL string) error {
	_, err := d.Exec("UPDATE video_comments SET image_url = ? WHERE id = ?", imageURL, commentID)
	return err
}

func CommentExists(d *pdb.DB, commentID int64) (bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM video_comments WHERE id = ?", commentID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func GetVideoCommentOwner(d *pdb.DB, commentID int64) (userID int64, found bool, err error) {
	qerr := d.QueryRow("SELECT user_id FROM video_comments WHERE id = ?", commentID).Scan(&userID)
	if qerr == sql.ErrNoRows {
		return 0, false, nil
	}
	if qerr != nil {
		return 0, false, qerr
	}
	return userID, true, nil
}

func ReactToComment(d *pdb.DB, commentID, userID int64, reaction, ip string) (int64, int64, string, error) {
	var existing string
	err := d.QueryRow("SELECT reaction FROM comment_likes WHERE comment_id = ? AND user_id = ?", commentID, userID).Scan(&existing)
	hasExisting := true
	if err == sql.ErrNoRows {
		hasExisting = false
	} else if err != nil {
		return 0, 0, "", err
	}

	myReaction := reaction
	switch {
	case hasExisting && existing == reaction:
		if _, err := d.Exec("DELETE FROM comment_likes WHERE comment_id = ? AND user_id = ?", commentID, userID); err != nil {
			return 0, 0, "", err
		}
		myReaction = ""
	case hasExisting:
		if _, err := d.Exec("UPDATE comment_likes SET reaction = ?, created_at = "+nowExpr(d)+" WHERE comment_id = ? AND user_id = ?", reaction, commentID, userID); err != nil {
			return 0, 0, "", err
		}
	default:
		if _, err := d.Exec("INSERT INTO comment_likes (comment_id, user_id, reaction, ip_address) VALUES (?, ?, ?, ?)", commentID, userID, reaction, ip); err != nil {
			return 0, 0, "", err
		}
	}

	var likeCount, dislikeCount int64
	if err := d.QueryRow("SELECT COUNT(*) FROM comment_likes WHERE comment_id = ? AND reaction = 'like'", commentID).Scan(&likeCount); err != nil {
		return 0, 0, "", err
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM comment_likes WHERE comment_id = ? AND reaction = 'dislike'", commentID).Scan(&dislikeCount); err != nil {
		return 0, 0, "", err
	}
	return likeCount, dislikeCount, myReaction, nil
}

func nowExpr(d *pdb.DB) string {
	if d.Backend == "mysql" {
		return "NOW()"
	}
	return "datetime('now','localtime')"
}

type VideoCommentDisplay struct {
	ID           int64
	Nickname     string
	Content      string
	Date         string
	Picture      sql.NullString
	ImageURL     sql.NullString
	LikeCount    int64
	DislikeCount int64
	MyReaction   sql.NullString
}

func (c VideoCommentDisplay) ToMap() map[string]any {
	nickname := c.Nickname
	if nickname == "" {
		nickname = "스텔리언"
	}
	var myReaction any
	if c.MyReaction.Valid {
		myReaction = c.MyReaction.String
	}
	return map[string]any{
		"id": c.ID, "nickname": nickname, "content": c.Content, "date": c.Date,
		"picture": m2s(c.Picture), "image_url": m2s(c.ImageURL),
		"like_count": c.LikeCount, "dislike_count": c.DislikeCount, "my_reaction": myReaction,
	}
}

func GetVideoComments(d *pdb.DB, videoID string, userID int64) ([]map[string]any, error) {
	rows, err := d.Query("SELECT id, nickname, content, created_at, picture, image_url FROM video_comments WHERE video_id = ? ORDER BY id DESC", videoID)
	if err != nil {
		return nil, err
	}
	var list []VideoCommentDisplay
	var ids []int64
	for rows.Next() {
		var c VideoCommentDisplay
		var nickname sql.NullString
		var createdAt string
		if err := rows.Scan(&c.ID, &nickname, &c.Content, &createdAt, &c.Picture, &c.ImageURL); err != nil {
			rows.Close()
			return nil, err
		}
		c.Nickname = nickname.String
		c.Date = formatDateShort(createdAt)
		list = append(list, c)
		ids = append(ids, c.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []map[string]any{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	likeCounts := make(map[int64]int64)
	dislikeCounts := make(map[int64]int64)
	if crows, err := d.Query("SELECT comment_id, reaction, COUNT(*) as cnt FROM comment_likes WHERE comment_id IN ("+placeholders+") GROUP BY comment_id, reaction", args...); err == nil {
		for crows.Next() {
			var cid int64
			var reaction string
			var cnt int64
			if crows.Scan(&cid, &reaction, &cnt) == nil {
				if reaction == "like" {
					likeCounts[cid] = cnt
				} else {
					dislikeCounts[cid] = cnt
				}
			}
		}
		crows.Close()
	}
	myReactions := make(map[int64]string)
	if userID != 0 {
		argsWithUser := append(append([]any{}, args...), userID)
		if mrows, err := d.Query("SELECT comment_id, reaction FROM comment_likes WHERE comment_id IN ("+placeholders+") AND user_id = ?", argsWithUser...); err == nil {
			for mrows.Next() {
				var cid int64
				var reaction string
				if mrows.Scan(&cid, &reaction) == nil {
					myReactions[cid] = reaction
				}
			}
			mrows.Close()
		}
	}

	for i := range list {
		list[i].LikeCount = likeCounts[list[i].ID]
		list[i].DislikeCount = dislikeCounts[list[i].ID]
		if r, ok := myReactions[list[i].ID]; ok {
			list[i].MyReaction = sql.NullString{String: r, Valid: true}
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].LikeCount > list[j].LikeCount })

	out := make([]map[string]any, len(list))
	for i, c := range list {
		out[i] = c.ToMap()
	}
	return out, nil
}
