package handlers

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"pastellive/internal/auth"
	"pastellive/internal/config"
	"pastellive/internal/httputil"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

var (
	loginIDPattern    = regexp.MustCompile(`^[a-zA-Z0-9_]{4,20}$`)
	nicknameForbidden = regexp.MustCompile(`[<>` + "`" + `\x00-\x1f\x7f]`)

	staffNicknames = map[string]bool{}
	devNicknames   = map[string]bool{}
	devEmail       = ""
	devBypassIPs   = map[string]bool{}
)

func initDevAccess(cfg *config.Config) {
	staffNicknames = cfg.StaffNicknames
	devNicknames = cfg.DevNicknames
	devEmail = cfg.DevEmail
	devBypassIPs = cfg.DevBypassIPs
}

type authRequest struct {
	Nickname        string `json:"nickname"`
	LoginID         string `json:"login_id"`
	Password        string `json:"password"`
	CurrentPassword string `json:"current_password"`
}

func parseAuthRequest(r *http.Request) *authRequest {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return &authRequest{}
	}
	var req authRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	return &req
}

func isValidNicknameContent(nickname string) bool {
	trimmed := strings.TrimSpace(nickname)
	if trimmed == "" {
		return false
	}
	if len([]rune(nickname)) > 20 {
		return false
	}
	if nicknameForbidden.MatchString(nickname) {
		return false
	}
	return true
}

func validateNewCredentials(nickname, loginID, password, currentNickname string, allowReservedNickname bool) string {
	if nickname == "" {
		return "닉네임을 입력해주세요."
	}
	if nickname != currentNickname && !isValidNicknameContent(nickname) {
		return "닉네임에 사용할 수 없는 문자가 포함되어 있거나 20자를 초과했습니다."
	}
	if staffNicknames[nickname] && nickname != currentNickname && !allowReservedNickname {
		return "'" + nickname + "'은(는) 사용할 수 없는 닉네임입니다."
	}
	if !loginIDPattern.MatchString(loginID) {
		return "아이디는 영문/숫자/밑줄(_) 4~20자여야 합니다."
	}
	if len([]rune(password)) < 8 {
		return "비밀번호는 8자 이상이어야 합니다."
	}
	return ""
}

