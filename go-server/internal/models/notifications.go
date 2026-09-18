package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

func InitNotificationsTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		actor_user_id INTEGER,
		actor_nickname VARCHAR(100),
		type VARCHAR(30) NOT NULL,
		target_type VARCHAR(20) NOT NULL,
		target_id INTEGER NOT NULL,
		preview_text VARCHAR(200),
		is_read INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`); err != nil {
		return err
	}
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON notifications (user_id, is_read, created_at)`)
	return nil
}

func truncatePreview(s string) string {
	r := []rune(s)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

func CreateNotification(d *pdb.DB, recipientUserID, actorUserID int64, actorNickname, notifType, targetType string, targetID int64, previewText string) error {
	if recipientUserID == 0 || recipientUserID == actorUserID {
		return nil
	}
	var actorIDArg any
	if actorUserID != 0 {
		actorIDArg = actorUserID
	}
	_, err := d.Exec(
		"INSERT INTO notifications (user_id, actor_user_id, actor_nickname, type, target_type, target_id, preview_text) VALUES (?, ?, ?, ?, ?, ?, ?)",
		recipientUserID, actorIDArg, actorNickname, notifType, targetType, targetID, truncatePreview(previewText),
	)
	return err
}

type Notification struct {
	ID            int64
	ActorNickname sql.NullString
	Type          string
	TargetType    string
	TargetID      int64
	PreviewText   sql.NullString
	IsRead        bool
	CreatedAt     string
}

func ListNotifications(d *pdb.DB, userID int64, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := d.Query(
		"SELECT id, actor_nickname, type, target_type, target_id, preview_text, is_read, created_at "+
			"FROM notifications WHERE user_id = ? ORDER BY id DESC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		var n Notification
		var isRead int
		if err := rows.Scan(&n.ID, &n.ActorNickname, &n.Type, &n.TargetType, &n.TargetID, &n.PreviewText, &isRead, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.IsRead = isRead != 0
		out = append(out, n)
	}
	return out, rows.Err()
}

func CountUnreadNotifications(d *pdb.DB, userID int64) (int64, error) {
	var count int64
	err := d.QueryRow("SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = 0", userID).Scan(&count)
	return count, err
}

func MarkNotificationRead(d *pdb.DB, userID, notificationID int64) error {
	_, err := d.Exec("UPDATE notifications SET is_read = 1 WHERE id = ? AND user_id = ?", notificationID, userID)
	return err
}

func MarkAllNotificationsRead(d *pdb.DB, userID int64) error {
	_, err := d.Exec("UPDATE notifications SET is_read = 1 WHERE user_id = ? AND is_read = 0", userID)
	return err
}
