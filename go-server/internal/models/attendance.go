package models

import (
	"database/sql"
	"time"

	pdb "pastellive/internal/db"
)

var KST = time.FixedZone("KST", 9*60*60)

func KSTToday() string {
	return time.Now().In(KST).Format("2006-01-02")
}

func InitAttendanceTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS user_attendance (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INT NOT NULL,
		attendance_date DATE NOT NULL,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	_, _ = d.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS unique_user_date ON user_attendance (user_id, attendance_date)`)
	_, _ = d.Exec(`ALTER TABLE user_attendance ADD COLUMN ip_address VARCHAR(45)`)
	for _, s := range []string{
		`ALTER TABLE users ADD COLUMN total_attendance INT DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN consecutive_attendance INT DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN last_attendance_date DATE`,
	} {
		_, _ = d.Exec(s)
	}
	return nil
}

func HasAttendedToday(d *pdb.DB, userID int64, today string) (bool, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM user_attendance WHERE user_id = ? AND attendance_date = ?", userID, today).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func MarkAttendance(d *pdb.DB, userID int64, today, yesterday, ip string) (total, consecutive int64, alreadyDone bool, err error) {
	done, err := HasAttendedToday(d, userID, today)
	if err != nil {
		return 0, 0, false, err
	}
	if done {
		return 0, 0, true, nil
	}
	if _, err = d.Exec("INSERT INTO user_attendance (user_id, attendance_date, ip_address) VALUES (?, ?, ?)", userID, today, ip); err != nil {
		return 0, 0, false, err
	}

	var totalAttendance, consecutiveAttendance sql.NullInt64
	var lastDate sql.NullString
	if err = d.QueryRow("SELECT total_attendance, consecutive_attendance, last_attendance_date FROM users WHERE id = ?", userID).
		Scan(&totalAttendance, &consecutiveAttendance, &lastDate); err != nil {
		return 0, 0, false, err
	}

	total = totalAttendance.Int64 + 1

	lastDateStr := lastDate.String
	if len(lastDateStr) > 10 {
		lastDateStr = lastDateStr[:10]
	}
	if lastDate.Valid && lastDateStr == yesterday {
		consecutive = consecutiveAttendance.Int64 + 1
	} else {
		consecutive = 1
	}

	_, err = d.Exec("UPDATE users SET total_attendance = ?, consecutive_attendance = ?, last_attendance_date = ? WHERE id = ?", total, consecutive, today, userID)
	if err != nil {
		return 0, 0, false, err
	}
	return total, consecutive, false, nil
}
