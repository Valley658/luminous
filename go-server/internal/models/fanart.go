package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

func InitFanartTables(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS fanart_gallery (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INT NOT NULL,
			nickname VARCHAR(50) NOT NULL,
			image_url TEXT NOT NULL,
			title VARCHAR(50),
			description TEXT,
			status VARCHAR(20) DEFAULT 'active',
			discord_message_id VARCHAR(100),
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE TABLE IF NOT EXISTS fanart_reactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fanart_id INT NOT NULL,
			user_id INT NOT NULL,
			reaction_type TEXT NOT NULL CHECK(reaction_type IN ('like', 'dislike', 'report')),
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS unique_fanart_user_reaction ON fanart_reactions (fanart_id, user_id, reaction_type)`,
		`CREATE TABLE IF NOT EXISTS fanart_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fanart_id INT NOT NULL,
			reporter_user_id INT,
			reporter_nickname VARCHAR(50),
			reason VARCHAR(100),
			fanart_title VARCHAR(50),
			fanart_image_url TEXT,
			fanart_author VARCHAR(50),
			status VARCHAR(20) DEFAULT 'pending',
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE TABLE IF NOT EXISTS fanart_comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fanart_id INT NOT NULL,
			user_id INT NOT NULL,
			nickname VARCHAR(50) NOT NULL,
			picture TEXT,
			content VARCHAR(500) NOT NULL,
			ip_address VARCHAR(45),
			created_at DATETIME DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fanart_comments_fanart_id ON fanart_comments (fanart_id)`,
	}
	for _, s := range stmts {
		_, _ = d.Exec(s)
	}

	for _, s := range []string{
		`ALTER TABLE fanart_gallery ADD COLUMN discord_message_id VARCHAR(100)`,
		`ALTER TABLE fanart_gallery ADD COLUMN title VARCHAR(50)`,
		`ALTER TABLE fanart_gallery ADD COLUMN description TEXT`,
		`ALTER TABLE fanart_gallery ADD COLUMN thumbnail_url TEXT`,
		`ALTER TABLE fanart_gallery ADD COLUMN lqip TEXT`,
	} {
		_, _ = d.Exec(s)
	}

	stmtsExtra := []string{
		`ALTER TABLE fanart_gallery ADD COLUMN ip_address VARCHAR(45)`,
		`ALTER TABLE fanart_reactions ADD COLUMN ip_address VARCHAR(45)`,
		`ALTER TABLE fanart_reports ADD COLUMN ip_address VARCHAR(45)`,
	}
	for _, s := range stmtsExtra {
		_, _ = d.Exec(s)
	}
	return nil
}

func InsertFanart(d *pdb.DB, userID int64, nickname, imageURL string, title, description sql.NullString, thumbnailURL, lqip, ip string) (int64, error) {
	var titleArg, descArg any
	if title.Valid {
		titleArg = title.String
	}
	if description.Valid {
		descArg = description.String
	}
	res, err := d.Exec(
		"INSERT INTO fanart_gallery (user_id, nickname, image_url, title, description, discord_message_id, thumbnail_url, lqip, ip_address) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		userID, nickname, imageURL, titleArg, descArg, nil, nullIfEmptyStr(thumbnailURL), nullIfEmptyStr(lqip), ip,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func nullIfEmptyStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func GetFanartOwner(d *pdb.DB, fanartID int64) (userID int64, found bool, err error) {
	qerr := d.QueryRow("SELECT user_id FROM fanart_gallery WHERE id = ?", fanartID).Scan(&userID)
	if qerr == sql.ErrNoRows {
		return 0, false, nil
	}
	if qerr != nil {
		return 0, false, qerr
	}
	return userID, true, nil
}

func UpdateFanart(d *pdb.DB, fanartID int64, title, description sql.NullString) error {
	var titleArg, descArg any
	if title.Valid {
		titleArg = title.String
	}
	if description.Valid {
		descArg = description.String
	}
	_, err := d.Exec("UPDATE fanart_gallery SET title = ?, description = ? WHERE id = ?", titleArg, descArg, fanartID)
	return err
}

func FanartImageURL(d *pdb.DB, fanartID int64) (string, bool, error) {
	var url string
	qerr := d.QueryRow("SELECT image_url FROM fanart_gallery WHERE id = ?", fanartID).Scan(&url)
	if qerr == sql.ErrNoRows {
		return "", false, nil
	}
	if qerr != nil {
		return "", false, qerr
	}
	return url, true, nil
}

func DeleteFanartFully(d *pdb.DB, fanartID int64) error {
	if _, err := d.Exec("DELETE FROM fanart_gallery WHERE id = ?", fanartID); err != nil {
		return err
	}
	if _, err := d.Exec("DELETE FROM fanart_reactions WHERE fanart_id = ?", fanartID); err != nil {
		return err
	}
	_, err := d.Exec("DELETE FROM fanart_comments WHERE fanart_id = ?", fanartID)
	return err
}

func FanartReactionExists(d *pdb.DB, fanartID, userID int64, reaction string) (bool, error) {
	var id int64
	qerr := d.QueryRow("SELECT id FROM fanart_reactions WHERE fanart_id=? AND user_id=? AND reaction_type=?", fanartID, userID, reaction).Scan(&id)
	if qerr == sql.ErrNoRows {
		return false, nil
	}
	if qerr != nil {
		return false, qerr
	}
	return true, nil
}

func InsertFanartReaction(d *pdb.DB, fanartID, userID int64, reaction, ip string) error {
	_, err := d.Exec("INSERT INTO fanart_reactions (fanart_id, user_id, reaction_type, ip_address) VALUES (?, ?, ?, ?)", fanartID, userID, reaction, ip)
	return err
}

type FanartMeta struct {
	Title    sql.NullString
	ImageURL sql.NullString
	Nickname sql.NullString
}

func GetFanartMeta(d *pdb.DB, fanartID int64) (FanartMeta, bool, error) {
	var m FanartMeta
	qerr := d.QueryRow("SELECT title, image_url, nickname FROM fanart_gallery WHERE id = ?", fanartID).Scan(&m.Title, &m.ImageURL, &m.Nickname)
	if qerr == sql.ErrNoRows {
		return m, false, nil
	}
	if qerr != nil {
		return m, false, qerr
	}
	return m, true, nil
}

func InsertFanartReport(d *pdb.DB, fanartID int64, reporterUserID int64, reporterNickname, reason string, meta FanartMeta, ip string) error {
	_, err := d.Exec(
		"INSERT INTO fanart_reports (fanart_id, reporter_user_id, reporter_nickname, reason, fanart_title, fanart_image_url, fanart_author, ip_address) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		fanartID, reporterUserID, reporterNickname, reason, m2s(meta.Title), m2s(meta.ImageURL), m2s(meta.Nickname), ip,
	)
	return err
}

func m2s(n sql.NullString) any {
	if n.Valid {
		return n.String
	}
	return nil
}

type FanartReportRow struct {
	ID               int64
	FanartID         int64
	ReporterNickname sql.NullString
	Reason           sql.NullString
	FanartTitle      sql.NullString
	FanartImageURL   sql.NullString
	FanartAuthor     sql.NullString
	Status           sql.NullString
	Date             string
	FanartAlive      bool
}

func (r FanartReportRow) ToMap() map[string]any {
	return map[string]any{
		"id": r.ID, "fanart_id": r.FanartID,
		"reporter_nickname": m2s(r.ReporterNickname), "reason": m2s(r.Reason),
		"fanart_title": m2s(r.FanartTitle), "fanart_image_url": m2s(r.FanartImageURL),
		"fanart_author": m2s(r.FanartAuthor), "status": m2s(r.Status),
		"date": r.Date, "fanart_alive": r.FanartAlive,
	}
}

func ListFanartReports(d *pdb.DB, statusFilter string) ([]FanartReportRow, error) {
	dateExpr := sqlDtFmt(d, "created_at")
	var rows *sql.Rows
	var err error
	if statusFilter != "" && statusFilter != "all" {
		rows, err = d.Query(
			"SELECT id, fanart_id, reporter_nickname, reason, fanart_title, fanart_image_url, fanart_author, status, "+dateExpr+" as date "+
				"FROM fanart_reports WHERE status = ? ORDER BY id DESC LIMIT 200", statusFilter)
	} else {
		rows, err = d.Query(
			"SELECT id, fanart_id, reporter_nickname, reason, fanart_title, fanart_image_url, fanart_author, status, " + dateExpr + " as date " +
				"FROM fanart_reports ORDER BY id DESC LIMIT 200")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]FanartReportRow, 0)
	fanartIDs := make(map[int64]bool)
	for rows.Next() {
		var row FanartReportRow
		var date string
		if err := rows.Scan(&row.ID, &row.FanartID, &row.ReporterNickname, &row.Reason, &row.FanartTitle, &row.FanartImageURL, &row.FanartAuthor, &row.Status, &date); err != nil {
			return nil, err
		}
		row.Date = formatDateShort(date)
		out = append(out, row)
		fanartIDs[row.FanartID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(fanartIDs) == 0 {
		return out, nil
	}

	ids := make([]any, 0, len(fanartIDs))
	placeholders := ""
	for id := range fanartIDs {
		if placeholders != "" {
			placeholders += ","
		}
		placeholders += "?"
		ids = append(ids, id)
	}
	aliveRows, err := d.Query("SELECT id FROM fanart_gallery WHERE id IN ("+placeholders+")", ids...)
	if err != nil {
		return nil, err
	}
	defer aliveRows.Close()
	alive := make(map[int64]bool)
	for aliveRows.Next() {
		var id int64
		if err := aliveRows.Scan(&id); err != nil {
			return nil, err
		}
		alive[id] = true
	}
	for i := range out {
		out[i].FanartAlive = alive[out[i].FanartID]
	}
	return out, nil
}

func ResolveFanartReport(d *pdb.DB, reportID int64) (int64, error) {
	res, err := d.Exec("UPDATE fanart_reports SET status = 'resolved' WHERE id = ?", reportID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func CountFanartReports(d *pdb.DB, fanartID int64) (int64, error) {
	var cnt int64
	err := d.QueryRow("SELECT COUNT(*) FROM fanart_reactions WHERE fanart_id=? AND reaction_type='report'", fanartID).Scan(&cnt)
	return cnt, err
}

func MarkFanartReportsAutoDeleted(d *pdb.DB, fanartID int64) error {
	_, err := d.Exec("UPDATE fanart_reports SET status = 'auto_deleted' WHERE fanart_id = ?", fanartID)
	return err
}

type FanartComment struct {
	ID        int64
	UserID    sql.NullInt64
	Nickname  sql.NullString
	Picture   sql.NullString
	Content   string
	CreatedAt string
}

func GetFanartComments(d *pdb.DB, fanartID int64) ([]FanartComment, error) {
	rows, err := d.Query("SELECT id, user_id, nickname, picture, content, created_at FROM fanart_comments WHERE fanart_id = ? ORDER BY id ASC", fanartID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FanartComment
	for rows.Next() {
		var c FanartComment
		if err := rows.Scan(&c.ID, &c.UserID, &c.Nickname, &c.Picture, &c.Content, &c.CreatedAt); err == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func FanartExists(d *pdb.DB, fanartID int64) (bool, error) {
	var id int64
	qerr := d.QueryRow("SELECT id FROM fanart_gallery WHERE id = ?", fanartID).Scan(&id)
	if qerr == sql.ErrNoRows {
		return false, nil
	}
	if qerr != nil {
		return false, qerr
	}
	return true, nil
}

func AddFanartComment(d *pdb.DB, fanartID, userID int64, nickname, picture, content, ip string) (int64, error) {
	res, err := d.Exec(
		"INSERT INTO fanart_comments (fanart_id, user_id, nickname, picture, content, ip_address) VALUES (?, ?, ?, ?, ?, ?)",
		fanartID, userID, nickname, picture, content, ip,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetFanartCommentOwner(d *pdb.DB, commentID int64) (userID int64, found bool, err error) {
	qerr := d.QueryRow("SELECT user_id FROM fanart_comments WHERE id = ?", commentID).Scan(&userID)
	if qerr == sql.ErrNoRows {
		return 0, false, nil
	}
	if qerr != nil {
		return 0, false, qerr
	}
	return userID, true, nil
}

func DeleteFanartComment(d *pdb.DB, commentID int64) error {
	_, err := d.Exec("DELETE FROM fanart_comments WHERE id = ?", commentID)
	return err
}

type FanartRow struct {
	ID           int64
	UserID       sql.NullInt64
	Nickname     sql.NullString
	ImageURL     string
	ThumbnailURL sql.NullString
	LQIP         sql.NullString
	Title        sql.NullString
	Description  sql.NullString
	Date         string
	LikeCount    int64
	DislikeCount int64
}

func (f FanartRow) ToMap() map[string]any {
	return map[string]any{
		"id": f.ID, "user_id": nullInt64Val(f.UserID), "nickname": f.Nickname.String,
		"image_url": f.ImageURL, "thumbnail_url": m2s(f.ThumbnailURL), "lqip": m2s(f.LQIP),
		"title": m2s(f.Title), "description": m2s(f.Description), "date": f.Date,
		"like_count": f.LikeCount, "dislike_count": f.DislikeCount,
	}
}

func GetFanartLatest(d *pdb.DB) ([]map[string]any, error) {
	query := `SELECT f.id, f.user_id, f.nickname, f.image_url, f.thumbnail_url, f.lqip, f.title, f.description,
		` + sqlDtFmt(d, "f.created_at") + ` as date,
		SUM(CASE WHEN r.reaction_type = 'like' THEN 1 ELSE 0 END) as like_count,
		SUM(CASE WHEN r.reaction_type = 'dislike' THEN 1 ELSE 0 END) as dislike_count
		FROM fanart_gallery f
		LEFT JOIN fanart_reactions r ON f.id = r.fanart_id
		WHERE f.status = 'active'
		GROUP BY f.id
		ORDER BY f.id DESC LIMIT 50`
	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var f FanartRow
		var likeCount, dislikeCount sql.NullInt64
		if err := rows.Scan(&f.ID, &f.UserID, &f.Nickname, &f.ImageURL, &f.ThumbnailURL, &f.LQIP, &f.Title, &f.Description, &f.Date, &likeCount, &dislikeCount); err == nil {
			f.LikeCount = likeCount.Int64
			f.DislikeCount = dislikeCount.Int64
			f.Date = formatDateShort(f.Date)
			out = append(out, f.ToMap())
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
}

func GetFanartByUser(d *pdb.DB, userID int64) ([]map[string]any, error) {
	query := `SELECT id, image_url, thumbnail_url, lqip, title, description,
		` + sqlDtFmt(d, "created_at") + ` as date
		FROM fanart_gallery WHERE user_id = ? AND status = 'active' ORDER BY id DESC`
	rows, err := d.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var imageURL string
		var thumbnailURL, lqip, title, description sql.NullString
		var date string
		if err := rows.Scan(&id, &imageURL, &thumbnailURL, &lqip, &title, &description, &date); err == nil {
			out = append(out, map[string]any{
				"id": id, "image_url": imageURL, "thumbnail_url": m2s(thumbnailURL), "lqip": m2s(lqip),
				"title": m2s(title), "description": m2s(description), "date": formatDateShort(date),
			})
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
}

// GetRandomActiveFanart는 승인(활성) 상태인 팬아트 중 하나를 무작위로 골라
// 돌려준다. 루미가 "팬갤러리 보여줘" 같은 요청을 받았을 때 실제 사진 한 장을
// 대화 중에 보여주기 위해 씀 - LLM한테 이미지 URL을 지어내게 하는 게 아니라
// DB에서 진짜로 하나 뽑아오는 방식.
func GetRandomActiveFanart(d *pdb.DB) (imageURL string, title, nickname sql.NullString, found bool, err error) {
	orderExpr := "RANDOM()"
	if d.Backend == "mysql" {
		orderExpr = "RAND()"
	}
	qerr := d.QueryRow(
		"SELECT image_url, title, nickname FROM fanart_gallery WHERE status = 'active' ORDER BY "+orderExpr+" LIMIT 1",
	).Scan(&imageURL, &title, &nickname)
	if qerr == sql.ErrNoRows {
		return "", sql.NullString{}, sql.NullString{}, false, nil
	}
	if qerr != nil {
		return "", sql.NullString{}, sql.NullString{}, false, qerr
	}
	return imageURL, title, nickname, true, nil
}

func sqlDtFmt(d *pdb.DB, col string) string {
	if d.Backend == "mysql" {
		return "DATE_FORMAT(" + col + ", '%Y-%m-%d %H:%i')"
	}
	return col
}
