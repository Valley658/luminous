package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

func InitHighlightClipTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS highlight_clips (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_video_id VARCHAR(20) NOT NULL,
		source_title VARCHAR(255),
		member_name VARCHAR(50),
		start_seconds INT NOT NULL,
		duration_seconds INT NOT NULL DEFAULT 15,
		tag_text VARCHAR(200) NOT NULL,
		status VARCHAR(20) NOT NULL DEFAULT 'pending',
		clip_path VARCHAR(255),
		gif_path VARCHAR(255),
		error_message TEXT,
		ip_address VARCHAR(64),
		user_id INT,
		nickname VARCHAR(50),
		view_count INT NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_status ON highlight_clips (status)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_source_video ON highlight_clips (source_video_id)`)
	for _, s := range []string{
		`ALTER TABLE highlight_clips ADD COLUMN user_id INT`,
		`ALTER TABLE highlight_clips ADD COLUMN nickname VARCHAR(50)`,
		`ALTER TABLE highlight_clips ADD COLUMN sprite_path VARCHAR(255)`,
		`ALTER TABLE highlight_clips ADD COLUMN sprite_frame_count INT`,
		`ALTER TABLE highlight_clips ADD COLUMN sprite_frame_width INT`,
		`ALTER TABLE highlight_clips ADD COLUMN sprite_interval_seconds REAL`,
	} {
		_, _ = d.Exec(s)
	}
	return nil
}

func InsertHighlightClip(d *pdb.DB, sourceVideoID string, sourceTitle, memberName sql.NullString, startSeconds, durationSeconds int, tagText, ip string, userID int64, nickname string) (int64, error) {
	res, err := d.Exec(
		"INSERT INTO highlight_clips (source_video_id, source_title, member_name, start_seconds, duration_seconds, tag_text, status, ip_address, user_id, nickname) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)",
		sourceVideoID, ns(sourceTitle), ns(memberName), startSeconds, durationSeconds, tagText, ip, userID, nickname,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ns(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

type HighlightClipStatus struct {
	ID                    int64
	SourceVideoID         string
	SourceTitle           sql.NullString
	MemberName            sql.NullString
	StartSeconds          int64
	DurationSeconds       int64
	TagText               string
	Status                string
	ClipPath              sql.NullString
	GifPath               sql.NullString
	SpritePath            sql.NullString
	SpriteFrameCount      sql.NullInt64
	SpriteFrameWidth      sql.NullInt64
	SpriteIntervalSeconds sql.NullFloat64
	ErrorMessage          sql.NullString
	Nickname              sql.NullString
}

func (c HighlightClipStatus) ToMap() map[string]any {
	return map[string]any{
		"id": c.ID, "source_video_id": c.SourceVideoID, "source_title": m2s(c.SourceTitle),
		"member_name": m2s(c.MemberName), "start_seconds": c.StartSeconds, "duration_seconds": c.DurationSeconds,
		"tag_text": c.TagText, "status": c.Status, "clip_path": m2s(c.ClipPath), "gif_path": m2s(c.GifPath),
		"sprite_path": m2s(c.SpritePath), "sprite_frame_count": nullInt64Val(c.SpriteFrameCount),
		"sprite_frame_width": nullInt64Val(c.SpriteFrameWidth), "sprite_interval_seconds": nullFloat64Val(c.SpriteIntervalSeconds),
		"error_message": m2s(c.ErrorMessage), "nickname": m2s(c.Nickname),
	}
}

func nullFloat64Val(n sql.NullFloat64) any {
	if n.Valid {
		return n.Float64
	}
	return nil
}

func GetHighlightClipStatus(d *pdb.DB, clipID int64) (map[string]any, bool, error) {
	var c HighlightClipStatus
	qerr := d.QueryRow(
		"SELECT id, source_video_id, source_title, member_name, start_seconds, duration_seconds, "+
			"tag_text, status, clip_path, gif_path, sprite_path, sprite_frame_count, sprite_frame_width, "+
			"sprite_interval_seconds, error_message, nickname FROM highlight_clips WHERE id = ?", clipID,
	).Scan(&c.ID, &c.SourceVideoID, &c.SourceTitle, &c.MemberName, &c.StartSeconds, &c.DurationSeconds,
		&c.TagText, &c.Status, &c.ClipPath, &c.GifPath, &c.SpritePath, &c.SpriteFrameCount, &c.SpriteFrameWidth,
		&c.SpriteIntervalSeconds, &c.ErrorMessage, &c.Nickname)
	if qerr == sql.ErrNoRows {
		return nil, false, nil
	}
	if qerr != nil {
		return nil, false, qerr
	}
	return c.ToMap(), true, nil
}

type HighlightClipListItem struct {
	ID                    int64
	SourceVideoID         string
	SourceTitle           sql.NullString
	MemberName            sql.NullString
	StartSeconds          int64
	DurationSeconds       int64
	TagText               string
	ClipPath              sql.NullString
	GifPath               sql.NullString
	SpritePath            sql.NullString
	SpriteFrameCount      sql.NullInt64
	SpriteFrameWidth      sql.NullInt64
	SpriteIntervalSeconds sql.NullFloat64
	ViewCount             int64
	Nickname              sql.NullString
	CreatedAt             string
}

func (c HighlightClipListItem) ToMap() map[string]any {
	return map[string]any{
		"id": c.ID, "source_video_id": c.SourceVideoID, "source_title": m2s(c.SourceTitle),
		"member_name": m2s(c.MemberName), "start_seconds": c.StartSeconds, "duration_seconds": c.DurationSeconds,
		"tag_text": c.TagText, "clip_path": m2s(c.ClipPath), "gif_path": m2s(c.GifPath),
		"sprite_path": m2s(c.SpritePath), "sprite_frame_count": nullInt64Val(c.SpriteFrameCount),
		"sprite_frame_width": nullInt64Val(c.SpriteFrameWidth), "sprite_interval_seconds": nullFloat64Val(c.SpriteIntervalSeconds),
		"view_count": c.ViewCount, "nickname": m2s(c.Nickname), "created_at": c.CreatedAt,
	}
}

func ListHighlightClips(d *pdb.DB, member string, limit, offset int) ([]map[string]any, error) {
	query := `SELECT id, source_video_id, source_title, member_name, start_seconds, duration_seconds,
		tag_text, clip_path, gif_path, sprite_path, sprite_frame_count, sprite_frame_width,
		sprite_interval_seconds, view_count, nickname, created_at FROM highlight_clips
		WHERE status = 'done'`
	var args []any
	if member != "" {
		query += " AND member_name = ?"
		args = append(args, member)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var c HighlightClipListItem
		if err := rows.Scan(&c.ID, &c.SourceVideoID, &c.SourceTitle, &c.MemberName, &c.StartSeconds, &c.DurationSeconds,
			&c.TagText, &c.ClipPath, &c.GifPath, &c.SpritePath, &c.SpriteFrameCount, &c.SpriteFrameWidth,
			&c.SpriteIntervalSeconds, &c.ViewCount, &c.Nickname, &c.CreatedAt); err == nil {
			out = append(out, c.ToMap())
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
}
