package models

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	pdb "pastellive/internal/db"
)

// InitEmailDigestColumn은 users 테이블에 주간 다이제스트 수신거부 여부를
// 저장할 컬럼을 추가한다(이미 있으면 에러 무시 - 다른 Init 함수들과 동일한
// 패턴). 기본값 0(수신) - 이메일을 등록한 사용자는 이미 비밀번호 재설정 등
// 사이트 운영 메일을 받는 상태라, 주간 인기글 요약도 옵트아웃 방식으로 둔다.
// 메일 맨 아래에 항상 로그인 없이 클릭 한 번으로 되는 수신거부 링크를 넣는다.
func InitEmailDigestColumn(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, _ = d.Exec(`ALTER TABLE users ADD COLUMN email_digest_unsubscribed TINYINT DEFAULT 0`)
		return nil
	}
	_, _ = d.Exec(`ALTER TABLE users ADD COLUMN email_digest_unsubscribed INTEGER DEFAULT 0`)
	return nil
}

type DigestRecipient struct {
	UserID   int64
	Email    string
	Nickname string
}

// GetEmailDigestRecipients는 이메일을 등록했고 수신거부하지 않은 사용자
// 목록을 돌려준다.
func GetEmailDigestRecipients(d *pdb.DB) ([]DigestRecipient, error) {
	rows, err := d.Query(
		`SELECT id, email, COALESCE(nickname, '') FROM users
		 WHERE email IS NOT NULL AND email <> '' AND COALESCE(email_digest_unsubscribed, 0) = 0`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DigestRecipient, 0)
	for rows.Next() {
		var r DigestRecipient
		if err := rows.Scan(&r.UserID, &r.Email, &r.Nickname); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetEmailDigestUnsubscribed는 다이제스트 수신거부 처리를 한다.
func SetEmailDigestUnsubscribed(d *pdb.DB, userID int64) error {
	_, err := d.Exec("UPDATE users SET email_digest_unsubscribed = 1 WHERE id = ?", userID)
	return err
}

// DigestUnsubscribeToken/VerifyDigestUnsubscribeToken - 이메일 속 수신거부
// 링크는 로그인 없이 눌려야 하니, 세션 대신 서버 SECRET_KEY로 서명한 토큰을
// 쓴다(위조 방지 - 다른 사람의 user_id를 넣어서 마음대로 수신거부시킬 수
// 없게). 링크가 하는 일이 "이메일 그만 보내기"뿐이라 토큰에 만료시간은 없음.
func DigestUnsubscribeToken(secretKey string, userID int64) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte("email_digest_unsubscribe:"))
	mac.Write([]byte(strconv.FormatInt(userID, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyDigestUnsubscribeToken(secretKey string, userID int64, token string) bool {
	expected := DigestUnsubscribeToken(secretKey, userID)
	return hmac.Equal([]byte(expected), []byte(token))
}

type DigestFanart struct {
	ID       int64
	Title    string
	ImageURL string
	Nickname string
	Likes    int64
}

// GetTopFanartThisWeek는 최근 7일 사이에 올라온 팬아트 중 좋아요가 많은
// 순으로 limit개를 돌려준다(주간 이메일 다이제스트용).
func GetTopFanartThisWeek(d *pdb.DB, limit int) ([]DigestFanart, error) {
	if limit <= 0 {
		limit = 5
	}
	weekAgo := KSTToday7DaysAgo()
	rows, err := d.Query(
		`SELECT g.id, COALESCE(g.title, ''), g.image_url, g.nickname, COUNT(r.id) as likes
		 FROM fanart_gallery g
		 LEFT JOIN fanart_reactions r ON r.fanart_id = g.id AND r.reaction_type = 'like'
		 WHERE g.status = 'active' AND g.created_at >= ?
		 GROUP BY g.id, g.title, g.image_url, g.nickname
		 ORDER BY likes DESC, g.id DESC
		 LIMIT ?`, weekAgo, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DigestFanart, 0)
	for rows.Next() {
		var f DigestFanart
		if err := rows.Scan(&f.ID, &f.Title, &f.ImageURL, &f.Nickname, &f.Likes); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type DigestPost struct {
	ID         int64
	MemberName string
	Title      string
	Content    string
	Nickname   string
}

// GetRecentCommunityPostsThisWeek는 최근 7일 사이 커뮤니티 게시글을 최신순
// limit개 돌려준다(좋아요 집계는 target_id가 문자열이라 백엔드별로 캐스팅이
// 달라 복잡해지므로, 다이제스트에서는 최신순으로 충분히 단순하게 감).
func GetRecentCommunityPostsThisWeek(d *pdb.DB, limit int) ([]DigestPost, error) {
	if limit <= 0 {
		limit = 5
	}
	weekAgo := KSTToday7DaysAgo()
	rows, err := d.Query(
		`SELECT id, member_name, COALESCE(title, ''), content, COALESCE(nickname, '스텔리언')
		 FROM community_posts WHERE created_at >= ? ORDER BY id DESC LIMIT ?`, weekAgo, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DigestPost, 0)
	for rows.Next() {
		var p DigestPost
		if err := rows.Scan(&p.ID, &p.MemberName, &p.Title, &p.Content, &p.Nickname); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// KSTToday7DaysAgo는 "YYYY-MM-DD" 형식으로 7일 전 날짜를 돌려준다(위 두 함수의
// 기간 필터용 - created_at 컬럼과 비교해도 자연스럽게 동작하도록 날짜 앞부분만
// 비교 가능한 형식을 씀).
func KSTToday7DaysAgo() string {
	return time.Now().In(KST).AddDate(0, 0, -7).Format("2006-01-02")
}
