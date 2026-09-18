package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

// InitPlaylistVideoItemsTable creates the table that links videos to a
// user's playlist (user_playlists only stores the playlist itself).
// Like InitInquiriesTable, this does NOT skip the mysql backend: there is
// no separate migration mechanism for the production database in this
// codebase, so both backends are created here directly (see main.go's
// fanart_phash table for the same pattern).
func InitPlaylistVideoItemsTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS user_playlist_videos (
			id INT AUTO_INCREMENT PRIMARY KEY,
			playlist_id VARCHAR(50) NOT NULL,
			video_id VARCHAR(50) NOT NULL,
			title VARCHAR(255),
			thumbnail TEXT,
			added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY unique_playlist_video (playlist_id, video_id)
		)`)
		return err
	}
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS user_playlist_videos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		playlist_id VARCHAR(50) NOT NULL,
		video_id VARCHAR(50) NOT NULL,
		title VARCHAR(255),
		thumbnail TEXT,
		added_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	if err != nil {
		return err
	}
	_, err = d.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS unique_playlist_video ON user_playlist_videos (playlist_id, video_id)`)
	return err
}

// GetPlaylistOwner returns the user_id that owns the given playlist_id, or
// 0 if the playlist does not exist.
func GetPlaylistOwner(d *pdb.DB, playlistID string) (int64, error) {
	var userID int64
	err := d.QueryRow("SELECT user_id FROM user_playlists WHERE playlist_id = ?", playlistID).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return userID, nil
}

// AddVideoToPlaylist adds a video to a playlist. added is false (with no
// error) if the video was already in that playlist.
func AddVideoToPlaylist(d *pdb.DB, playlistID, videoID, title, thumbnail string) (added bool, err error) {
	var exists int
	if err := d.QueryRow("SELECT COUNT(*) FROM user_playlist_videos WHERE playlist_id = ? AND video_id = ?", playlistID, videoID).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	_, err = d.Exec(
		"INSERT INTO user_playlist_videos (playlist_id, video_id, title, thumbnail) VALUES (?, ?, ?, ?)",
		playlistID, videoID, title, thumbnail,
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

func RemoveVideoFromPlaylist(d *pdb.DB, playlistID, videoID string) error {
	_, err := d.Exec("DELETE FROM user_playlist_videos WHERE playlist_id = ? AND video_id = ?", playlistID, videoID)
	return err
}

type PlaylistVideoItem struct {
	VideoID   string
	Title     sql.NullString
	Thumbnail sql.NullString
	AddedAt   string
}

func GetPlaylistVideos(d *pdb.DB, playlistID string) ([]PlaylistVideoItem, error) {
	rows, err := d.Query(
		"SELECT video_id, title, thumbnail, added_at FROM user_playlist_videos WHERE playlist_id = ? ORDER BY id DESC",
		playlistID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PlaylistVideoItem, 0)
	for rows.Next() {
		var v PlaylistVideoItem
		if err := rows.Scan(&v.VideoID, &v.Title, &v.Thumbnail, &v.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
