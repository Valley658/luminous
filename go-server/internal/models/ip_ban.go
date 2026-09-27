package models

import (
	pdb "pastellive/internal/db"
)

// banned_ips: 관리자가 수동으로 차단한 IP 목록. rate limiter의 자동/일시
// 차단과 별개로, 관리자가 "이 IP는 영구 차단" 하고 싶을 때 쓰는 기능.
func InitIPBanTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS banned_ips (
		ip VARCHAR(45) PRIMARY KEY,
		reason VARCHAR(255) NULL,
		banned_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	return nil
}

type BannedIP struct {
	IP       string `json:"ip"`
	Reason   string `json:"reason"`
	BannedAt string `json:"banned_at"`
}

func ListBannedIPs(d *pdb.DB) ([]BannedIP, error) {
	rows, err := d.Query("SELECT ip, COALESCE(reason,''), banned_at FROM banned_ips ORDER BY banned_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BannedIP
	for rows.Next() {
		var b BannedIP
		if err := rows.Scan(&b.IP, &b.Reason, &b.BannedAt); err == nil {
			out = append(out, b)
		}
	}
	if out == nil {
		out = []BannedIP{}
	}
	return out, rows.Err()
}

func BanIP(d *pdb.DB, ip, reason string) error {
	if d.Backend == "mysql" {
		_, err := d.Exec("INSERT INTO banned_ips (ip, reason) VALUES (?, ?) ON DUPLICATE KEY UPDATE reason = VALUES(reason)", ip, reason)
		return err
	}
	_, err := d.Exec("INSERT INTO banned_ips (ip, reason) VALUES (?, ?) ON CONFLICT(ip) DO UPDATE SET reason = excluded.reason", ip, reason)
	return err
}

func UnbanIP(d *pdb.DB, ip string) error {
	_, err := d.Exec("DELETE FROM banned_ips WHERE ip = ?", ip)
	return err
}
