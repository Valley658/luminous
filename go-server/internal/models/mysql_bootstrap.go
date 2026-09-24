package models

import (
	pdb "pastellive/internal/db"
)

// InitMySQLMissingTables recreates, on MySQL only, every table that the
// various InitXTable() functions in this package only ever created on the
// SQLite branch (they early-return on MySQL because the original MySQL
// schema was assumed to already exist, having been created years ago by
// the now-deleted Python app - there was never a Go/SQL migration for it).
// After a from-scratch database this left ~13 tables (and a few extra
// columns on `users`) missing, breaking attendance, fanart, highlights,
// kirinuki archive, lumi chat history, notifications, schedules, search
// trends, watch history/comment likes/playlists and the shorts cache.
//
// This is a one-time recovery shim: it is intentionally verbose/explicit
// (one CREATE TABLE per feature, mirroring the SQLite definitions in each
// file) rather than clever, so it's easy to audit against the code that
// actually queries these tables. All statements are idempotent
// (IF NOT EXISTS / duplicate-column errors ignored) and safe to run on
// every startup.
func InitMySQLMissingTables(d *pdb.DB) error {
	if d.Backend != "mysql" {
		return nil
	}

	// --- users: attendance columns (normally added by attendance.go, which
	// skips MySQL). GetUserByID/LoginID/DiscordID all SELECT these columns
	// unconditionally, so without them every login/register/profile lookup
	// fails with "Unknown column 'total_attendance'". ---
	for _, s := range []string{
		`ALTER TABLE users ADD COLUMN total_attendance INT DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN consecutive_attendance INT DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN last_attendance_date DATE`,
	} {
		_, _ = d.Exec(s)
	}

	// --- attendance.go: user_attendance ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS user_attendance (
		id INT PRIMARY KEY AUTO_INCREMENT,
		user_id INT NOT NULL,
		attendance_date DATE NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_user_date (user_id, attendance_date)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- channel.go: channel_videos / channel_subscriptions ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS channel_videos (
		id INT PRIMARY KEY AUTO_INCREMENT,
		channel_user_id INT NOT NULL,
		title VARCHAR(200) NOT NULL,
		description TEXT NULL,
		video_url TEXT NOT NULL,
		thumbnail TEXT NULL,
		view_count INT DEFAULT 0,
		ip_address VARCHAR(45) NULL,
		tags VARCHAR(500) NULL,
		video_link VARCHAR(255) NULL,
		visibility VARCHAR(20) DEFAULT 'public',
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_channel_created (channel_user_id, created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS channel_subscriptions (
		id INT PRIMARY KEY AUTO_INCREMENT,
		subscriber_user_id INT NOT NULL,
		channel_user_id INT NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_subscription (subscriber_user_id, channel_user_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- fanart.go: fanart_gallery / fanart_reactions / fanart_reports / fanart_comments ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS fanart_gallery (
		id INT PRIMARY KEY AUTO_INCREMENT,
		user_id INT NOT NULL,
		nickname VARCHAR(50) NOT NULL,
		image_url TEXT NOT NULL,
		title VARCHAR(50) NULL,
		description TEXT NULL,
		status VARCHAR(20) DEFAULT 'active',
		discord_message_id VARCHAR(100) NULL,
		thumbnail_url TEXT NULL,
		lqip TEXT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS fanart_reactions (
		id INT PRIMARY KEY AUTO_INCREMENT,
		fanart_id INT NOT NULL,
		user_id INT NOT NULL,
		reaction_type VARCHAR(10) NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_fanart_user_reaction (fanart_id, user_id, reaction_type),
		CHECK (reaction_type IN ('like', 'dislike', 'report'))
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS fanart_reports (
		id INT PRIMARY KEY AUTO_INCREMENT,
		fanart_id INT NOT NULL,
		reporter_user_id INT NULL,
		reporter_nickname VARCHAR(50) NULL,
		reason VARCHAR(100) NULL,
		fanart_title VARCHAR(50) NULL,
		fanart_image_url TEXT NULL,
		fanart_author VARCHAR(50) NULL,
		status VARCHAR(20) DEFAULT 'pending',
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS fanart_comments (
		id INT PRIMARY KEY AUTO_INCREMENT,
		fanart_id INT NOT NULL,
		user_id INT NOT NULL,
		nickname VARCHAR(50) NOT NULL,
		picture TEXT NULL,
		content VARCHAR(500) NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_fanart_comments_fanart_id (fanart_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- highlights.go: highlight_clips ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS highlight_clips (
		id INT PRIMARY KEY AUTO_INCREMENT,
		source_video_id VARCHAR(20) NOT NULL,
		source_title VARCHAR(255) NULL,
		member_name VARCHAR(50) NULL,
		start_seconds INT NOT NULL,
		duration_seconds INT NOT NULL DEFAULT 15,
		tag_text VARCHAR(200) NOT NULL,
		status VARCHAR(20) NOT NULL DEFAULT 'pending',
		clip_path VARCHAR(255) NULL,
		gif_path VARCHAR(255) NULL,
		error_message TEXT NULL,
		ip_address VARCHAR(64) NULL,
		user_id INT NULL,
		nickname VARCHAR(50) NULL,
		view_count INT NOT NULL DEFAULT 0,
		sprite_path VARCHAR(255) NULL,
		sprite_frame_count INT NULL,
		sprite_frame_width INT NULL,
		sprite_interval_seconds DOUBLE NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_status (status),
		INDEX idx_source_video (source_video_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- kirinuki.go: kirinuki_channels (+ seed) ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS kirinuki_channels (
		id INT PRIMARY KEY AUTO_INCREMENT,
		channel_id VARCHAR(50) NOT NULL UNIQUE,
		channel_name VARCHAR(100) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	for _, chID := range KirinukiSeedChannelIDs {
		_, _ = d.Exec("INSERT IGNORE INTO kirinuki_channels (channel_id, channel_name) VALUES (?, ?)", chID, "키리누키")
	}

	// --- lumi_chat.go: lumi_chat_history ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS lumi_chat_history (
		id INT PRIMARY KEY AUTO_INCREMENT,
		user_id INT NOT NULL,
		question TEXT NOT NULL,
		reply TEXT NOT NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_lumi_chat_history_user (user_id, id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- notifications.go: notifications ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS notifications (
		id INT PRIMARY KEY AUTO_INCREMENT,
		user_id INT NOT NULL,
		actor_user_id INT NULL,
		actor_nickname VARCHAR(100) NULL,
		type VARCHAR(30) NOT NULL,
		target_type VARCHAR(20) NOT NULL,
		target_id INT NOT NULL,
		preview_text VARCHAR(200) NULL,
		is_read INT NOT NULL DEFAULT 0,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_notifications_user_unread (user_id, is_read, created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- schedules.go: member_schedules ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS member_schedules (
		id INT PRIMARY KEY AUTO_INCREMENT,
		member_name VARCHAR(50) NOT NULL,
		event_date DATE NOT NULL,
		event_time TIME NULL,
		title VARCHAR(255) NOT NULL,
		is_day_off TINYINT(1) NOT NULL DEFAULT 0,
		created_by INT NULL,
		updated_by INT NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_schedule_date (event_date)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- search_trends.go: search_trends ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS search_trends (
		keyword VARCHAR(100) PRIMARY KEY,
		search_count INT DEFAULT 1,
		last_searched TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- video_tables.go: watch_history / comment_likes / user_playlists / user_bookmarks ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS watch_history (
		id INT PRIMARY KEY AUTO_INCREMENT,
		user_id INT NOT NULL,
		video_id VARCHAR(50) NOT NULL,
		title VARCHAR(255) NULL,
		thumbnail TEXT NULL,
		ip_address VARCHAR(45) NULL,
		watched_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_user_video (user_id, video_id),
		INDEX idx_watch_history_video_id (video_id),
		INDEX idx_watch_history_watched_at (watched_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS comment_likes (
		id INT PRIMARY KEY AUTO_INCREMENT,
		comment_id INT NOT NULL,
		user_id INT NOT NULL,
		reaction VARCHAR(10) NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_comment_user (comment_id, user_id),
		CHECK (reaction IN ('like', 'dislike'))
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS user_playlists (
		id INT PRIMARY KEY AUTO_INCREMENT,
		playlist_id VARCHAR(50) UNIQUE NOT NULL,
		user_id INT NOT NULL,
		title VARCHAR(255) NOT NULL,
		privacy VARCHAR(20) DEFAULT 'private',
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS user_bookmarks (
		id INT PRIMARY KEY AUTO_INCREMENT,
		user_id INT NOT NULL,
		video_id VARCHAR(50) NOT NULL,
		title VARCHAR(255) NULL,
		thumbnail TEXT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_user_bookmark (user_id, video_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- video/shorts_cache.go: video_shorts_cache ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS video_shorts_cache (
		video_id VARCHAR(20) PRIMARY KEY,
		is_short TINYINT(1) NOT NULL,
		checked_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- video_comments.go: video_comments (had NO Init function at all,
	// on either backend - same situation as `members`). ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS video_comments (
		id INT PRIMARY KEY AUTO_INCREMENT,
		video_id VARCHAR(50) NOT NULL,
		user_id INT NULL,
		nickname VARCHAR(50) NULL,
		picture TEXT NULL,
		content TEXT NOT NULL,
		image_url TEXT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_video_comments_video_id (video_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// --- community.go: community_posts / community_comments / community_likes
	// (also had NO Init function at all, on either backend). ---
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS community_posts (
		id INT PRIMARY KEY AUTO_INCREMENT,
		member_name VARCHAR(50) NOT NULL,
		user_id INT NULL,
		nickname VARCHAR(50) NULL,
		picture TEXT NULL,
		title VARCHAR(100) NULL,
		content TEXT NOT NULL,
		image_url TEXT NULL,
		video_url VARCHAR(255) NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_community_posts_member_name (member_name, created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS community_comments (
		id INT PRIMARY KEY AUTO_INCREMENT,
		post_id INT NOT NULL,
		user_id INT NULL,
		nickname VARCHAR(50) NULL,
		picture TEXT NULL,
		content TEXT NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_community_comments_post_id (post_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	// target_id must stay a string column (not INT): "video" targets store a
	// YouTube video ID there, while "post"/"comment" targets store a numeric
	// ID as text - see CommunityLikeTarget in community.go.
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS community_likes (
		id INT PRIMARY KEY AUTO_INCREMENT,
		target_type VARCHAR(20) NOT NULL,
		target_id VARCHAR(50) NOT NULL,
		user_id INT NOT NULL,
		reaction VARCHAR(10) NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_community_like (target_type, target_id, user_id),
		CHECK (reaction IN ('like', 'dislike'))
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)

	// NOTE: "blocked_bots" is intentionally NOT created here. It's named in
	// admin_dashboard.go's DB overview list, but nothing in this repo ever
	// reads or writes to it - a dead reference to an old Python-app feature
	// that never made it into the Go rewrite. See DropUnusedTables below,
	// which removes it if a previous run of this code already created it.

	return nil
}

// DropUnusedTables removes tables that used to get created defensively but
// turned out to have no real code behind them anywhere in this repo, mirroring
// the existing `DROP TABLE IF EXISTS public_error_events` cleanup in main.go
// for the same reason. Safe/idempotent to run on every startup.
func DropUnusedTables(d *pdb.DB) error {
	_, _ = d.Exec(`DROP TABLE IF EXISTS blocked_bots`)
	return nil
}
