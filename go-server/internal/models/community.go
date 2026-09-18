package models

import (
	"database/sql"
	"encoding/json"
	"strings"

	pdb "pastellive/internal/db"
)

func formatDateShort(s string) string {
	s = strings.TrimSuffix(s, "Z")
	s = strings.Replace(s, "T", " ", 1)
	if len(s) > 16 {
		s = s[:16]
	}
	return s
}

func timeHHMMExpr(d *pdb.DB, col string) string {
	if d.Backend == "mysql" {
		return "DATE_FORMAT(" + col + ", '%H:%i')"
	}
	return "strftime('%H:%M', " + col + ")"
}

type Cheer struct {
	Nickname string
	Message  string
	Time     string
}

func GetCheers(d *pdb.DB, memberName string) ([]Cheer, error) {
	query := "SELECT nickname, message, " + timeHHMMExpr(d, "created_at") + " as time FROM live_cheers WHERE member_name = ? ORDER BY id DESC LIMIT 50"
	rows, err := d.Query(query, memberName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cheer
	for rows.Next() {
		var c Cheer
		if rows.Scan(&c.Nickname, &c.Message, &c.Time) == nil {
			out = append(out, c)
		}
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

func AddCheer(d *pdb.DB, memberName, nickname, message, ip string) error {
	_, err := d.Exec("INSERT INTO live_cheers (member_name, nickname, message, ip_address) VALUES (?, ?, ?, ?)", memberName, nickname, message, ip)
	return err
}

func MemberExists(d *pdb.DB, memberName string) (bool, error) {
	var cnt int64
	err := d.QueryRow("SELECT COUNT(*) FROM members WHERE member_name = ?", memberName).Scan(&cnt)
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func CreateCommunityPost(d *pdb.DB, memberName string, userID int64, nickname, picture, title, content, imageURLJSON string, videoURL sql.NullString, ip string) error {
	nowExpr := d.NowExpr()
	sql := "INSERT INTO community_posts (member_name, user_id, nickname, picture, title, content, image_url, video_url, ip_address, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, " + nowExpr + ")"
	var titleArg any
	if title != "" {
		titleArg = title
	}
	_, err := d.Exec(sql, memberName, userID, nickname, picture, titleArg, content, imageURLJSON, videoURL, ip)
	return err
}

type CommunityPostRow struct {
	ID            int64
	MemberName    string
	UserID        sql.NullInt64
	Nickname      sql.NullString
	Picture       sql.NullString
	Title         sql.NullString
	Content       string
	ImageURL      sql.NullString
	VideoURL      sql.NullString
	CreatedAt     string
	LikesCount    int64
	CommentsCount int64
}

func GetCommunityPosts(d *pdb.DB, memberName, sortBy string, viewerID int64) ([]map[string]any, error) {
	orderClause := "ORDER BY p.created_at DESC"
	if sortBy == "popular" {
		orderClause = "ORDER BY likes_count DESC, p.created_at DESC"
	}
	query := `SELECT p.id, p.member_name, p.user_id, p.nickname, p.picture, p.title, p.content, p.image_url, p.video_url, p.created_at,
		(SELECT COUNT(*) FROM community_likes WHERE target_type='post' AND target_id=p.id AND reaction='like') as likes_count,
		(SELECT COUNT(*) FROM community_comments WHERE post_id=p.id) as comments_count
		FROM community_posts p WHERE p.member_name = ? ` + orderClause
	rows, err := d.Query(query, memberName)
	if err != nil {
		return nil, err
	}
	var posts []CommunityPostRow
	var ids []int64
	for rows.Next() {
		var p CommunityPostRow
		if err := rows.Scan(&p.ID, &p.MemberName, &p.UserID, &p.Nickname, &p.Picture, &p.Title, &p.Content, &p.ImageURL, &p.VideoURL, &p.CreatedAt, &p.LikesCount, &p.CommentsCount); err == nil {
			posts = append(posts, p)
			ids = append(ids, p.ID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	myReactions := make(map[int64]string)
	if viewerID != 0 && len(ids) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, 0, len(ids)+1)
		for _, id := range ids {
			args = append(args, id)
		}
		args = append(args, viewerID)
		rrows, err := d.Query("SELECT target_id, reaction FROM community_likes WHERE target_type='post' AND target_id IN ("+placeholders+") AND user_id = ?", args...)
		if err == nil {
			for rrows.Next() {
				var tid int64
				var reaction string
				if rrows.Scan(&tid, &reaction) == nil {
					myReactions[tid] = reaction
				}
			}
			rrows.Close()
		}
	}

	out := make([]map[string]any, len(posts))
	for i, p := range posts {
		var images []string
		if p.ImageURL.Valid && p.ImageURL.String != "" {
			images = parseJSONStringArray(p.ImageURL.String)
		}
		author := p.Nickname.String
		if author == "" {
			author = "스텔리언"
		}
		var myReaction any
		if r, ok := myReactions[p.ID]; ok {
			myReaction = r
		}
		out[i] = map[string]any{
			"id": p.ID, "member_name": p.MemberName, "user_id": nullInt64Val(p.UserID),
			"nickname": p.Nickname.String, "picture": p.Picture.String, "title": p.Title.String,
			"content": p.Content, "images": images, "video_url": p.VideoURL.String,
			"created_at": p.CreatedAt, "likes_count": p.LikesCount, "comments_count": p.CommentsCount,
			"author": author, "my_reaction": myReaction,
		}
	}
	return out, nil
}

func nullInt64Val(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

func parseJSONStringArray(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func CommunityLikeTarget(d *pdb.DB, targetType string, targetIDInt int64, targetIDStr string, userID int64, reaction, ip string) (likeCount, dislikeCount int64, myReaction string, err error) {
	targetIDForQuery := any(targetIDInt)
	if targetType == "video" {
		targetIDForQuery = targetIDStr
	}

	var existing string
	qerr := d.QueryRow("SELECT reaction FROM community_likes WHERE target_type=? AND target_id=? AND user_id=?", targetType, targetIDForQuery, userID).Scan(&existing)
	switch {
	case qerr == nil && existing == reaction:
		if _, err = d.Exec("DELETE FROM community_likes WHERE target_type=? AND target_id=? AND user_id=?", targetType, targetIDForQuery, userID); err != nil {
			return
		}
		myReaction = ""
	case qerr == nil:
		if _, err = d.Exec("UPDATE community_likes SET reaction=? WHERE target_type=? AND target_id=? AND user_id=?", reaction, targetType, targetIDForQuery, userID); err != nil {
			return
		}
		myReaction = reaction
	case qerr == sql.ErrNoRows:
		if _, err = d.Exec("INSERT INTO community_likes (target_type, target_id, user_id, reaction, ip_address) VALUES (?, ?, ?, ?, ?)", targetType, targetIDForQuery, userID, reaction, ip); err != nil {
			return
		}
		myReaction = reaction
	default:
		err = qerr
		return
	}

	if err = d.QueryRow("SELECT COUNT(*) FROM community_likes WHERE target_type=? AND target_id=? AND reaction='like'", targetType, targetIDForQuery).Scan(&likeCount); err != nil {
		return
	}
	err = d.QueryRow("SELECT COUNT(*) FROM community_likes WHERE target_type=? AND target_id=? AND reaction='dislike'", targetType, targetIDForQuery).Scan(&dislikeCount)
	return
}

func CommunityTargetExists(d *pdb.DB, targetType string, targetID int64) (bool, error) {
	table := "community_posts"
	if targetType == "comment" {
		table = "community_comments"
	}
	var id int64
	err := d.QueryRow("SELECT id FROM "+table+" WHERE id = ?", targetID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func GetVideoReaction(d *pdb.DB, videoID string, userID int64) (likeCount, dislikeCount int64, myReaction string, err error) {
	if err = d.QueryRow("SELECT COUNT(*) FROM community_likes WHERE target_type='video' AND target_id=? AND reaction='like'", videoID).Scan(&likeCount); err != nil {
		return
	}
	if err = d.QueryRow("SELECT COUNT(*) FROM community_likes WHERE target_type='video' AND target_id=? AND reaction='dislike'", videoID).Scan(&dislikeCount); err != nil {
		return
	}
	if userID != 0 {
		var r string
		qerr := d.QueryRow("SELECT reaction FROM community_likes WHERE target_type='video' AND target_id=? AND user_id=?", videoID, userID).Scan(&r)
		if qerr == nil {
			myReaction = r
		} else if qerr != sql.ErrNoRows {
			err = qerr
		}
	}
	return
}

type CommunityComment struct {
	ID        int64
	UserID    sql.NullInt64
	Nickname  sql.NullString
	Picture   sql.NullString
	Content   string
	CreatedAt string
}

func GetCommunityComments(d *pdb.DB, postID int64, viewerID int64) ([]map[string]any, error) {
	rows, err := d.Query("SELECT id, user_id, nickname, picture, content, created_at FROM community_comments WHERE post_id = ? ORDER BY id ASC", postID)
	if err != nil {
		return nil, err
	}
	var comments []CommunityComment
	var ids []int64
	for rows.Next() {
		var c CommunityComment
		if err := rows.Scan(&c.ID, &c.UserID, &c.Nickname, &c.Picture, &c.Content, &c.CreatedAt); err == nil {
			comments = append(comments, c)
			ids = append(ids, c.ID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	likeCounts := make(map[int64]int64)
	dislikeCounts := make(map[int64]int64)
	myReactions := make(map[int64]string)
	if len(ids) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		rrows, err := d.Query("SELECT target_id, reaction, COUNT(*) as cnt FROM community_likes WHERE target_type='comment' AND target_id IN ("+placeholders+") GROUP BY target_id, reaction", args...)
		if err == nil {
			for rrows.Next() {
				var tid, cnt int64
				var reaction string
				if rrows.Scan(&tid, &reaction, &cnt) == nil {
					if reaction == "like" {
						likeCounts[tid] = cnt
					} else {
						dislikeCounts[tid] = cnt
					}
				}
			}
			rrows.Close()
		}
		if viewerID != 0 {
			args2 := make([]any, 0, len(ids)+1)
			for _, id := range ids {
				args2 = append(args2, id)
			}
			args2 = append(args2, viewerID)
			mrows, err := d.Query("SELECT target_id, reaction FROM community_likes WHERE target_type='comment' AND target_id IN ("+placeholders+") AND user_id = ?", args2...)
			if err == nil {
				for mrows.Next() {
					var tid int64
					var reaction string
					if mrows.Scan(&tid, &reaction) == nil {
						myReactions[tid] = reaction
					}
				}
				mrows.Close()
			}
		}
	}

	out := make([]map[string]any, len(comments))
	for i, c := range comments {
		nickname := c.Nickname.String
		if nickname == "" {
			nickname = "스텔리언"
		}
		date := formatDateShort(c.CreatedAt)
		var myReaction any
		if r, ok := myReactions[c.ID]; ok {
			myReaction = r
		}
		out[i] = map[string]any{
			"id": c.ID, "user_id": nullInt64Val(c.UserID), "nickname": nickname,
			"picture": c.Picture.String, "content": c.Content, "date": date,
			"like_count": likeCounts[c.ID], "dislike_count": dislikeCounts[c.ID],
			"my_reaction": myReaction, "is_mine": viewerID != 0 && c.UserID.Valid && c.UserID.Int64 == viewerID,
		}
	}
	return out, nil
}

func GetCommunityPostOwner(d *pdb.DB, postID int64) (userID int64, found bool, err error) {
	var uid sql.NullInt64
	qerr := d.QueryRow("SELECT user_id FROM community_posts WHERE id = ?", postID).Scan(&uid)
	if qerr == sql.ErrNoRows {
		return 0, false, nil
	}
	if qerr != nil {
		return 0, false, qerr
	}
	return uid.Int64, uid.Valid, nil
}

func AddCommunityComment(d *pdb.DB, postID int64, userID int64, nickname, picture, content, ip string) (newID int64, postExists bool, err error) {
	var id int64
	qerr := d.QueryRow("SELECT id FROM community_posts WHERE id = ?", postID).Scan(&id)
	if qerr == sql.ErrNoRows {
		return 0, false, nil
	}
	if qerr != nil {
		return 0, false, qerr
	}
	res, err := d.Exec("INSERT INTO community_comments (post_id, user_id, nickname, picture, content, ip_address) VALUES (?, ?, ?, ?, ?, ?)", postID, userID, nickname, picture, content, ip)
	if err != nil {
		return 0, true, err
	}
	newID, err = res.LastInsertId()
	return newID, true, err
}

func GetCommunityCommentOwner(d *pdb.DB, commentID int64) (userID int64, found bool, err error) {
	var uid sql.NullInt64
	qerr := d.QueryRow("SELECT user_id FROM community_comments WHERE id = ?", commentID).Scan(&uid)
	if qerr == sql.ErrNoRows {
		return 0, false, nil
	}
	if qerr != nil {
		return 0, false, qerr
	}
	return uid.Int64, true, nil
}

func DeleteCommunityComment(d *pdb.DB, commentID int64) error {
	if _, err := d.Exec("DELETE FROM community_comments WHERE id = ?", commentID); err != nil {
		return err
	}
	_, err := d.Exec("DELETE FROM community_likes WHERE target_type='comment' AND target_id=?", commentID)
	return err
}
