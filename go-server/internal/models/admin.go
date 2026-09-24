package models

import pdb "pastellive/internal/db"

func InitAdminAuditLogTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS admin_audit_log (
			id INT PRIMARY KEY AUTO_INCREMENT,
			admin_email VARCHAR(255) NULL,
			admin_nickname VARCHAR(100) NULL,
			action VARCHAR(100) NOT NULL,
			target_type VARCHAR(50) NULL,
			target_id VARCHAR(100) NULL,
			detail TEXT NULL,
			ip_address VARCHAR(45) NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			KEY idx_created_at (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS admin_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			admin_email VARCHAR(255),
			admin_nickname VARCHAR(100),
			action VARCHAR(100) NOT NULL,
			target_type VARCHAR(50),
			target_id VARCHAR(100),
			detail TEXT,
			ip_address VARCHAR(45),
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_created_at ON admin_audit_log (created_at)`,
	}
	for _, s := range stmts {
		_, _ = d.Exec(s)
	}
	return nil
}

func InsertAdminAuditLog(d *pdb.DB, adminEmail, adminNickname, action, targetType, targetID, detail, ip string) error {
	_, err := d.Exec(
		"INSERT INTO admin_audit_log (admin_email, admin_nickname, action, target_type, target_id, detail, ip_address) VALUES (?, ?, ?, ?, ?, ?, ?)",
		nullIfEmpty(adminEmail), adminNickname, action, nullIfEmpty(targetType), nullIfEmpty(targetID), nullIfEmpty(detail), ip,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func InitLiveCheersTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS live_cheers (
			id INT PRIMARY KEY AUTO_INCREMENT,
			member_name VARCHAR(50) NOT NULL,
			nickname VARCHAR(50) NOT NULL,
			message TEXT NOT NULL,
			ip_address VARCHAR(45) NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
		return err
	}
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS live_cheers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		member_name VARCHAR(50) NOT NULL,
		nickname VARCHAR(50) NOT NULL,
		message TEXT NOT NULL,
		ip_address VARCHAR(45),
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)

	_, _ = d.Exec(`ALTER TABLE live_cheers ADD COLUMN ip_address VARCHAR(45)`)
	return nil
}