func runesLen(s string) int { return len([]rune(s)) }

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (a *App) Register(w http.ResponseWriter, r *http.Request) {
	req := parseAuthRequest(r)
	nickname := truncateRunes(strings.TrimSpace(req.Nickname), 50)
	loginID := strings.TrimSpace(req.LoginID)
	password := req.Password

	// DB가 비어있는(=완전히 새로 만들어진) 상태에서만, 예약된 닉네임(리도 등)으로
	// 첫 계정을 만드는 것을 1회 허용한다. 유저가 1명이라도 있으면 이 예외는
	// 즉시 다시 잠긴다 - 재부팅/재실행해도 다시 열리지 않는 자동 잠금 장치.
	allowReserved := false
	var userCount int64
	if err := a.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err == nil && userCount == 0 {
		allowReserved = true
	}

	if msg := validateNewCredentials(nickname, loginID, password, "", allowReserved); msg != "" {
		httputil.JSONError(w, 400, msg)
		return
	}

	exists, err := models.ExistsLoginID(a.DB, loginID, 0)
	if err != nil {
		log.Printf("register: exists check failed: %v", err)
		httputil.JSONError(w, 500, "가입 중 오류가 발생했습니다.")
		return
	}
	if exists {
		httputil.JSONError(w, 409, "이미 사용 중인 아이디입니다.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		httputil.JSONError(w, 500, "가입 중 오류가 발생했습니다.")
		return
	}
	newID, err := models.CreateUser(a.DB, loginID, hash, nickname, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSONError(w, 409, "이미 사용 중인 아이디입니다.")
		return
	}
	user, err := models.GetUserByID(a.DB, newID)
	if err != nil || user == nil {
		httputil.JSONError(w, 500, "가입 중 오류가 발생했습니다.")
		return
	}

	sess := middleware.GetSession(r)
	sess.Set("user_id", user.ID)
	sess.Set("user_nickname", user.NicknameOr(""))
	sess.Set("user_picture", user.PictureOr(""))

	httputil.JSONOK(w, map[string]any{"nickname": user.NicknameOr(""), "picture": user.PictureOr("")})
}

func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	req := parseAuthRequest(r)
	loginID := strings.TrimSpace(req.LoginID)
	password := req.Password
	if loginID == "" || password == "" {
		httputil.JSONError(w, 400, "아이디와 비밀번호를 입력해주세요.")
		return
	}

	user, err := models.GetUserByLoginID(a.DB, loginID)
	if err != nil {
		log.Printf("login: lookup failed: %v", err)
		httputil.JSONError(w, 500, "처리 중 오류가 발생했습니다.")
		return
	}

	var storedHash string
	if user != nil && user.PasswordHash.Valid {
		storedHash = user.PasswordHash.String
	}
	ok, needsRehash := auth.VerifyPassword(storedHash, password)
	if user == nil || !ok {
		log.Printf("[로그인 실패] login_id=%q ip=%s", loginID, httputil.GetClientIP(r))
		httputil.JSONError(w, 401, "아이디 또는 비밀번호가 올바르지 않습니다.")
		return
	}

	sess := middleware.GetSession(r)
	sess.Set("user_id", user.ID)
	sess.Set("user_nickname", user.NicknameOr(""))
	sess.Set("user_picture", user.PictureOr(""))

	if needsRehash {
		if newHash, err := auth.HashPassword(password); err == nil {
			_ = models.UpdateLastIPAndPasswordHash(a.DB, user.ID, httputil.GetClientIP(r), newHash)
		} else {
			_ = models.UpdateLastIP(a.DB, user.ID, httputil.GetClientIP(r))
		}
	} else {
		_ = models.UpdateLastIP(a.DB, user.ID, httputil.GetClientIP(r))
	}

	httputil.JSONOK(w, map[string]any{"nickname": user.NicknameOr(""), "picture": user.PictureOr("")})
}

