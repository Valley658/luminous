package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

// 반복 신고 사용자 자동 플래그. phash 도용 탐지가 "이 이미지, 전에 올라온
// 것과 똑같다"를 자동으로 잡아내듯, 이건 "이 사람 게시물이 신고로 자주
// 삭제된다"를 자동으로 잡아내서 관리자가 검토할 수 있게 표시만 해준다
// (자동으로 계정을 정지시키거나 하진 않음 - 오신고로 억울하게 당할 수도
// 있으니 최종 판단은 항상 관리자가 함).
func InitReportFlagsTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS user_report_flags (
			user_id INT PRIMARY KEY,
			nickname VARCHAR(50),
			strike_count INT NOT NULL DEFAULT 0,
			last_reason VARCHAR(200),
			status VARCHAR(20) NOT NULL DEFAULT 'open',
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`); err != nil {
			return err
		}
		return nil
	}
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS user_report_flags (
		user_id INTEGER PRIMARY KEY,
		nickname VARCHAR(50),
		strike_count INTEGER NOT NULL DEFAULT 0,
		last_reason VARCHAR(200),
		status VARCHAR(20) NOT NULL DEFAULT 'open',
		created_at DATETIME DEFAULT (datetime('now','localtime')),
		updated_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	return err
}

// reportStrikeThreshold - 신고로 자동삭제된 게시물이 이 개수를 넘으면
// (한 번이 아니라 "반복"이어야 하므로 2 이상) 계정을 자동 플래그한다.
const reportStrikeThreshold = 2

// IncrementUserReportStrike는 사용자의 신고 누적 카운트를 1 올리고, 이번에
// 새로 임계값을 넘겨서 막 플래그된 것이면 true를 돌려준다(알림/로그용).
func IncrementUserReportStrike(d *pdb.DB, userID int64, nickname, reason string) (strikeCount int64, justFlagged bool, err error) {
	if userID == 0 {
		return 0, false, nil
	}
	if d.Backend == "mysql" {
		if _, err = d.Exec(
			`INSERT INTO user_report_flags (user_id, nickname, strike_count, last_reason, status)
			 VALUES (?, ?, 1, ?, 'open')
			 ON DUPLICATE KEY UPDATE strike_count = strike_count + 1, nickname = VALUES(nickname),
				last_reason = VALUES(last_reason), status = IF(status = 'resolved', 'open', status)`,
			userID, nickname, reason,
		); err != nil {
			return 0, false, err
		}
	} else {
		if _, err = d.Exec(
			`INSERT INTO user_report_flags (user_id, nickname, strike_count, last_reason, status)
			 VALUES (?, ?, 1, ?, 'open')
			 ON CONFLICT(user_id) DO UPDATE SET strike_count = strike_count + 1, nickname = excluded.nickname,
				last_reason = excluded.last_reason, status = CASE WHEN status = 'resolved' THEN 'open' ELSE status END`,
			userID, nickname, reason,
		); err != nil {
			return 0, false, err
		}
	}
	if err = d.QueryRow("SELECT strike_count FROM user_report_flags WHERE user_id = ?", userID).Scan(&strikeCount); err != nil {
		return 0, false, err
	}
	return strikeCount, strikeCount == reportStrikeThreshold, nil
}

type ReportFlagRow struct {
	UserID      int64
	Nickname    sql.NullString
	StrikeCount int64
	LastReason  sql.NullString
	Status      string
	UpdatedAt   string
}

func (r ReportFlagRow) ToMap() map[string]any {
	return map[string]any{
		"user_id": r.UserID, "nickname": m2s(r.Nickname), "strike_count": r.StrikeCount,
		"last_reason": m2s(r.LastReason), "status": r.Status, "updated_at": r.UpdatedAt,
	}
}

// ListReportFlags는 신고 누적으로 자동 플래그된 사용자 목록(관리자용)을
// strike_count가 임계값 이상인 것만 돌려준다.
func ListReportFlags(d *pdb.DB, statusFilter string) ([]ReportFlagRow, error) {
	dateExpr := sqlDtFmt(d, "updated_at")
	var rows *sql.Rows
	var err error
	if statusFilter != "" && statusFilter != "all" {
		rows, err = d.Query(
			"SELECT user_id, nickname, strike_count, last_reason, status, "+dateExpr+" as updated_at "+
				"FROM user_report_flags WHERE strike_count >= ? AND status = ? ORDER BY strike_count DESC, updated_at DESC LIMIT 200",
			reportStrikeThreshold, statusFilter,
		)
	} else {
		rows, err = d.Query(
			"SELECT user_id, nickname, strike_count, last_reason, status, "+dateExpr+" as updated_at "+
				"FROM user_report_flags WHERE strike_count >= ? ORDER BY strike_count DESC, updated_at DESC LIMIT 200",
			reportStrikeThreshold,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ReportFlagRow, 0)
	for rows.Next() {
		var row ReportFlagRow
		if err := rows.Scan(&row.UserID, &row.Nickname, &row.StrikeCount, &row.LastReason, &row.Status, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.UpdatedAt = formatDateShort(row.UpdatedAt)
		out = append(out, row)
	}
	return out, rows.Err()
}

func ResolveReportFlag(d *pdb.DB, userID int64) (int64, error) {
	res, err := d.Exec("UPDATE user_report_flags SET status = 'resolved' WHERE user_id = ?", userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
