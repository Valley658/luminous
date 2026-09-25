package handlers

import (
	"net/http"
	"strconv"

	"pastellive/internal/models"
)

// ApiLeaderboardHandler는 포인트 상위 랭킹을 돌려준다. 누구나(비로그인도) 볼 수 있다.
// ?period=week 나 ?period=month를 주면 이번 주/이번 달 동안 쌓인 포인트만으로
// 랭킹을 매긴다(생략하면 전체 누적 랭킹).
func (a *App) ApiLeaderboardHandler(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	period := r.URL.Query().Get("period")
	entries, err := models.GetLeaderboardByPeriod(a.DB, period, limit)
	if err != nil {
		writeJSON(w, map[string]any{"success": false, "leaderboard": []any{}})
		return
	}
	writeJSON(w, map[string]any{"success": true, "leaderboard": entries, "period": period})
}

// ApiMyPointsHandler는 로그인한 사용자 본인의 포인트/뱃지/순위를 돌려준다.
func (a *App) ApiMyPointsHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		writeJSON(w, map[string]any{"success": false, "logged_in": false})
		return
	}
	points, badge, rank, err := models.GetUserPoints(a.DB, userID)
	if err != nil {
		writeJSON(w, map[string]any{"success": false, "logged_in": true})
		return
	}
	writeJSON(w, map[string]any{"success": true, "logged_in": true, "points": points, "badge": badge, "rank": rank})
}