func (a *App) Me(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	discordLinked := sess.Pop("flash_discord_linked")
	discordFlashError := sess.Pop("flash_discord_error")

	flash := map[string]any{
		"discord_link_success": discordLinked != nil && discordLinked != false,
		"discord_flash_error":  discordFlashError,
	}

	userID := sess.GetInt64("user_id")
	if userID == 0 {
		out := map[string]any{"logged_in": false}
		for k, v := range flash {
			out[k] = v
		}
		httputil.JSON(w, 200, out)
		return
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {
		sess.Clear()
		out := map[string]any{"logged_in": false}
		for k, v := range flash {
			out[k] = v
		}
		httputil.JSON(w, 200, out)
		return
	}
	needsSetup := sess.Pop("needs_setup")
	nickname := user.NicknameOr("")
	out := map[string]any{
		"logged_in":        true,
		"nickname":         nickname,
		"picture":          user.PictureOr(""),
		"email":            nullStr(user.Email),
		"needs_setup":      needsSetup != nil && needsSetup != false,
		"needs_migration":  !(user.PasswordHash.Valid && user.PasswordHash.String != ""),
		"discord_linked":   user.DiscordID.Valid && user.DiscordID.String != "",
		"discord_username": nullStr(user.DiscordUsername),
		"is_admin":         a.isAdmin(r),
		"is_staff":         staffNicknames[nickname],
	}
	for k, v := range flash {
		out[k] = v
	}
	httputil.JSON(w, 200, out)
}

func nullStr(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	next := httputil.SafeNextURL(r.Referer())
	sess.Clear()
	http.Redirect(w, r, next, http.StatusFound)
}

func (a *App) DiscordLoginStart(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.DiscordClientID == "" || a.Cfg.DiscordRedirectURI == "" {
		http.Error(w, "디스코드 로그인이 아직 설정되지 않았습니다. 잠시 후 다시 시도해주세요.", http.StatusServiceUnavailable)
		return
	}
	sess := middleware.GetSession(r)
	state := randomHex(16)
	sess.Set("discord_oauth_state", state)
	intent := "login"
	if sess.GetInt64("user_id") != 0 {
		intent = "link"
	}
	sess.Set("discord_oauth_intent", intent)
	sess.Set("next_url", httputil.SafeNextURL(r.Referer()))

	q := url.Values{}
	q.Set("client_id", a.Cfg.DiscordClientID)
	q.Set("redirect_uri", a.Cfg.DiscordRedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "identify")
	q.Set("state", state)
	q.Set("prompt", "consent")
	http.Redirect(w, r, "https://discord.com/api/oauth2/authorize?"+q.Encode(), http.StatusFound)
}

func (a *App) DiscordLoginCallback(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	expectedState, _ := sess.Pop("discord_oauth_state").(string)
	returnedState := r.URL.Query().Get("state")
	intentVal := sess.Pop("discord_oauth_intent")
	intent, _ := intentVal.(string)
	if intent == "" {
		intent = "login"
	}
	nextURLVal := sess.Pop("next_url")
	nextURL, _ := nextURLVal.(string)
	if nextURL == "" {
		nextURL = "/"
	}

	if expectedState == "" || returnedState == "" || subtle.ConstantTimeCompare([]byte(expectedState), []byte(returnedState)) != 1 {
		log.Printf("[보안] 디스코드 로그인 state 불일치/누락 - OAuth 로그인 CSRF 의심 (IP: %s)", httputil.GetClientIP(r))
		http.Redirect(w, r, "/?login_error=1", http.StatusFound)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/?login_error=1", http.StatusFound)
		return
	}

	discordID, discordUsername, err := exchangeDiscordCode(a.Cfg.DiscordClientID, a.Cfg.DiscordClientSecret, a.Cfg.DiscordRedirectURI, code)
	if err != nil || discordID == "" {
		log.Printf("디스코드 OAuth 콜백 실패: %v", err)
		http.Redirect(w, r, "/?login_error=1", http.StatusFound)
		return
	}

	if intent == "link" {
		userID := sess.GetInt64("user_id")
		if userID == 0 {
			http.Redirect(w, r, "/?login_error=1", http.StatusFound)
			return
		}
		existing, err := models.GetUserByDiscordID(a.DB, discordID)
		if err == nil && existing != nil && existing.ID != userID {
			sess.Set("flash_discord_error", "이미 다른 계정에 연동되어 있는 디스코드 계정입니다.")
			http.Redirect(w, r, nextURL, http.StatusFound)
			return
		}
		if err := models.LinkDiscord(a.DB, userID, discordID, discordUsername); err != nil {
			sess.Set("flash_discord_error", "디스코드 연동 중 오류가 발생했습니다.")
			http.Redirect(w, r, nextURL, http.StatusFound)
			return
		}
		sess.Set("flash_discord_linked", true)
		http.Redirect(w, r, nextURL, http.StatusFound)
		return
	}

	user, err := models.GetUserByDiscordID(a.DB, discordID)
	if err != nil || user == nil {
		sess.Set("flash_discord_error", "이 디스코드 계정에 연동된 사이트 계정이 없습니다. 먼저 아이디/비밀번호로 가입한 뒤 프로필에서 디스코드를 연동해주세요.")
		http.Redirect(w, r, nextURL, http.StatusFound)
		return
	}
	sess.Set("user_id", user.ID)
	sess.Set("user_nickname", user.NicknameOr(""))
	sess.Set("user_picture", user.PictureOr(""))
	_ = models.UpdateLastIP(a.DB, user.ID, httputil.GetClientIP(r))
	http.Redirect(w, r, nextURL, http.StatusFound)
}
