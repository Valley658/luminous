package models

import (
	pdb "pastellive/internal/db"
)

// InitMembersTable creates the `members` table, which the video-pool
// refresher, search, watch-page and per-member lookups all read from
// (member_name, channel_id, music_playlists / shorts_playlists /
// replay_playlists as JSON-encoded string arrays). There was never a Go
// migration for this table on any backend - it originally lived only in
// the (now-deleted) Python app's schema, which is why it silently went
// missing after a from-scratch DB. This also seeds it once with each
// StelLive member's real public YouTube channel ID (reconstructed from
// stellive.me/<member> pages and matched against the last surviving
// server logs from before the DB was wiped) so the site recovers on its
// own without anyone hand-typing 13 channel IDs.
func InitMembersTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS members (
			id INT PRIMARY KEY AUTO_INCREMENT,
			member_name VARCHAR(50) NOT NULL,
			channel_id VARCHAR(50) NULL,
			music_playlists TEXT NULL,
			shorts_playlists TEXT NULL,
			replay_playlists TEXT NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_members_member_name (member_name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`); err != nil {
			return err
		}
	} else {
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS members (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				member_name VARCHAR(50) NOT NULL UNIQUE,
				channel_id VARCHAR(50),
				music_playlists TEXT,
				shorts_playlists TEXT,
				replay_playlists TEXT,
				created_at DATETIME DEFAULT (datetime('now','localtime')),
				updated_at DATETIME DEFAULT (datetime('now','localtime'))
			)`,
		}
		for _, s := range stmts {
			_, _ = d.Exec(s)
		}
	}

	seedMysql := `INSERT INTO members (member_name, channel_id) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE member_name = member_name`
	seedSqlite := `INSERT OR IGNORE INTO members (member_name, channel_id) VALUES (?, ?)`

	// 2026-09 기준 stellive.me/talents 각 멤버 페이지의 공식 유튜브 채널
	// (DB가 비어있던 동안 남아있던 서버 로그의 target= 채널ID와 대조해 확인함).
	// 칸나는 졸업으로 채널이 없어 기존 관례상 placeholder를 채널ID로 사용
	// (video/pool.go 등에서 이 값을 만나면 건너뛰도록 이미 되어 있음).
	seed := []struct{ name, channelID string }{
		{"스텔라이브", "UC2b4WRE5BZ6SIUWBeJU8rwg"},
		{"칸나", "UC_KANNA_PLACEHOLDER"},
		{"유니", "UClbYIn9LDbbFZ9w2shX3K0g"},
		{"후야", "UC0YQnenKBCu5sGb7H61n6HA"},
		{"히나", "UC1afpiIuBDcjYlmruAa0HiA"},
		{"리제", "UC7-m6jQLinZQWIbwm9W-1iw"},
		{"마시로", "UC_eeSpMBz8PG4ssdBPnP07g"},
		{"타비", "UCAHVQ44O81aehLWfy9O6Elw"},
		{"시부키", "UCYxLMfeX1CbMBll9MsGlzmw"},
		{"린", "UCQmcltnre6aG9SkDRYZqFIg"},
		{"나나", "UCcA21_PzN1EhNe7xS4MJGsQ"},
		{"리코", "UCj0c1jUr91dTetIQP2pFeLA"},
		{"강지", "UCIVFv8AiQLqM9oLHTixrNYw"},
		{"김블루", "UCNzcxCN_Hh_lu5RCSFXKgGQ"},
	}
	q := seedSqlite
	if d.Backend == "mysql" {
		q = seedMysql
	}
	for _, m := range seed {
		_, _ = d.Exec(q, m.name, m.channelID)
	}
	return nil
}
