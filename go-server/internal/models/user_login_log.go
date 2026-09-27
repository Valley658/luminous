package models

import (
	"strconv"

	pdb "pastellive/internal/db"
)

// user_login_log: 관리자 "회원 관리" 탭에서 "회원별 접속 IP 이력"을 보여주기
// 위한 로그. users.last_ip는 최근 값 하나만 덮어쓰기 때문에, 언제 어떤 IP로
// 로그인했었는지 전체 이력을 보려면 별도 테이블이 필요해서 추가함.
// UpdateLastIP/UpdateLastIPAndPasswordHash(로그인 성공 시 항상 호출됨)에서
// 같이 기록한다.
func InitUserLoginLogTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS user_login_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		ip_address VARCHAR(45) NULL,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_user_login_log_user_id ON user_login_log(user_id, created_at)`)
	return nil
}

func LogUserLoginIP(d *pdb.DB, userID int64, ip string) error {
	if ip == "" {
		return nil
	}
	if d.Backend == "mysql" {
		_, err := d.Exec("INSERT INTO user_login_log (user_id, ip_address) VALUES (?, ?)", userID, ip)
		return err
	}
	_, err := d.Exec("INSERT INTO user_login_log (user_id, ip_address, created_at) VALUES (?, ?, datetime('now','localtime'))", userID, ip)
	return err
}

type UserLoginLogEntry struct {
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
}

// GetUserLoginHistory: 이 유저의 최근 로그인 IP 이력(최신순, 최대 limit개).
func GetUserLoginHistory(d *pdb.DB, userID int64, limit int) ([]UserLoginLogEntry, error) {
	rows, err := d.Query("SELECT ip_address, created_at FROM user_login_log WHERE user_id = ? ORDER BY created_at DESC LIMIT ?", userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserLoginLogEntry
	for rows.Next() {
		var e UserLoginLogEntry
		var ip, ca *string
		if err := rows.Scan(&ip, &ca); err == nil {
			if ip != nil {
				e.IP = *ip
			}
			if ca != nil {
				e.CreatedAt = *ca
			}
			out = append(out, e)
		}
	}
	if out == nil {
		out = []UserLoginLogEntry{}
	}
	return out, rows.Err()
}

// BackfillUserLoginLogFromLastIP: 이 기능(user_login_log)이 생기기 전부터
// 로그인해있던 회원들은 세션 쿠키로 계속 로그인 상태를 유지하고 있어서
// 실제로 "다시 로그인"하기 전까진 이력이 하나도 안 쌓인다 - 그 사이엔 관리자
// 화면에 "접속 기록이 없습니다"만 뜨게 되어 혼란스러우므로, users.last_ip에
// 이미 저장돼 있던 값으로 이력 1건을 소급 생성해준다. user_login_log에 해당
// 유저 행이 하나도 없을 때만 채우는 멱등 작업이라 서버 재시작마다 실행해도
// 안전하다.
func BackfillUserLoginLogFromLastIP(d *pdb.DB) error {
	_, err := d.Exec(`INSERT INTO user_login_log (user_id, ip_address, created_at)
		SELECT u.id, u.last_ip, COALESCE(u.updated_at, u.created_at)
		FROM users u
		WHERE u.last_ip IS NOT NULL AND u.last_ip != ''
		AND NOT EXISTS (SELECT 1 FROM user_login_log l WHERE l.user_id = u.id)`)
	return err
}

func DeleteOldUserLoginLog(d *pdb.DB, retainDays int) (int64, error) {
	cutoffExpr := "datetime('now','localtime','-" + strconv.Itoa(retainDays) + " days')"
	if d.Backend == "mysql" {
		cutoffExpr = "DATE_SUB(NOW(), INTERVAL " + strconv.Itoa(retainDays) + " DAY)"
	}
	res, err := d.Exec("DELETE FROM user_login_log WHERE created_at < " + cutoffExpr)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
