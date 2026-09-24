package models

import (
	"database/sql"
	"time"

	pdb "pastellive/internal/db"
)

type User struct {
	ID                    int64
	Email                 sql.NullString
	Name                  sql.NullString
	Picture               sql.NullString
	Nickname              sql.NullString
	CreatedAt             time.Time
	UpdatedAt             time.Time
	LastIP                sql.NullString
	ChannelID             sql.NullString
	ChannelDescription    sql.NullString
	ChannelLink           sql.NullString
	ChannelCountry        sql.NullString
	LoginID               sql.NullString
	PasswordHash          sql.NullString
	DiscordID             sql.NullString
	DiscordUsername       sql.NullString
	TotalAttendance       sql.NullInt64
	ConsecutiveAttendance sql.NullInt64
	LastAttendanceDate    sql.NullString
}

func (u *User) ToTemplateMap() map[string]any {
	m := map[string]any{
		"id": u.ID, "picture": u.Picture.String, "nickname": u.Nickname.String,
		"email": u.Email.String, "name": u.Name.String,
		"total_attendance": int64(0), "consecutive_attendance": int64(0),
		"discord_username": u.DiscordUsername.String,
	}
	if u.TotalAttendance.Valid {
		m["total_attendance"] = u.TotalAttendance.Int64
	}
	if u.ConsecutiveAttendance.Valid {
		m["consecutive_attendance"] = u.ConsecutiveAttendance.Int64
	}
	return m
}

func (u *User) NicknameOr(def string) string {
	if u.Nickname.Valid {
		return u.Nickname.String
	}
	return def
}

func (u *User) PictureOr(def string) string {
	if u.Picture.Valid {
		return u.Picture.String
	}
	return def
}

func InitUsersTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS users (
			id INT PRIMARY KEY AUTO_INCREMENT,
			email VARCHAR(255) NULL,
			name VARCHAR(255) NULL,
			picture TEXT NULL,
			nickname VARCHAR(50) NULL,
			last_ip VARCHAR(45) NULL,
			channel_id VARCHAR(30) NULL,
			channel_description TEXT NULL,
			channel_link VARCHAR(255) NULL,
			channel_country VARCHAR(50) NULL,
			login_id VARCHAR(30) NULL,
			password_hash VARCHAR(255) NULL,
			discord_id VARCHAR(30) NULL,
			discord_username VARCHAR(64) NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_users_channel_id (channel_id),
			UNIQUE KEY uniq_users_login_id (login_id),
			UNIQUE KEY uniq_users_discord_id (discord_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email VARCHAR(255),
			name VARCHAR(255),
			picture TEXT,
			nickname VARCHAR(50),
			created_at DATETIME DEFAULT (datetime('now','localtime')),
			updated_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`ALTER TABLE users ADD COLUMN last_ip VARCHAR(45)`,
		`ALTER TABLE users ADD COLUMN channel_id VARCHAR(30)`,
		`ALTER TABLE users ADD COLUMN channel_description TEXT`,
		`ALTER TABLE users ADD COLUMN channel_link VARCHAR(255)`,
		`ALTER TABLE users ADD COLUMN channel_country VARCHAR(50)`,
		`ALTER TABLE users ADD COLUMN login_id VARCHAR(30)`,
		`ALTER TABLE users ADD COLUMN password_hash VARCHAR(255)`,
		`ALTER TABLE users ADD COLUMN discord_id VARCHAR(30)`,
		`ALTER TABLE users ADD COLUMN discord_username VARCHAR(64)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_channel_id ON users (channel_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_login_id ON users (login_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_discord_id ON users (discord_id)`,
		`CREATE TRIGGER IF NOT EXISTS trg_users_updated_at
			AFTER UPDATE ON users
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
			BEGIN
				UPDATE users SET updated_at = datetime('now','localtime') WHERE id = NEW.id;
			END`,
	}
	for _, s := range stmts {

		_, _ = d.Exec(s)
	}
	return nil
}

