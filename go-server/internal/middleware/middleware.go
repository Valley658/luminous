package middleware

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"pastellive/internal/httputil"
	"pastellive/internal/session"
)

type ctxKey string

const sessionCtxKey ctxKey = "pl_session"

func SessionMiddleware(store *session.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := store.Load(r)
			ctx := context.WithValue(r.Context(), sessionCtxKey, sess)

			ww := &sessionSavingWriter{ResponseWriter: w, store: store, sess: sess}
			next.ServeHTTP(ww, r.WithContext(ctx))
			ww.ensureSaved()
		})
	}
}

type sessionSavingWriter struct {
	http.ResponseWriter
	store *session.Store
	sess  *session.Session
	saved bool
}

func (w *sessionSavingWriter) ensureSaved() {
	if w.saved {
		return
	}
	w.saved = true
	w.store.Save(w.ResponseWriter, w.sess)
}

func (w *sessionSavingWriter) WriteHeader(status int) {
	w.ensureSaved()
	w.ResponseWriter.WriteHeader(status)
}

func (w *sessionSavingWriter) Write(b []byte) (int, error) {
	w.ensureSaved()
	return w.ResponseWriter.Write(b)
}

// Flush: sessionSavingWriter가 http.ResponseWriter를 구조체 필드로 감싸고 있어서,
// 이 메서드가 없으면 내부 ResponseWriter가 실제로 http.Flusher를 구현하더라도
// 바깥쪽 sessionSavingWriter에 대한 타입 단언(w.(http.Flusher))은 항상 실패한다.
// 그 결과 SSE(Server-Sent Events)로 응답하는 핸들러(예: 실시간 알림 스트림)가
// 전부 "실시간 스트림을 지원하지 않는 서버 환경입니다" 500 에러를 반환하고 있었다.
// 내부 ResponseWriter가 Flusher를 구현할 때만 그대로 위임한다.
func (w *sessionSavingWriter) Flush() {
	w.ensureSaved()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func GetSession(r *http.Request) *session.Session {
	s, _ := r.Context().Value(sessionCtxKey).(*session.Session)
	return s
}

func CSRFGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		default:
			next.ServeHTTP(w, r)
			return
		}
		if _, err := r.Cookie(session.CookieName); err != nil {
			next.ServeHTTP(w, r)
			return
		}
		checkValue := r.Header.Get("Origin")
		if checkValue == "" {
			checkValue = r.Header.Get("Referer")
		}
		// [2026-09-25 보안 감사: Origin/Referer가 둘 다 없으면 그냥 통과시키던
		// 예전 방식은 CSRF 방어를 우회당할 수 있는 구멍이었다 - 실제 브라우저가
		// 세션 쿠키를 들고 상태 변경 요청(POST 등)을 보낼 땐 거의 항상 둘 중
		// 하나는 붙어 있으므로, 없으면 "통과"가 아니라 "거부"가 안전한 기본값이다.]
		if checkValue == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		u, err := url.Parse(checkValue)
		host := ""
		if err == nil {
			host = strings.ToLower(u.Hostname())
		}
		if !httputil.TrustedOriginHosts[host] {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
