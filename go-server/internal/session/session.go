package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

const CookieName = "pl_session"

type Store struct {
	secret       []byte
	cookieSecure bool
	cookieDomain string
	maxAge       time.Duration
}

// cookieDomain: 로그인 세션 쿠키를 어느 도메인 범위까지 보낼지. 빈 문자열이면
// 로그인했던 정확한 호스트에만(예: pastellive.co.kr), ".pastellive.co.kr"처럼
// 앞에 점을 붙인 값이면 admin.pastellive.co.kr 같은 서브도메인까지 전부
// 공유된다 - config.defaultCookieDomain 참고.
func NewStore(secretKey string, cookieSecure bool, cookieDomain string) *Store {
	return &Store{
		secret:       []byte(secretKey),
		cookieSecure: cookieSecure,
		cookieDomain: cookieDomain,
		maxAge:       30 * 24 * time.Hour,
	}
}

type Session struct {
	data  map[string]any
	dirty bool
	store *Store
}

func (s *Session) Get(key string) any {
	return s.data[key]
}

func (s *Session) GetString(key string) string {
	if v, ok := s.data[key].(string); ok {
		return v
	}
	return ""
}

func (s *Session) GetInt64(key string) int64 {
	switch v := s.data[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

func (s *Session) Set(key string, value any) {
	s.data[key] = value
	s.dirty = true
}

func (s *Session) Delete(key string) {
	if _, ok := s.data[key]; ok {
		delete(s.data, key)
		s.dirty = true
	}
}

func (s *Session) Pop(key string) any {
	v, ok := s.data[key]
	if ok {
		delete(s.data, key)
		s.dirty = true
	}
	return v
}

func (s *Session) Clear() {
	s.data = map[string]any{}
	s.dirty = true
}

func (st *Store) sign(payload []byte) string {
	mac := hmac.New(sha256.New, st.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (st *Store) Load(r *http.Request) *Session {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return &Session{data: map[string]any{}, store: st}
	}
	parts := splitOnce(c.Value, '.')
	if parts == nil {
		return &Session{data: map[string]any{}, store: st}
	}
	payloadB64, sig := parts[0], parts[1]
	expectedSig := st.sign([]byte(payloadB64))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return &Session{data: map[string]any{}, store: st}
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return &Session{data: map[string]any{}, store: st}
	}
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return &Session{data: map[string]any{}, store: st}
	}
	return &Session{data: data, store: st}
}

func (st *Store) Save(w http.ResponseWriter, s *Session) {
	if !s.dirty {
		return
	}
	payload, err := json.Marshal(s.data)
	if err != nil {
		return
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	sig := st.sign([]byte(payloadB64))
	value := payloadB64 + "." + sig
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Domain:   st.cookieDomain,
		HttpOnly: true,
		Secure:   st.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(st.maxAge.Seconds()),
	})

	// [2026-09-22: admin.pastellive.co.kr 로그인 인식 버그를 고치면서 쿠키에
	// Domain(.pastellive.co.kr)을 새로 지정했는데, 예전엔 Domain 없이(호스트
	// 전용) 발급했었다 - 브라우저는 이름이 같아도 Domain이 다르면 완전히 별개의
	// 쿠키로 취급해서 예전 쿠키가 안 지워지고 새 쿠키와 같이 남는다. 그 상태로
	// 요청을 보내면 서버가 어느 쪽을 먼저 읽을지 보장이 안 되고(브라우저/서버
	// 구현마다 다름), 실제로 예전의 빈 세션 쿠키를 먼저 읽어서 방금 로그인했는데도
	// 로그아웃 상태로 보이는 문제가 있었음. Domain을 지정하는 경우엔 예전
	// 호스트 전용 쿠키를 같이 명시적으로 만료시켜서 브라우저에서 정리한다.]
	if st.cookieDomain != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     CookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   st.cookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
}

func splitOnce(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return nil
}
