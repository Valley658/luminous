package models

import (
	"database/sql"
	"time"

	pdb "pastellive/internal/db"
)

func dateNDaysAgoExpr(d *pdb.DB, n int) string {
	if d.Backend == "mysql" {
		return "DATE_SUB(NOW(), INTERVAL " + itoa(n) + " DAY)"
	}
	return "date('now','localtime','-" + itoa(n) + " days')"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

type KeywordCount struct {
	Keyword string
	Count   int64
}

type DailyCount struct {
	Date  string
	Count int64
}

type StatusCount struct {
	Status string
	Count  int64
}

type MemberCount struct {
	MemberName string
	Count      int64
}

type AdminStatsOverview struct {
	TopKeywords     []KeywordCount
	FanartDaily     []DailyCount
	CommentsDaily   []DailyCount
	ReportsByStatus []StatusCount
	CheersByMember  []MemberCount
	TotalUsers      int64
	NewUsers14d     int64
	TotalFanart     int64
	TotalComments   int64
}

func GetAdminStatsOverview(d *pdb.DB) (*AdminStatsOverview, error) {
	out := &AdminStatsOverview{}

	rows, err := d.Query("SELECT keyword, search_count FROM search_trends ORDER BY search_count DESC LIMIT 10")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k KeywordCount
		if rows.Scan(&k.Keyword, &k.Count) == nil {
			out.TopKeywords = append(out.TopKeywords, k)
		}
	}
	rows.Close()

	fanartMap := make(map[string]int64)
	frows, err := d.Query("SELECT DATE(created_at) as d, COUNT(*) as c FROM fanart_gallery WHERE created_at >= " + dateNDaysAgoExpr(d, 13) + " GROUP BY DATE(created_at)")
	if err != nil {
		return nil, err
	}
	for frows.Next() {
		var day string
		var c int64
		if frows.Scan(&day, &c) == nil {
			fanartMap[day] = c
		}
	}
	frows.Close()

	commentsMap := make(map[string]int64)
	crows, err := d.Query("SELECT DATE(created_at) as d, COUNT(*) as c FROM video_comments WHERE created_at >= " + dateNDaysAgoExpr(d, 13) + " GROUP BY DATE(created_at)")
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var day string
		var c int64
		if crows.Scan(&day, &c) == nil {
			commentsMap[day] = c
		}
	}
	crows.Close()

	today := time.Now()
	for i := 13; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		out.FanartDaily = append(out.FanartDaily, DailyCount{Date: day, Count: fanartMap[day]})
		out.CommentsDaily = append(out.CommentsDaily, DailyCount{Date: day, Count: commentsMap[day]})
	}

	srows, err := d.Query("SELECT status, COUNT(*) as c FROM fanart_reports GROUP BY status")
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var s StatusCount
		if srows.Scan(&s.Status, &s.Count) == nil {
			out.ReportsByStatus = append(out.ReportsByStatus, s)
		}
	}
	srows.Close()

	mrows, err := d.Query("SELECT member_name, COUNT(*) as c FROM live_cheers WHERE created_at >= " + dateNDaysAgoExpr(d, 6) + " GROUP BY member_name ORDER BY c DESC LIMIT 10")
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var m MemberCount
		if mrows.Scan(&m.MemberName, &m.Count) == nil {
			out.CheersByMember = append(out.CheersByMember, m)
		}
	}
	mrows.Close()

	if err := d.QueryRow("SELECT COUNT(*) FROM users").Scan(&out.TotalUsers); err != nil {
		return nil, err
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM users WHERE created_at >= " + dateNDaysAgoExpr(d, 13)).Scan(&out.NewUsers14d); err != nil {
		return nil, err
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM fanart_gallery").Scan(&out.TotalFanart); err != nil {
		return nil, err
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM video_comments").Scan(&out.TotalComments); err != nil {
		return nil, err
	}

	if out.TopKeywords == nil {
		out.TopKeywords = []KeywordCount{}
	}
	if out.ReportsByStatus == nil {
		out.ReportsByStatus = []StatusCount{}
	}
	if out.CheersByMember == nil {
		out.CheersByMember = []MemberCount{}
	}
	return out, nil
}

type AdminUserRow struct {
	ID              int64
	Email           sql.NullString
	Nickname        sql.NullString
	Picture         sql.NullString
	LoginID         sql.NullString
	DiscordUsername sql.NullString
	ChannelID       sql.NullString
	LastIP          sql.NullString
	CreatedAt       sql.NullString
	UpdatedAt       sql.NullString
}

