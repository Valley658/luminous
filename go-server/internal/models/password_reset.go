package models

import (
	crand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	pdb "pastellive/internal/db"
)

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

// password_reset_tokens: 비밀번호 재설정 이메일에 담아 보내는 토큰을 저장한다.
// 원문 토큰 자체는 DB에 절대 저장하지 않고 SHA-256 해시만 저장한다(세션
// 토큰과 같은 이유 - DB가 유출돼도 토큰을 역산해서 남의 비밀번호를 바꿀 수
// 없게). 토큰은 30분 뒤 만료되고, 한 번 쓰면 used_at이 찍혀서 재사용이
// 막힌다.
func InitPasswordResetTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id INT PRIMARY KEY AUTO_INCREMENT,
			user_id INT NOT NULL,
			token_hash CHAR(64) NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			used_at TIMESTAMP NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_password_reset_token_hash (token_hash),
			INDEX idx_password_reset_user (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
		return err
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS password_reset_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		token_hash CHAR(64) NOT NULL,
		expires_at DATETIME NOT NULL,
		used_at DATETIME,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`); err != nil {
		return err
	}
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_password_reset_token_hash ON password_reset_tokens (token_hash)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_password_reset_user ON password_reset_tokens (user_id)`)
	return nil
}

func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreatePasswordResetToken은 새 토큰을 만들어 저장하고 원문 토큰을 돌려준다
// (원문은 메일 본문에만 쓰이고 DB엔 해시만 남는다). 같은 계정에 대해 예전에
// 발급된, 아직 안 쓴 토큰들은 먼저 전부 무효화한다 - 재설정 메일을 여러 번
// 요청했을 때 옛날 메일 속 링크가 계속 살아있는 걸 막기 위함.
func CreatePasswordResetToken(d *pdb.DB, userID int64, ttl time.Duration) (token string, err error) {
	if _, err := d.Exec("UPDATE password_reset_tokens SET used_at = "+d.NowExpr()+" WHERE user_id = ? AND used_at IS NULL", userID); err != nil {
		return "", err
	}
	token = randomToken(32)
	tokenHash := hashResetToken(token)
	expiresAt := time.Now().Add(ttl)
	_, err = d.Exec(
		"INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)",
		userID, tokenHash, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

type passwordResetRow struct {
	ID        int64
	UserID    int64
	ExpiresAt time.Time
	UsedAt    sql.NullTime
}

// ConsumePasswordResetToken은 토큰이 유효하면(존재+안 만료+아직 안 씀) 그
// 자리에서 바로 used_at을 찍어 소모시키고 대상 user_id를 돌려준다. 유효하지
// 않으면 (0, false, nil)을 돌려준다 - 호출하는 쪽에서 "링크가 만료되었거나
// 이미 사용됨" 한 가지 메시지로만 안내하면 되게(어느 쪽인지 굳이 구분해서
// 공격자에게 정보를 더 주지 않기 위함).
func ConsumePasswordResetToken(d *pdb.DB, token string) (userID int64, ok bool, err error) {
	tokenHash := hashResetToken(token)
	row := d.QueryRow(
		"SELECT id, user_id, expires_at, used_at FROM password_reset_tokens WHERE token_hash = ?",
		tokenHash,
	)
	var rec passwordResetRow
	if err := row.Scan(&rec.ID, &rec.UserID, &rec.ExpiresAt, &rec.UsedAt); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	if rec.UsedAt.Valid || time.Now().After(rec.ExpiresAt) {
		return 0, false, nil
	}
	if _, err := d.Exec("UPDATE password_reset_tokens SET used_at = "+d.NowExpr()+" WHERE id = ?", rec.ID); err != nil {
		return 0, false, err
	}
	return rec.UserID, true, nil
}

// DeleteExpiredPasswordResetTokens는 백그라운드 정리용(스케줄러가 주기적으로
// 호출) - 만료된 지 오래된 토큰 행을 지워서 테이블이 무한히 커지지 않게 한다.
func DeleteExpiredPasswordResetTokens(d *pdb.DB) (int64, error) {
	var res sql.Result
	var err error
	if d.Backend == "mysql" {
		res, err = d.Exec("DELETE FROM password_reset_tokens WHERE expires_at < DATE_SUB(NOW(), INTERVAL 7 DAY)")
	} else {
		res, err = d.Exec("DELETE FROM password_reset_tokens WHERE expires_at < datetime('now', '-7 days')")
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
