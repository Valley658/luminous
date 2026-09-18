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
	maxAge       time.Duration
}

func NewStore(secretKey string, cookieSecure bool) *Store {
	return &Store{
		secret:       []byte(secretKey),
		cookieSecure: cookieSecure,
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
		HttpOnly: true,
		Secure:   st.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(st.maxAge.Seconds()),
	})
}

func splitOnce(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return nil
}