func (u AdminUserRow) ToMap() map[string]any {
	return map[string]any{
		"id": u.ID, "email": u.Email.String, "nickname": u.Nickname.String, "picture": u.Picture.String,
		"login_id": u.LoginID.String, "discord_username": u.DiscordUsername.String, "channel_id": u.ChannelID.String,
		"last_ip": u.LastIP.String, "created_at": u.CreatedAt.String, "updated_at": u.UpdatedAt.String,
	}
}

func ListAdminUsers(d *pdb.DB, q string, page, perPage int) (users []AdminUserRow, total int64, err error) {
	where := ""
	var args []any
	if q != "" {
		where = "WHERE email LIKE ? OR nickname LIKE ? OR login_id LIKE ? OR discord_username LIKE ?"
		like := "%" + q + "%"
		args = []any{like, like, like, like}
	}
	if err = d.QueryRow("SELECT COUNT(*) FROM users "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * perPage
	queryArgs := append(append([]any{}, args...), perPage, offset)
	rows, err := d.Query(
		"SELECT id, email, nickname, picture, login_id, discord_username, channel_id, last_ip, created_at, updated_at FROM users "+where+" ORDER BY id DESC LIMIT ? OFFSET ?",
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var u AdminUserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.Nickname, &u.Picture, &u.LoginID, &u.DiscordUsername, &u.ChannelID, &u.LastIP, &u.CreatedAt, &u.UpdatedAt); err == nil {
			users = append(users, u)
		}
	}
	if users == nil {
		users = []AdminUserRow{}
	}
	return users, total, rows.Err()
}

func GetUserBasicInfo(d *pdb.DB, userID int64) (email, nickname string, found bool, err error) {
	var e, n sql.NullString
	qerr := d.QueryRow("SELECT email, nickname FROM users WHERE id = ?", userID).Scan(&e, &n)
	if qerr == sql.ErrNoRows {
		return "", "", false, nil
	}
	if qerr != nil {
		return "", "", false, qerr
	}
	return e.String, n.String, true, nil
}

func AdminDeleteUser(d *pdb.DB, userID int64) error {
	stmts := []string{
		"DELETE FROM watch_history WHERE user_id = ?",
		"DELETE FROM user_bookmarks WHERE user_id = ?",
		"DELETE FROM user_playlists WHERE user_id = ?",
		"DELETE FROM comment_likes WHERE user_id = ?",
		"DELETE FROM video_comments WHERE user_id = ?",
		"DELETE FROM fanart_reactions WHERE user_id = ?",
		"DELETE FROM community_likes WHERE user_id = ?",
		// 아래 4개 테이블은 본인이 작성한 공개 콘텐츠(팬아트/게시물/댓글/신고)라
		// 통째로 지우는 대신 개인정보처리방침 3조대로 작성자 표시만 비식별화하고 유지함
		// (user_id 컬럼은 NOT NULL이라 값 자체는 남지만, 해당 계정은 이미 삭제되어
		// 프로필/닉네임을 다시 찾아올 수 없으므로 더 이상 개인 식별 정보가 아님).
		"UPDATE fanart_gallery SET nickname='탈퇴한 사용자', ip_address=NULL WHERE user_id = ?",
		"UPDATE fanart_comments SET nickname='탈퇴한 사용자', picture=NULL, ip_address=NULL WHERE user_id = ?",
		"UPDATE fanart_reports SET reporter_nickname='탈퇴한 사용자', ip_address=NULL WHERE reporter_user_id = ?",
		"UPDATE community_posts SET nickname='탈퇴한 사용자', picture=NULL, ip_address=NULL WHERE user_id = ?",
		"UPDATE community_comments SET nickname='탈퇴한 사용자', picture=NULL, ip_address=NULL WHERE user_id = ?",
		"DELETE FROM users WHERE id = ?",
	}
	for _, s := range stmts {
		if _, err := d.Exec(s, userID); err != nil {
			return err
		}
	}
	return nil
}

type AdminDBOverviewTable struct {
	Name  string
	Count *int64
}

type AdminDBOverviewGroup struct {
	Label  string
	Tables []AdminDBOverviewTable
}

