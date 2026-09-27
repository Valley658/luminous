package models

import (
	pdb "pastellive/internal/db"
)

// InitUserModerationColumns: 회원 관리 탭 확장(스태프 표시, 정지)에 쓰는
// users 테이블 컬럼들. is_staff/staff_role은 관리자 페이지 접근 권한을 주는
// 게 아니라(그 권한은 isAdmin()이 owner 계정 하나로 하드코딩돼 있음, 여기서
// 안 건드림) 회원 목록에 "운영진" 배지를 표시하기 위한 라벨일 뿐이다.
func InitUserModerationColumns(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	for _, s := range []string{
		`ALTER TABLE users ADD COLUMN is_staff INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN staff_role VARCHAR(50)`,
		`ALTER TABLE users ADD COLUMN suspended INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN suspended_reason VARCHAR(255)`,
		`ALTER TABLE users ADD COLUMN suspended_at DATETIME`,
	} {
		_, _ = d.Exec(s)
	}
	return nil
}

func SetUserStaff(d *pdb.DB, userID int64, isStaff bool, staffRole string) error {
	v := 0
	if isStaff {
		v = 1
	}
	_, err := d.Exec("UPDATE users SET is_staff=?, staff_role=? WHERE id=?", v, staffRole, userID)
	return err
}

func SuspendUser(d *pdb.DB, userID int64, reason string) error {
	if d.Backend == "mysql" {
		_, err := d.Exec("UPDATE users SET suspended=1, suspended_reason=?, suspended_at=NOW() WHERE id=?", reason, userID)
		return err
	}
	_, err2 := d.Exec("UPDATE users SET suspended=1, suspended_reason=?, suspended_at=datetime('now','localtime') WHERE id=?", reason, userID)
	return err2
}

func UnsuspendUser(d *pdb.DB, userID int64) error {
	_, err := d.Exec("UPDATE users SET suspended=0, suspended_reason=NULL, suspended_at=NULL WHERE id=?", userID)
	return err
}

type UserSuspensionStatus struct {
	Suspended bool
	Reason    string
}

func GetUserSuspensionStatus(d *pdb.DB, userID int64) (UserSuspensionStatus, error) {
	var suspended int
	var reason *string
	err := d.QueryRow("SELECT suspended, suspended_reason FROM users WHERE id=?", userID).Scan(&suspended, &reason)
	if err != nil {
		return UserSuspensionStatus{}, err
	}
	out := UserSuspensionStatus{Suspended: suspended != 0}
	if reason != nil {
		out.Reason = *reason
	}
	return out, nil
}

type UserActivitySummary struct {
	FanartCount    int `json:"fanart_count"`
	CommentCount   int `json:"comment_count"`
	PostCount      int `json:"post_count"`
	ReportedCount  int `json:"reported_count"`
	AttendanceDays int `json:"attendance_days"`
	Points         int `json:"points"`
	LoginCount     int `json:"login_count"`
}

// GetUserActivitySummary: 회원 상세 패널용 - 이 유저가 사이트에서 뭘 했는지
// 한 화면에 보이는 요약. 각 카운트 쿼리는 실패해도(테이블/컬럼 문제 등)
// 전체를 막지 않고 0으로 둔다.
func GetUserActivitySummary(d *pdb.DB, userID int64) UserActivitySummary {
	var s UserActivitySummary
	_ = d.QueryRow("SELECT COUNT(*) FROM fanart_gallery WHERE user_id=?", userID).Scan(&s.FanartCount)
	_ = d.QueryRow("SELECT COUNT(*) FROM video_comments WHERE user_id=?", userID).Scan(&s.CommentCount)
	_ = d.QueryRow("SELECT COUNT(*) FROM community_posts WHERE user_id=?", userID).Scan(&s.PostCount)
	_ = d.QueryRow("SELECT COUNT(*) FROM fanart_reports WHERE reporter_user_id=?", userID).Scan(&s.ReportedCount)
	_ = d.QueryRow("SELECT COUNT(*) FROM user_attendance WHERE user_id=?", userID).Scan(&s.AttendanceDays)
	_ = d.QueryRow("SELECT COALESCE(points,0) FROM user_points WHERE user_id=?", userID).Scan(&s.Points)
	_ = d.QueryRow("SELECT COUNT(*) FROM user_login_log WHERE user_id=?", userID).Scan(&s.LoginCount)
	return s
}
