package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

func InitSchedulesTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS member_schedules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		member_name VARCHAR(50) NOT NULL,
		event_date DATE NOT NULL,
		event_time TIME NULL,
		title VARCHAR(255) NOT NULL,
		is_day_off TINYINT(1) NOT NULL DEFAULT 0,
		created_by INT NULL,
		updated_by INT NULL,
		created_at DATETIME DEFAULT (datetime('now','localtime')),
		updated_at DATETIME DEFAULT (datetime('now','localtime'))
	)`); err != nil {
		return err
	}
	if _, err := d.Exec(`CREATE INDEX IF NOT EXISTS idx_schedule_date ON member_schedules (event_date)`); err != nil {
		return err
	}
	_, _ = d.Exec(`CREATE TRIGGER IF NOT EXISTS trg_member_schedules_updated_at
		AFTER UPDATE ON member_schedules
		FOR EACH ROW
		WHEN NEW.updated_at = OLD.updated_at
		BEGIN
			UPDATE member_schedules SET updated_at = datetime('now','localtime') WHERE id = NEW.id;
		END`)
	return nil
}

type ScheduleRow struct {
	ID                int64
	MemberName        string
	EventDate         string
	EventTime         sql.NullString
	Title             string
	IsDayOff          bool
	CreatedByNickname sql.NullString
}

func (s ScheduleRow) ToMap() map[string]any {
	var eventTime any
	if s.EventTime.Valid {
		eventTime = s.EventTime.String
	}
	var createdBy any
	if s.CreatedByNickname.Valid {
		createdBy = s.CreatedByNickname.String
	}
	return map[string]any{
		"id":                  s.ID,
		"member_name":         s.MemberName,
		"event_date":          s.EventDate,
		"event_time":          eventTime,
		"title":               s.Title,
		"is_day_off":          s.IsDayOff,
		"created_by_nickname": createdBy,
	}
}

func GetSchedules(d *pdb.DB, startDate, endDate string) ([]map[string]any, error) {
	rows, err := d.Query(`
		SELECT s.id, s.member_name, s.event_date, s.event_time, s.title, s.is_day_off,
		       u.nickname AS created_by_nickname
		FROM member_schedules s
		LEFT JOIN users u ON u.id = s.created_by
		WHERE s.event_date BETWEEN ? AND ?
		ORDER BY s.event_date ASC, (s.event_time IS NULL) ASC, s.event_time ASC
	`, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var s ScheduleRow
		var isDayOff int64
		var eventDate string
		if err := rows.Scan(&s.ID, &s.MemberName, &eventDate, &s.EventTime, &s.Title, &isDayOff, &s.CreatedByNickname); err != nil {
			return nil, err
		}

		if len(eventDate) > 10 {
			eventDate = eventDate[:10]
		}
		s.EventDate = eventDate
		if s.EventTime.Valid && len(s.EventTime.String) > 5 {
			s.EventTime.String = s.EventTime.String[:5]
		}
		s.IsDayOff = isDayOff != 0
		out = append(out, s.ToMap())
	}
	return out, rows.Err()
}

func CreateSchedule(d *pdb.DB, memberName, eventDate string, eventTime sql.NullString, title string, isDayOff bool, userID int64) (int64, error) {
	res, err := d.Exec(`INSERT INTO member_schedules (member_name, event_date, event_time, title, is_day_off, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, memberName, eventDate, eventTime, title, boolToInt(isDayOff), userID, userID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ScheduleExists(d *pdb.DB, scheduleID int64) (bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM member_schedules WHERE id=?", scheduleID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func UpdateSchedule(d *pdb.DB, scheduleID int64, memberName, eventDate string, eventTime sql.NullString, title string, isDayOff bool, userID int64) error {
	_, err := d.Exec(`UPDATE member_schedules
		SET member_name=?, event_date=?, event_time=?, title=?, is_day_off=?, updated_by=?
		WHERE id=?`, memberName, eventDate, eventTime, title, boolToInt(isDayOff), userID, scheduleID)
	return err
}

func DeleteSchedule(d *pdb.DB, scheduleID int64) (int64, error) {
	res, err := d.Exec("DELETE FROM member_schedules WHERE id=?", scheduleID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
