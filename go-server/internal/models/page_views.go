package models

import (
	"strconv"

	pdb "pastellive/internal/db"
)

// page_views: "트래픽 검사" 대시보드(관리자 > 트래픽)를 위한 방문 로그.
// handlers.(*App).TrackPageView 미들웨어가 실제 페이지 GET 요청마다 한 줄씩
// 쌓는다 (API/정적 파일/SPA 내부 멤버 전환 클릭 등은 제외 - 서버가 보는
// URL 자체가 안 바뀌는 SPA 특성상 "멤버별 조회수"까지는 이 테이블만으로는
// 못 만들고, 실제로 서버 라우트가 따로 있는 페이지(홈/워치/커뮤니티/채널 등)
// 방문 추이·실시간 접속자 수 위주로 쓴다).
func InitPageViewsTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS page_views (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path VARCHAR(255) NOT NULL,
		ip_address VARCHAR(45) NULL,
		viewed_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_page_views_viewed_at ON page_views(viewed_at)`)
	return nil
}

func LogPageView(d *pdb.DB, path, ip string) error {
	if d.Backend == "mysql" {
		_, err := d.Exec("INSERT INTO page_views (path, ip_address) VALUES (?, ?)", path, ip)
		return err
	}
	_, err := d.Exec("INSERT INTO page_views (path, ip_address, viewed_at) VALUES (?, ?, datetime('now','localtime'))", path, ip)
	return err
}

// DeleteOldPageViews: 방문 로그가 무한정 쌓이지 않도록 보관 기간(일)이 지난
// 행을 지운다. CleanupStaleSearchTrends처럼 스케줄러가 24시간마다 돌린다.
func DeleteOldPageViews(d *pdb.DB, retainDays int) (int64, error) {
	cutoffExpr := "datetime('now','localtime','-" + strconv.Itoa(retainDays) + " days')"
	if d.Backend == "mysql" {
		cutoffExpr = "DATE_SUB(NOW(), INTERVAL " + strconv.Itoa(retainDays) + " DAY)"
	}
	res, err := d.Exec("DELETE FROM page_views WHERE viewed_at < " + cutoffExpr)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

type DailyTrafficPoint struct {
	Date     string `json:"date"`
	Views    int    `json:"views"`
	Visitors int    `json:"visitors"`
}

// DailyPageViews: 최근 days일간 일별 페이지뷰/순방문자(IP 기준) 추이.
func DailyPageViews(d *pdb.DB, days int) ([]DailyTrafficPoint, error) {
	dateExpr := "date(viewed_at)"
	cutoffExpr := "datetime('now','localtime','-" + strconv.Itoa(days) + " days')"
	if d.Backend == "mysql" {
		dateExpr = "DATE(viewed_at)"
		cutoffExpr = "DATE_SUB(NOW(), INTERVAL " + strconv.Itoa(days) + " DAY)"
	}
	rows, err := d.Query("SELECT " + dateExpr + " AS d, COUNT(*), COUNT(DISTINCT ip_address) FROM page_views WHERE viewed_at >= " + cutoffExpr + " GROUP BY d ORDER BY d")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyTrafficPoint
	for rows.Next() {
		var p DailyTrafficPoint
		if err := rows.Scan(&p.Date, &p.Views, &p.Visitors); err == nil {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []DailyTrafficPoint{}
	}
	return out, rows.Err()
}

type HourlyTrafficPoint struct {
	Hour  int `json:"hour"`
	Views int `json:"views"`
}

// HourlyPageViewsToday: 오늘(현지 시각 기준) 0~23시별 페이지뷰 수. 데이터가
// 없는 시간대도 0으로 채워서 반환한다 (프론트에서 24칸 막대 그래프로 씀).
func HourlyPageViewsToday(d *pdb.DB) ([]HourlyTrafficPoint, error) {
	hourExpr := "CAST(strftime('%H', viewed_at) AS INTEGER)"
	todayExpr := "date(viewed_at) = date('now','localtime')"
	if d.Backend == "mysql" {
		hourExpr = "HOUR(viewed_at)"
		todayExpr = "DATE(viewed_at) = CURDATE()"
	}
	rows, err := d.Query("SELECT " + hourExpr + " AS h, COUNT(*) FROM page_views WHERE " + todayExpr + " GROUP BY h ORDER BY h")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int]int, 24)
	for rows.Next() {
		var h, c int
		if rows.Scan(&h, &c) == nil {
			counts[h] = c
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]HourlyTrafficPoint, 24)
	for h := 0; h < 24; h++ {
		out[h] = HourlyTrafficPoint{Hour: h, Views: counts[h]}
	}
	return out, nil
}

// OnlineNowCount: 최근 windowMinutes분 이내에 페이지를 조회한 서로 다른 IP 수.
// 정확한 "동시 접속자 수"는 아니지만(로그인 세션이 아닌 IP 기준 근사치),
// 별도의 실시간 인프라 없이 저비용으로 계산 가능한 근사값으로 충분하다.
func OnlineNowCount(d *pdb.DB, windowMinutes int) (int, error) {
	cutoffExpr := "datetime('now','localtime','-" + strconv.Itoa(windowMinutes) + " minutes')"
	if d.Backend == "mysql" {
		cutoffExpr = "DATE_SUB(NOW(), INTERVAL " + strconv.Itoa(windowMinutes) + " MINUTE)"
	}
	var n int
	err := d.QueryRow("SELECT COUNT(DISTINCT ip_address) FROM page_views WHERE viewed_at >= " + cutoffExpr).Scan(&n)
	return n, err
}

type IPTrafficStat struct {
	IP        string `json:"ip"`
	Views     int    `json:"views"`
	Paths     int    `json:"paths"`
	LastSeen  string `json:"last_seen"`
	FirstSeen string `json:"first_seen"`
}

// TrafficByIPToday: 오늘 접속한 IP별 요청 수/방문 경로 수/최초·최근 접속
// 시각 - "이 IP에서 어떤 트래픽이 왔는지" 분석용 (와이어샤크처럼 패킷 단위는
// 아니지만, 페이지 요청 단위로는 동일한 정보).
func TrafficByIPToday(d *pdb.DB, limit int) ([]IPTrafficStat, error) {
	todayExpr := "date(viewed_at) = date('now','localtime')"
	if d.Backend == "mysql" {
		todayExpr = "DATE(viewed_at) = CURDATE()"
	}
	rows, err := d.Query("SELECT ip_address, COUNT(*) AS c, COUNT(DISTINCT path), MIN(viewed_at), MAX(viewed_at) "+
		"FROM page_views WHERE "+todayExpr+" AND ip_address IS NOT NULL AND ip_address != '' "+
		"GROUP BY ip_address ORDER BY c DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPTrafficStat
	for rows.Next() {
		var s IPTrafficStat
		if err := rows.Scan(&s.IP, &s.Views, &s.Paths, &s.FirstSeen, &s.LastSeen); err == nil {
			out = append(out, s)
		}
	}
	if out == nil {
		out = []IPTrafficStat{}
	}
	return out, rows.Err()
}

// RecentPageViewsForIP: 특정 IP가 오늘 실제로 어떤 경로들을 방문했는지
// 시간순으로 - IP별 상세(와이어샤크 스타일 드릴다운) 조회용.
func RecentPageViewsForIP(d *pdb.DB, ip string, limit int) ([]struct {
	Path     string `json:"path"`
	ViewedAt string `json:"viewed_at"`
}, error) {
	rows, err := d.Query("SELECT path, viewed_at FROM page_views WHERE ip_address = ? ORDER BY viewed_at DESC LIMIT ?", ip, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Path     string `json:"path"`
		ViewedAt string `json:"viewed_at"`
	}
	for rows.Next() {
		var p struct {
			Path     string `json:"path"`
			ViewedAt string `json:"viewed_at"`
		}
		if err := rows.Scan(&p.Path, &p.ViewedAt); err == nil {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []struct {
			Path     string `json:"path"`
			ViewedAt string `json:"viewed_at"`
		}{}
	}
	return out, rows.Err()
}

// TopPathsToday: 오늘 방문 많은 경로 TOP N (참고용 - 멤버별 랭킹은 SPA 특성상
// 여기서 안 나오고, 서버 라우트가 실제로 나뉘는 페이지들만 잡힌다).
func TopPathsToday(d *pdb.DB, limit int) ([]struct {
	Path  string `json:"path"`
	Views int    `json:"views"`
}, error) {
	todayExpr := "date(viewed_at) = date('now','localtime')"
	if d.Backend == "mysql" {
		todayExpr = "DATE(viewed_at) = CURDATE()"
	}
	rows, err := d.Query("SELECT path, COUNT(*) AS c FROM page_views WHERE "+todayExpr+" GROUP BY path ORDER BY c DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Path  string `json:"path"`
		Views int    `json:"views"`
	}
	for rows.Next() {
		var p struct {
			Path  string `json:"path"`
			Views int    `json:"views"`
		}
		if err := rows.Scan(&p.Path, &p.Views); err == nil {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []struct {
			Path  string `json:"path"`
			Views int    `json:"views"`
		}{}
	}
	return out, rows.Err()
}
