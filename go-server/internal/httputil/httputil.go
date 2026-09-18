package httputil

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func GetClientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if first != "" {
			return first
		}
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if host == "" {
		return "unknown"
	}
	return host
}

var TrustedOriginHosts = map[string]bool{
	"pastellive.co.kr": true, "www.pastellive.co.kr": true, "m.pastellive.co.kr": true,
	"admin.pastellive.co.kr": true, "api.pastellive.co.kr": true,
}

func SafeNextURL(raw string) string {
	if raw == "" {
		return "/"
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.HasPrefix(raw, "/\\") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	host := strings.ToLower(u.Hostname())
	if TrustedOriginHosts[host] {
		return raw
	}
	return "/"
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func JSONOK(w http.ResponseWriter, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["success"]; !ok {
		payload["success"] = true
	}
	JSON(w, 200, payload)
}

func JSONError(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]any{"success": false, "message": message})
}