var adminDBOverviewGroups = []struct {
	Label  string
	Tables []string
}{
	{"사용자", []string{"users", "user_playlists", "user_bookmarks", "user_attendance", "watch_history"}},
	{"팬아트", []string{"fanart_gallery", "fanart_comments", "fanart_reactions", "fanart_reports"}},
	{"댓글 · 채팅", []string{"video_comments", "comment_likes", "live_cheers"}},
	{"커뮤니티", []string{"community_posts", "community_comments", "community_likes"}},
	{"시스템 · 운영", []string{"search_trends", "member_schedules", "admin_audit_log"}},
}

func GetAdminDBOverview(d *pdb.DB) []AdminDBOverviewGroup {
	var groups []AdminDBOverviewGroup
	for _, g := range adminDBOverviewGroups {
		var tables []AdminDBOverviewTable
		for _, name := range g.Tables {
			var cnt int64
			err := d.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&cnt)
			if err != nil {
				tables = append(tables, AdminDBOverviewTable{Name: name, Count: nil})
				continue
			}
			c := cnt
			tables = append(tables, AdminDBOverviewTable{Name: name, Count: &c})
		}

		for i := 1; i < len(tables); i++ {
			for j := i; j > 0; j-- {
				a, b := tables[j-1], tables[j]
				swap := false
				if a.Count == nil && b.Count != nil {
					swap = true
				} else if a.Count != nil && b.Count != nil && *a.Count < *b.Count {
					swap = true
				}
				if swap {
					tables[j-1], tables[j] = tables[j], tables[j-1]
				} else {
					break
				}
			}
		}
		groups = append(groups, AdminDBOverviewGroup{Label: g.Label, Tables: tables})
	}
	return groups
}

type AdminAuditLogRow struct {
	ID            int64
	AdminEmail    sql.NullString
	AdminNickname sql.NullString
	Action        string
	TargetType    sql.NullString
	TargetID      sql.NullString
	Detail        sql.NullString
	IPAddress     sql.NullString
	Date          string
}

func GetAdminAuditLog(d *pdb.DB) ([]map[string]any, error) {
	query := "SELECT id, admin_email, admin_nickname, action, target_type, target_id, detail, ip_address, " +
		sqlDtFmtSeconds(d, "created_at") + " as date FROM admin_audit_log ORDER BY id DESC LIMIT 200"
	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var r AdminAuditLogRow
		if err := rows.Scan(&r.ID, &r.AdminEmail, &r.AdminNickname, &r.Action, &r.TargetType, &r.TargetID, &r.Detail, &r.IPAddress, &r.Date); err == nil {
			out = append(out, map[string]any{
				"id": r.ID, "admin_email": r.AdminEmail.String, "admin_nickname": r.AdminNickname.String,
				"action": r.Action, "target_type": r.TargetType.String, "target_id": r.TargetID.String,
				"detail": r.Detail.String, "ip_address": r.IPAddress.String, "date": normalizeSqliteDatetime(r.Date),
			})
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
}

func sqlDtFmtSeconds(d *pdb.DB, col string) string {
	if d.Backend == "mysql" {
		return "DATE_FORMAT(" + col + ", '%Y-%m-%d %H:%i:%s')"
	}
	return col
}

func normalizeSqliteDatetime(s string) string {
	return formatDateShortSeconds(s)
}

func formatDateShortSeconds(s string) string {
	if len(s) >= 20 && s[len(s)-1] == 'Z' {
		s = s[:len(s)-1]
	}
	if len(s) > 10 && s[10] == 'T' {
		s = s[:10] + " " + s[11:]
	}
	return s
}

type VideoCommentRow struct {
	ID        int64
	VideoID   string
	Nickname  sql.NullString
	Content   string
	CreatedAt string
	Picture   sql.NullString
}

func ListAllVideoComments(d *pdb.DB) ([]map[string]any, error) {
	rows, err := d.Query("SELECT id, video_id, nickname, content, created_at, picture FROM video_comments ORDER BY id DESC LIMIT 300")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var c VideoCommentRow
		if err := rows.Scan(&c.ID, &c.VideoID, &c.Nickname, &c.Content, &c.CreatedAt, &c.Picture); err == nil {
			out = append(out, map[string]any{
				"id": c.ID, "video_id": c.VideoID, "nickname": c.Nickname.String, "content": c.Content,
				"date": formatDateShort(c.CreatedAt), "picture": c.Picture.String,
			})
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
}

func DeleteVideoComments(d *pdb.DB, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := ""
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	_, err := d.Exec("DELETE FROM video_comments WHERE id IN ("+placeholders+")", args...)
	return err
}
