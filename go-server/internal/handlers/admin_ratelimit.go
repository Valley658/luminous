package handlers

import (
	"net/http"

	"pastellive/internal/httputil"
)

func (a *App) ApiAdminRateLimitHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	if a.RateLimiter == nil {
		writeJSON(w, map[string]any{"success": true, "enabled": false, "ips": []any{}})
		return
	}
	enabled, windowSec, maxRequests, banMinutes := a.RateLimiter.Config()
	writeJSON(w, map[string]any{
		"success":      true,
		"enabled":      enabled,
		"window_sec":   windowSec,
		"max_requests": maxRequests,
		"ban_minutes":  banMinutes,
		"ips":          a.RateLimiter.Snapshot(),
	})
}
