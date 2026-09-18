package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

func CheckBookmark(d *pdb.DB, userID int64, videoID string) (bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM user_bookmarks WHERE user_id = ? AND video_id = ?", userID, videoID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func ToggleBookmark(d *pdb.DB, userID int64, videoID, title, thumbnail, ip string) (bookmarked bool, err error) {
	var existingID int64
	qerr := d.QueryRow("SELECT id FROM user_bookmarks WHERE user_id = ? AND video_id = ?", userID, videoID).Scan(&existingID)
	if qerr == nil {
		_, err = d.Exec("DELETE FROM user_bookmarks WHERE id = ?", existingID)
		return false, err
	}
	if qerr != sql.ErrNoRows {
		return false, qerr
	}
	_, err = d.Exec("INSERT INTO user_bookmarks (user_id, video_id, title, thumbnail, ip_address) VALUES (?, ?, ?, ?, ?)", userID, videoID, title, thumbnail, ip)
	return true, err
}

type BookmarkItem struct {
	VideoID   string
	Title     string
	Thumbnail string
	Date      string
}

func GetBookmarks(d *pdb.DB, userID int64) ([]BookmarkItem, error) {
	query := "SELECT video_id, title, thumbnail, " + sqlDtFmtDateOnly(d, "created_at") + " as date FROM user_bookmarks WHERE user_id = ? ORDER BY created_at DESC"
	rows, err := d.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BookmarkItem
	for rows.Next() {
		var b BookmarkItem
		if err := rows.Scan(&b.VideoID, &b.Title, &b.Thumbnail, &b.Date); err == nil {
			b.Date = formatDateOnly(b.Date)
			out = append(out, b)
		}
	}
	if out == nil {
		out = []BookmarkItem{}
	}
	return out, rows.Err()
}

func sqlDtFmtDateOnly(d *pdb.DB, col string) string {
	if d.Backend == "mysql" {
		return "DATE_FORMAT(" + col + ", '%Y-%m-%d')"
	}
	return col
}

func formatDateOnly(s string) string {
	if len(s) > 10 {
		s = s[:10]
	}
	return s
}
