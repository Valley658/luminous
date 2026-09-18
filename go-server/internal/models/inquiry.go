package models

import (
	"database/sql"

	pdb "pastellive/internal/db"
)

// InitInquiriesTable creates the inquiries table (문의사항/의견).
// Unlike most Init*Table functions in this package, this one does NOT skip
// the mysql backend: there is no separate schema-migration mechanism for
// the production mysql database in this codebase (see main.go's
// fanart_phash table for the same pattern), so both backends are created
// here with backend-appropriate syntax.
func InitInquiriesTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS inquiries (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id INT,
			nickname VARCHAR(50),
			contact VARCHAR(150),
			category VARCHAR(30) NOT NULL DEFAULT 'general',
			content TEXT NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			ip_address VARCHAR(45),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`)
		return err
	}
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS inquiries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		nickname VARCHAR(50),
		contact VARCHAR(150),
		category VARCHAR(30) NOT NULL DEFAULT 'general',
		content TEXT NOT NULL,
		status VARCHAR(20) NOT NULL DEFAULT 'pending',
		ip_address VARCHAR(45),
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`)
	return err
}

// CreateInquiry inserts a new inquiry/feedback submission. userID may be 0
// (anonymous submission).
func CreateInquiry(d *pdb.DB, userID int64, nickname, contact, category, content, ip string) error {
	var userIDArg any
	if userID != 0 {
		userIDArg = userID
	}
	_, err := d.Exec(
		"INSERT INTO inquiries (user_id, nickname, contact, category, content, ip_address) VALUES (?, ?, ?, ?, ?, ?)",
		userIDArg, nickname, contact, category, content, ip,
	)
	return err
}

type InquiryRow struct {
	ID       int64
	UserID   sql.NullInt64
	Nickname sql.NullString
	Contact  sql.NullString
	Category sql.NullString
	Content  string
	Status   string
	Date     string
}

func (r InquiryRow) ToMap() map[string]any {
	return map[string]any{
		"id": r.ID, "user_id": nullInt64OrNil(r.UserID),
		"nickname": m2s(r.Nickname), "contact": m2s(r.Contact),
		"category": m2s(r.Category), "content": r.Content,
		"status": r.Status, "date": r.Date,
	}
}

func nullInt64OrNil(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

// ListInquiries returns inquiries newest-first, optionally filtered by status
// ("pending", "resolved", or "" / "all" for everything).
func ListInquiries(d *pdb.DB, statusFilter string) ([]InquiryRow, error) {
	dateExpr := sqlDtFmt(d, "created_at")
	var rows *sql.Rows
	var err error
	if statusFilter != "" && statusFilter != "all" {
		rows, err = d.Query(
			"SELECT id, user_id, nickname, contact, category, content, status, "+dateExpr+" as date "+
				"FROM inquiries WHERE status = ? ORDER BY id DESC LIMIT 200", statusFilter)
	} else {
		rows, err = d.Query(
			"SELECT id, user_id, nickname, contact, category, content, status, " + dateExpr + " as date " +
				"FROM inquiries ORDER BY id DESC LIMIT 200")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]InquiryRow, 0)
	for rows.Next() {
		var row InquiryRow
		var date string
		if err := rows.Scan(&row.ID, &row.UserID, &row.Nickname, &row.Contact, &row.Category, &row.Content, &row.Status, &date); err != nil {
			return nil, err
		}
		row.Date = formatDateShort(date)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func ResolveInquiry(d *pdb.DB, inquiryID int64) (int64, error) {
	res, err := d.Exec("UPDATE inquiries SET status = 'resolved' WHERE id = ?", inquiryID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func CountPendingInquiries(d *pdb.DB) (int64, error) {
	var c int64
	err := d.QueryRow("SELECT COUNT(*) FROM inquiries WHERE status = 'pending'").Scan(&c)
	return c, err
}
