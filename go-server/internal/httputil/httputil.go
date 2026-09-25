package httputil

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// GetClientIP는 rate limiter/차단/로그에 쓸 "진짜" 접속자 IP를 뽑아낸다.
//
// [2026-09-25 보안 감사: 예전엔 CF-Connecting-IP가 없으면 X-Forwarded-For의
// 맨 앞 값을 그대로 믿었는데, 이건 클라이언트가 직접 그 헤더에 아무 값이나
// 써서 보내도 그대로 통과되는 구조라 IP 기반 차단/rate limit을 통째로
// 우회당할 수 있는 구멍이었다(예: 밴 당한 IP가 X-Forwarded-For: 1.2.3.4 를
// 스스로 붙여서 다른 사람인 척). 요청 경로는 Cloudflare Tunnel -> nginx ->
// 이 서버(127.0.0.1)라, 신뢰할 수 있는 값은 (1) Cloudflare가 실제 TCP
// 접속원 기준으로 직접 채워주는 CF-Connecting-IP, (2) nginx가 $remote_addr로
// 직접 채우는 X-Real-IP(클라이언트가 위조해서 nginx를 거쳐도 nginx가 덮어씀)
// 뿐이다. X-Forwarded-For는 nginx가 기존 값 뒤에 자기 $remote_addr를
// 이어붙이는 방식($proxy_add_x_forwarded_for)이라, 맨 앞이 아니라 맨 뒤
// 값이 nginx가 실제로 확인한 IP다.]
func GetClientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		last := strings.TrimSpace(parts[len(parts)-1])
		if last != "" {
			return last
		}
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
