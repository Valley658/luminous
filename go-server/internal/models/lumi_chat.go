package models

import (
	pdb "pastellive/internal/db"
)

// lumi_chat_history는 로그인한 사용자가 루미(AI 마스코트)와 나눈 대화를
// 저장해뒀다가, 나중에 다시 사이트에 들어왔을 때 그대로 불러와 보여주기 위한
// 테이블이다. 비로그인 방문자는 user_id가 없어서(세션에만 임시로 1회
// 카운트만 남음) 저장하지 않는다 - 로그인 사용자만 대상.
//
// [2026-09-25: 사이트 본 DB(pastellive_db)와 섞이지 않게, 이 테이블은 이제
// 별도의 루미 전용 DB(pdb.OpenLumi가 여는 cfg.LumiDBName/LumiSQLitePath)에
// 산다 - 호출하는 쪽(main.go)이 본 DB가 아니라 그 전용 연결을 넘겨준다.
// 그래서 예전처럼 "MySQL은 mysql_bootstrap.go가 대신 만들어주니 여기선
// 건너뛴다"고 할 필요가 없어졌고, 이 함수 하나가 SQLite/MySQL 둘 다 직접
// 책임진다.]
func InitLumiChatHistoryTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		_, err := d.Exec(`CREATE TABLE IF NOT EXISTS lumi_chat_history (
			id INT PRIMARY KEY AUTO_INCREMENT,
			user_id INT NOT NULL,
			question TEXT NOT NULL,
			reply TEXT NOT NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_lumi_chat_history_user (user_id, id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
		return err
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS lumi_chat_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		question TEXT NOT NULL,
		reply TEXT NOT NULL,
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`); err != nil {
		return err
	}
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_lumi_chat_history_user ON lumi_chat_history (user_id, id)`)
	return nil
}

type LumiChatMessage struct {
	ID        int64  `json:"id"`
	Question  string `json:"question"`
	Reply     string `json:"reply"`
	CreatedAt string `json:"created_at"`
}

// SaveLumiChatMessage는 질문/답변 한 쌍을 저장한다. userID가 0(비로그인)이면
// 아무것도 하지 않는다 - 호출하는 쪽에서도 걸러주지만 여기서도 한 번 더 방어.
func SaveLumiChatMessage(d *pdb.DB, userID int64, question, reply string) error {
	if userID == 0 {
		return nil
	}
	_, err := d.Exec(
		"INSERT INTO lumi_chat_history (user_id, question, reply) VALUES (?, ?, ?)",
		userID, question, reply,
	)
	return err
}

// ListLumiChatHistory는 최근 대화부터 limit개를 가져온 뒤, 화면에 그대로
// 순서대로 뿌릴 수 있게 오래된 것 -> 최신 순으로 뒤집어서 반환한다.
func ListLumiChatHistory(d *pdb.DB, userID int64, limit int) ([]LumiChatMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := d.Query(
		"SELECT id, question, reply, created_at FROM lumi_chat_history WHERE user_id = ? ORDER BY id DESC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LumiChatMessage
	for rows.Next() {
		var m LumiChatMessage
		if err := rows.Scan(&m.ID, &m.Question, &m.Reply, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// DeleteLumiChatHistory는 사용자가 직접 대화 기록을 지우고 싶을 때 쓴다.
func DeleteLumiChatHistory(d *pdb.DB, userID int64) error {
	_, err := d.Exec("DELETE FROM lumi_chat_history WHERE user_id = ?", userID)
	return err
}
