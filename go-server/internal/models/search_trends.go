package models

import (
	pdb "pastellive/internal/db"
)

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
	return n, nil
}
