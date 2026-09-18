package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

var inquiryCategories = map[string]bool{
	"general": true, // 일반 문의
	"bug":     true, // 버그 신고
	"suggest": true, // 건의/의견
}

// ApiSubmitInquiryHandler lets any visitor (logged in or not) send an
// inquiry / suggestion (문의사항, 의견) from within the site.
func (a *App) ApiSubmitInquiryHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content  string `json:"content"`
		Contact  string `json:"contact"`
		Category string `json:"category"`
		Nickname string `json:"nickname"`
	}
	_ = decodeJSONBody(r, &body)

	content := sanitizeUserHTML(strings.TrimSpace(body.Content))
	if content == "" {
		httputil.JSONError(w, http.StatusBadRequest, "내용을 입력해주세요.")
		return
	}
	if len([]rune(content)) > 3000 {
		httputil.JSONError(w, http.StatusBadRequest, "내용은 3000자 이하로 입력해주세요.")
		return
	}
	contact := sanitizeUserHTML(strings.TrimSpace(body.Contact))
	if len([]rune(contact)) > 150 {
		contact = string([]rune(contact)[:150])
	}
	category := strings.TrimSpace(body.Category)
	if !inquiryCategories[category] {
		category = "general"
	}

	sess := middleware.GetSession(r)
	userID := int64(0)
	nickname := sanitizeUserHTML(strings.TrimSpace(body.Nickname))
	if sess != nil {
		userID = sess.GetInt64("user_id")
		if n := sess.GetString("user_nickname"); n != "" {
			nickname = n
		}
	}
	if nickname == "" {
		nickname = "익명"
	}
	if len([]rune(nickname)) > 50 {
		nickname = string([]rune(nickname)[:50])
	}

	if err := models.CreateInquiry(a.DB, userID, nickname, contact, category, content, httputil.GetClientIP(r)); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "전송 중 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// ApiListInquiriesHandler (admin only) lists submitted inquiries.
func (a *App) ApiListInquiriesHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	statusFilter := r.URL.Query().Get("status")
	if statusFilter == "" {
		statusFilter = "pending"
	}
	rows, err := models.ListInquiries(a.DB, statusFilter)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	inquiries := make([]map[string]any, len(rows))
	for i, row := range rows {
		inquiries[i] = row.ToMap()
	}
	writeJSON(w, map[string]any{"success": true, "inquiries": inquiries})
}

// ApiResolveInquiryHandler (admin only) marks an inquiry as resolved.
func (a *App) ApiResolveInquiryHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	inquiryID, err := strconv.ParseInt(chi.URLParam(r, "inquiryID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	if _, err := models.ResolveInquiry(a.DB, inquiryID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	a.logAdminAction(r, "inquiry_resolve", "inquiry", strconv.FormatInt(inquiryID, 10), "")
	writeJSON(w, map[string]any{"success": true})
}