const userCols = `id, email, name, picture, nickname, created_at, updated_at, last_ip,
	channel_id, channel_description, channel_link, channel_country,
	login_id, password_hash, discord_id, discord_username,
	total_attendance, consecutive_attendance, last_attendance_date`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var createdAt, updatedAt sql.NullTime
	err := row.Scan(
		&u.ID, &u.Email, &u.Name, &u.Picture, &u.Nickname, &createdAt, &updatedAt, &u.LastIP,
		&u.ChannelID, &u.ChannelDescription, &u.ChannelLink, &u.ChannelCountry,
		&u.LoginID, &u.PasswordHash, &u.DiscordID, &u.DiscordUsername,
		&u.TotalAttendance, &u.ConsecutiveAttendance, &u.LastAttendanceDate,
	)
	if err != nil {
		return nil, err
	}
	if createdAt.Valid {
		u.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		u.UpdatedAt = updatedAt.Time
	}
	return &u, nil
}

func GetUserByID(d *pdb.DB, id int64) (*User, error) {
	row := d.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func GetUserByLoginID(d *pdb.DB, loginID string) (*User, error) {
	row := d.QueryRow("SELECT "+userCols+" FROM users WHERE login_id = ?", loginID)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func GetUserByDiscordID(d *pdb.DB, discordID string) (*User, error) {
	row := d.QueryRow("SELECT "+userCols+" FROM users WHERE discord_id = ?", discordID)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func CreateUser(d *pdb.DB, loginID, passwordHash, nickname, lastIP string) (int64, error) {
	res, err := d.Exec(
		"INSERT INTO users (login_id, password_hash, nickname, last_ip) VALUES (?, ?, ?, ?)",
		loginID, passwordHash, nickname, lastIP,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateLastIP(d *pdb.DB, userID int64, ip string) error {
	_, err := d.Exec("UPDATE users SET last_ip=? WHERE id=?", ip, userID)
	return err
}

func UpdateLastIPAndPasswordHash(d *pdb.DB, userID int64, ip, hash string) error {
	_, err := d.Exec("UPDATE users SET last_ip=?, password_hash=? WHERE id=?", ip, hash, userID)
	return err
}

func UpdateLoginCredentials(d *pdb.DB, userID int64, loginID, passwordHash, nickname string) error {
	_, err := d.Exec("UPDATE users SET login_id=?, password_hash=?, nickname=? WHERE id=?", loginID, passwordHash, nickname, userID)
	return err
}

func UpdateNickname(d *pdb.DB, userID int64, nickname string) error {
	_, err := d.Exec("UPDATE users SET nickname=? WHERE id=?", nickname, userID)
	return err
}

func UpdateNicknameAndPicture(d *pdb.DB, userID int64, nickname, picture string) error {
	_, err := d.Exec("UPDATE users SET nickname=?, picture=? WHERE id=?", nickname, picture, userID)
	return err
}

func UpdateEmail(d *pdb.DB, userID int64, email string) (int64, error) {
	res, err := d.Exec("UPDATE users SET email=? WHERE id=?", email, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func GetPicture(d *pdb.DB, userID int64) (sql.NullString, error) {
	var picture sql.NullString
	err := d.QueryRow("SELECT picture FROM users WHERE id=?", userID).Scan(&picture)
	if err == sql.ErrNoRows {
		return picture, nil
	}
	return picture, err
}

func LinkDiscord(d *pdb.DB, userID int64, discordID, discordUsername string) error {
	_, err := d.Exec("UPDATE users SET discord_id=?, discord_username=? WHERE id=?", discordID, discordUsername, userID)
	return err
}

func ExistsLoginID(d *pdb.DB, loginID string, excludeUserID int64) (bool, error) {
	var id int64
	var err error
	if excludeUserID > 0 {
		err = d.QueryRow("SELECT id FROM users WHERE login_id = ? AND id != ?", loginID, excludeUserID).Scan(&id)
	} else {
		err = d.QueryRow("SELECT id FROM users WHERE login_id = ?", loginID).Scan(&id)
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
