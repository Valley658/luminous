package models

import (
	pdb "pastellive/internal/db"
)

func InitHistoryTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS watch_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INT NOT NULL,
			video_id VARCHAR(50) NOT NULL,
			title VARCHAR(255),
			thumbnail TEXT,
			watched_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS unique_user_video ON watch_history (user_id, video_id)`,
		`CREATE INDEX IF NOT EXISTS idx_watch_history_video_id ON watch_history (video_id)`,
		`CREATE INDEX IF NOT EXISTS idx_watch_history_watched_at ON watch_history (watched_at)`,
	}
	for _, s := range stmts {
		_, _ = d.Exec(s)
	}
	_, _ = d.Exec(`ALTER TABLE watch_history ADD COLUMN ip_address VARCHAR(45)`)
	return nil
}

func InitCommentLikesTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS comment_likes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			comment_id INT NOT NULL,
			user_id INT NOT NULL,
			reaction TEXT NOT NULL CHECK(reaction IN ('like', 'dislike')),
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS unique_comment_user ON comment_likes (comment_id, user_id)`,
	}
	for _, s := range stmts {
		_, _ = d.Exec(s)
	}

	_, _ = d.Exec(`ALTER TABLE comment_likes ADD COLUMN ip_address VARCHAR(45)`)
	return nil
}

func InitPlaylistBookmarkTables(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_playlists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			playlist_id VARCHAR(50) UNIQUE NOT NULL,
			user_id INT NOT NULL,
			title VARCHAR(255) NOT NULL,
			privacy VARCHAR(20) DEFAULT 'private',
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE TABLE IF NOT EXISTS user_bookmarks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INT NOT NULL,
			video_id VARCHAR(50) NOT NULL,
			title VARCHAR(255),
			thumbnail TEXT,
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS unique_user_bookmark ON user_bookmarks (user_id, video_id)`,
	}
	for _, s := range stmts {
		_, _ = d.Exec(s)
	}
	_, _ = d.Exec(`ALTER TABLE user_playlists ADD COLUMN ip_address VARCHAR(45)`)
	_, _ = d.Exec(`ALTER TABLE user_bookmarks ADD COLUMN ip_address VARCHAR(45)`)
	return nil
}
