package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

// 참여/재방문 유도용 포인트 시스템. 출석/팬아트 업로드/커뮤니티 글 작성 같은
// 행동에 점수를 주고, 랭킹(리더보드)으로 보여준다. 매번 SUM()을 돌리지 않게
// user_points에 누적 합계를 들고 있고(랭킹 조회가 빨라짐), points_log에는
// "누가 언제 무슨 행동으로 몇 점을 받았는지" 감사 기록을 남긴다(나중에 특정
// 행동의 점수 값을 바꾸거나, 이상 적립을 조사할 때 씀).
func InitPointsTables(d *pdb.DB) error {
	if d.Backend == "mysql" {
		if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS user_points (
			user_id INT PRIMARY KEY,
			points INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`); err != nil {
			return err
		}
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS points_log (
			id INT PRIMARY KEY AUTO_INCREMENT,
			user_id INT NOT NULL,
			action VARCHAR(40) NOT NULL,
			points INT NOT NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_points_log_user (user_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
		return err
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS user_points (
		user_id INTEGER PRIMARY KEY,
		points INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME DEFAULT (datetime('now','localtime'))
	)`); err != nil {
		return err
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS points_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		action VARCHAR(40) NOT NULL,
		points INTEGER NOT NULL,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`); err != nil {
		return err
	}
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_points_log_user ON points_log (user_id, created_at)`)
	return nil
}

// 행동별 점수 - 값을 바꾸고 싶으면 여기만 고치면 된다.
const (
	PointsAttendance     = 5  // 출석 체크
	PointsFanartUpload   = 10 // 팬아트 업로드
	PointsCommunityPost  = 3  // 커뮤니티 게시글 작성
	PointsComment        = 1  // 영상/커뮤니티 댓글 작성
	PointsAttendanceWeek = 20 // 7일 연속 출석 보너스(ConsecutiveAttendance % 7 == 0일 때 추가)
)

// 배지(뱃지) 기준 - 누적 포인트 구간별로 이름을 매긴다. 순서는 낮은 점수부터.
var pointBadges = []struct {
	minPoints int
	name      string
}{
	{0, "새싹 루미너"},
	{50, "단골 루미너"},
	{150, "열혈 루미너"},
	{400, "베테랑 루미너"},
	{1000, "명예의 전당"},
}

// BadgeForPoints는 주어진 포인트에 맞는 뱃지 이름을 돌려준다.
func BadgeForPoints(points int) string {
	name := pointBadges[0].name
	for _, b := range pointBadges {
		if points >= b.minPoints {
			name = b.name
		}
	}
	return name
}

// AwardPoints는 포인트를 적립하고 로그를 남긴다. userID가 0(비로그인)이면
// 아무것도 안 하고 조용히 리턴 - 호출부에서 매번 로그인 체크를 반복할 필요가
// 없게. 실패해도 에러를 무시하고 넘어가는 것이 보통(포인트 적립은 부가
// 기능이라, 이것 때문에 진짜 하려던 동작 - 출석 체크, 글 작성 등 -이 실패
// 처리되면 안 됨) - 그래서 호출부에서는 보통 에러를 로그만 남기고 무시한다.
func AwardPoints(d *pdb.DB, userID int64, action string, points int) error {
	if userID == 0 || points == 0 {
		return nil
	}
	if d.Backend == "mysql" {
		if _, err := d.Exec(
			`INSERT INTO user_points (user_id, points) VALUES (?, ?)
			 ON DUPLICATE KEY UPDATE points = points + VALUES(points)`,
			userID, points,
		); err != nil {
			return err
		}
	} else {
		if _, err := d.Exec(
			`INSERT INTO user_points (user_id, points) VALUES (?, ?)
			 ON CONFLICT(user_id) DO UPDATE SET points = points + excluded.points`,
			userID, points,
		); err != nil {
			return err
		}
	}
	_, err := d.Exec("INSERT INTO points_log (user_id, action, points) VALUES (?, ?, ?)", userID, action, points)
	return err
}

type LeaderboardEntry struct {
	Rank     int    `json:"rank"`
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Picture  string `json:"picture"`
	Points   int    `json:"points"`
	Badge    string `json:"badge"`
}

// GetLeaderboard는 포인트 상위 limit명을 돌려준다.
func GetLeaderboard(d *pdb.DB, limit int) ([]LeaderboardEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := d.Query(
		`SELECT u.id, COALESCE(u.nickname, ''), COALESCE(u.picture, ''), p.points
		 FROM user_points p JOIN users u ON u.id = p.user_id
		 WHERE p.points > 0
		 ORDER BY p.points DESC, p.user_id ASC
		 LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LeaderboardEntry
	rank := 0
	for rows.Next() {
		rank++
		var e LeaderboardEntry
		if err := rows.Scan(&e.UserID, &e.Nickname, &e.Picture, &e.Points); err != nil {
			return nil, err
		}
		e.Rank = rank
		e.Badge = BadgeForPoints(e.Points)
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetUserPoints는 "내 포인트/뱃지/순위" 표시용.
func GetUserPoints(d *pdb.DB, userID int64) (points int, badge string, rank int, err error) {
	err = d.QueryRow("SELECT points FROM user_points WHERE user_id = ?", userID).Scan(&points)
	if err == sql.ErrNoRows {
		points, err = 0, nil
	}
	if err != nil {
		return 0, "", 0, err
	}
	badge = BadgeForPoints(points)
	if points > 0 {
		if err2 := d.QueryRow("SELECT COUNT(*) + 1 FROM user_points WHERE points > ?", points).Scan(&rank); err2 != nil {
			rank = 0
		}
	}
	return points, badge, rank, nil
}
