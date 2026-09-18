package handlers

import (
	"net/http"

	"pastellive/internal/httputil"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

// IsAdmin is the exported form of isAdmin, for use outside this package
// (e.g. the admin-hostname gate registered in cmd/server/main.go).
func (a *App) IsAdmin(r *http.Request) bool {
	return a.isAdmin(r)
}

func (a *App) isAdmin(r *http.Request) bool {
	sess := middleware.GetSession(r)
	if sess == nil || sess.GetString("user_nickname") != a.Cfg.AdminNickname {
		return false
	}
	userID := sess.GetInt64("user_id")
	if userID == 0 || a.Cfg.AdminDriveAllowedEmail == "" {
		return false
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {
		return false
	}
	return equalFoldTrim(user.Email.String, a.Cfg.AdminDriveAllowedEmail)
}

func (a *App) logAdminAction(r *http.Request, action, targetType, targetID, detail string) {
	sess := middleware.GetSession(r)
	if sess == nil {
		return
	}
	userID := sess.GetInt64("user_id")
	var email string
	if userID != 0 {
		if user, err := models.GetUserByID(a.DB, userID); err == nil && user != nil {
			email = user.Email.String
		}
	}
	_ = models.InsertAdminAuditLog(a.DB, email, sess.GetString("user_nickname"), action, targetType, targetID, detail, httputil.GetClientIP(r))
}
