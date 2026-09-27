package models

import (
	"database/sql"
	"regexp"
	"time"

	pdb "pastellive/internal/db"
)

// [2026-09-27] handlers.junkSearchPattern과 같은 목적의 패턴을 여기서도 씀(순환
// import를 피하려고 handlers 패키지 걸 재사용하는 대신 복제함). 이미 DB에 쌓여있는
// SQLi 프로브 문자열들을 한 번 정리하기 위한 용도.
var junkKeywordPattern = regexp.MustCompile(`(?i)['"<>;` + "`" + `]|--|/\*|\bunion\b|\bselect\b|\binsert\b|\bdelete\b|\bdrop\b|\bscript\b|\balert\(|\bOR\b\s*['"]?\s*\d|\bAND\b\s*\d+\s*=\s*\d+|\bCASE\s+WHEN\b|\bTHEN\b|\bELSE\b|\bEND\b|\bCHAR\(|\bJSON\(|\bCAST\(|\bCONCAT\(|\bSLEEP\(|\bBENCHMARK\(|%2[27]|%3[bB]|\d+\s*=\s*\d+`)

func InitSearchTrendsTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS search_trends (
		keyword VARCHAR(100) PRIMARY KEY,
		search_count INT DEFAULT 1,
		last_searched DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	_, _ = d.Exec(`CREATE TRIGGER IF NOT EXISTS trg_search_trends_last_searched
		AFTER UPDATE ON search_trends
		FOR EACH ROW
		WHEN NEW.last_searched = OLD.last_searched
		BEGIN
			UPDATE search_trends SET last_searched = datetime('now','localtime') WHERE keyword = NEW.keyword;
		END`)
	return nil
}

func LogSearchTrend(d *pdb.DB, keyword string) error {
	if d.Backend == "mysql" {
		_, err := d.Exec("INSERT INTO search_trends (keyword, search_count) VALUES (?, 1) "+
			"ON DUPLICATE KEY UPDATE search_count = search_count + 1, last_searched = NOW()", keyword)
		return err
	}
	_, err := d.Exec("INSERT INTO search_trends (keyword, search_count) VALUES (?, 1) "+
		"ON CONFLICT(keyword) DO UPDATE SET search_count = search_count + 1, last_searched = datetime('now','localtime')", keyword)
	return err
}

// TrendingKeywordMinCount: 실시간 인기 검색어에 뜨려면 최소 이만큼은 검색되어야 함.
// 한 번 검색했다고 바로 "인기 검색어"에 뜨는 게 이상해서 생긴 문턱값.
const TrendingKeywordMinCount = 10

func GetTrendingKeywords(d *pdb.DB, limit int) ([]string, error) {
	rows, err := d.Query("SELECT keyword FROM search_trends WHERE search_count >= ? ORDER BY search_count DESC, last_searched DESC LIMIT ?", TrendingKeywordMinCount, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kw string
		if err := rows.Scan(&kw); err == nil && kw != "" {
			out = append(out, kw)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}

func DeleteSearchTrend(d *pdb.DB, keyword string) error {
	_, err := d.Exec("DELETE FROM search_trends WHERE keyword = ?", keyword)
	return err
}

// DeleteAllSearchTrends: 관리자 "전체 삭제" 버튼 - 인기 검색어 통계를
// 전부 초기화한다.
func DeleteAllSearchTrends(d *pdb.DB) error {
	_, err := d.Exec("DELETE FROM search_trends")
	return err
}

type SearchTrendEntry struct {
	Keyword      string `json:"keyword"`
	SearchCount  int    `json:"search_count"`
	LastSearched string `json:"last_searched"`
}

// ListSearchTrends: 관리자 대시보드에서 검색어를 검색/조회하기 위한 목록.
// q가 비어있으면 전체(검색 많은 순), 아니면 keyword LIKE 검색.
func ListSearchTrends(d *pdb.DB, q string, limit int) ([]SearchTrendEntry, error) {
	var rows *sql.Rows
	var err error
	if q == "" {
		rows, err = d.Query("SELECT keyword, search_count, last_searched FROM search_trends ORDER BY search_count DESC, last_searched DESC LIMIT ?", limit)
	} else {
		rows, err = d.Query("SELECT keyword, search_count, last_searched FROM search_trends WHERE keyword LIKE ? ORDER BY search_count DESC, last_searched DESC LIMIT ?", "%"+q+"%", limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchTrendEntry
	for rows.Next() {
		var e SearchTrendEntry
		var lastSearched any
		if err := rows.Scan(&e.Keyword, &e.SearchCount, &lastSearched); err == nil {
			if s, ok := lastSearched.(string); ok {
				e.LastSearched = s
			} else if t, ok := lastSearched.(time.Time); ok {
				e.LastSearched = t.Format("2006-01-02 15:04:05")
			}
			out = append(out, e)
		}
	}
	if out == nil {
		out = []SearchTrendEntry{}
	}
	return out, rows.Err()
}

func CleanupStaleSearchTrends(d *pdb.DB) (int64, error) {
	cutoffExpr := "datetime('now','localtime','-7 days')"
	if d.Backend == "mysql" {
		cutoffExpr = "DATE_SUB(NOW(), INTERVAL 7 DAY)"
	}
	res, err := d.Exec("DELETE FROM search_trends WHERE last_searched < " + cutoffExpr)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	if junkN, jerr := CleanupJunkSearchTrends(d); jerr == nil {
		n += junkN
	}
	return n, nil
}

// CleanupJunkSearchTrends: 이미 쌓여있는 SQLi 프로브성 "검색어" 행들을 지운다.
// 서버 재배포 후 주기 작업(24시간마다) 때 같이 돌아서 기존에 오염된 데이터도
// 자연스럽게 정리됨.
func CleanupJunkSearchTrends(d *pdb.DB) (int64, error) {
	rows, err := d.Query("SELECT keyword FROM search_trends")
	if err != nil {
		return 0, err
	}
	var junk []string
	for rows.Next() {
		var kw string
		if rows.Scan(&kw) == nil && junkKeywordPattern.MatchString(kw) {
			junk = append(junk, kw)
		}
	}
	rows.Close()
	var deleted int64
	for _, kw := range junk {
		if res, derr := d.Exec("DELETE FROM search_trends WHERE keyword = ?", kw); derr == nil {
			n, _ := res.RowsAffected()
			deleted += n
		}
	}
	return deleted, nil
}
