package models

import (
	"strings"

	pdb "pastellive/internal/db"
)

func AddHistory(d *pdb.DB, userID int64, videoID, title, thumbnail, ip string) error {
	var query string
	if d.Backend == "mysql" {
		query = "INSERT INTO watch_history (user_id, video_id, title, thumbnail, ip_address, watched_at) VALUES (?, ?, ?, ?, ?, NOW()) " +
			"ON DUPLICATE KEY UPDATE watched_at = NOW(), title = VALUES(title), thumbnail = VALUES(thumbnail), ip_address = VALUES(ip_address)"
	} else {
		query = "INSERT INTO watch_history (user_id, video_id, title, thumbnail, ip_address, watched_at) VALUES (?, ?, ?, ?, ?, datetime('now','localtime')) " +
			"ON CONFLICT(user_id, video_id) DO UPDATE SET watched_at = datetime('now','localtime'), title = excluded.title, thumbnail = excluded.thumbnail, ip_address = excluded.ip_address"
	}
	_, err := d.Exec(query, userID, videoID, title, thumbnail, ip)
	return err
}

type HistoryItem struct {
	VideoID   string
	Title     string
	Thumbnail string
}

func GetHistory(d *pdb.DB, userID int64) ([]HistoryItem, error) {
	rows, err := d.Query("SELECT video_id, title, thumbnail FROM watch_history WHERE user_id = ? ORDER BY watched_at DESC LIMIT 100", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryItem
	for rows.Next() {
		var h HistoryItem
		if err := rows.Scan(&h.VideoID, &h.Title, &h.Thumbnail); err == nil {
			out = append(out, h)
		}
	}
	if out == nil {
		out = []HistoryItem{}
	}
	return out, rows.Err()
}

func DeleteHistoryItems(d *pdb.DB, userID int64, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(videoIDs)), ",")
	args := make([]any, 0, len(videoIDs)+1)
	args = append(args, userID)
	for _, id := range videoIDs {
		args = append(args, id)
	}
	_, err := d.Exec("DELETE FROM watch_history WHERE user_id = ? AND video_id IN ("+placeholders+")", args...)
	return err
}

func ClearHistory(d *pdb.DB, userID int64) error {
	_, err := d.Exec("DELETE FROM watch_history WHERE user_id = ?", userID)
	return err
}
