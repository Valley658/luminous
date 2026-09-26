package handlers

import (
	"fmt"
	"html"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pastellive/internal/auth"
	pdb "pastellive/internal/db"
	"pastellive/internal/httputil"
	"pastellive/internal/imgvalidate"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

func (a *App) ApiSetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인 정보가 만료되었습니다.")
		return
	}

	req := parseAuthRequest(r)
	currentNickname := user.NicknameOr("")
	nickname := currentNickname
	if req.Nickname != "" {
		nickname = req.Nickname
	}
	nickname = truncateRunes(strings.TrimSpace(nickname), 50)
	loginID := user.LoginID.String
	if req.LoginID != "" {
		loginID = req.LoginID
	}
	loginID = strings.TrimSpace(loginID)
	password := req.Password
	currentPassword := req.CurrentPassword

	if user.PasswordHash.Valid && user.PasswordHash.String != "" {
		ok := false
		if currentPassword != "" {
			ok, _ = auth.VerifyPassword(user.PasswordHash.String, currentPassword)
		}
		if !ok {
			httputil.JSONError(w, http.StatusForbidden, "현재 비밀번호가 일치하지 않습니다.")
			return
		}
	}

	if msg := validateNewCredentials(nickname, loginID, password, currentNickname, false); msg != "" {
		httputil.JSONError(w, http.StatusBadRequest, msg)
		return
	}

	exists, err := models.ExistsLoginID(a.DB, loginID, userID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if exists {
		httputil.JSONError(w, http.StatusConflict, "이미 사용 중인 아이디입니다.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if err := models.UpdateLoginCredentials(a.DB, userID, loginID, hash, nickname); err != nil {
		httputil.JSONError(w, http.StatusConflict, "이미 사용 중인 아이디입니다.")
		return
	}

	sess.Set("user_nickname", nickname)
	writeJSON(w, map[string]any{"success": true, "nickname": nickname})
}

func (a *App) ApiUpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {

		_ = r.ParseForm()
	}
	newNickname := strings.TrimSpace(r.FormValue("nickname"))

	if msg := validateNicknameChange(a.DB, userID, newNickname); msg != "" {
		httputil.JSONError(w, http.StatusForbidden, msg)
		return
	}

	oldPicture, _ := models.GetPicture(a.DB, userID)
	newPicture := ""

	var fh *multipart.FileHeader
	if r.MultipartForm != nil {
		if files := r.MultipartForm.File["picture"]; len(files) > 0 && files[0].Filename != "" {
			fh = files[0]
		}
	}
	if fh != nil {
		// [2026-09-26 보안 점검] 확장자/Content-Type을 신뢰하지 않고
		// imgvalidate가 실제 파일 내용을 검증/재인코딩한다.
		if fh.Size <= 0 || fh.Size > maxUploadImageBytes {
			httputil.JSONError(w, http.StatusBadRequest, "이미지 파일 크기가 올바르지 않습니다(최대 15MB).")
			return
		}
		userFolder := filepath.Join(a.Cfg.StaticDir, "uploads", "profiles", fmt.Sprintf("%d", userID))
		if err := os.MkdirAll(userFolder, 0o755); err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
			return
		}
		tmpPath := filepath.Join(userFolder, ".upload_"+imgvalidate.RandomBaseName("tmp", 16))
		if err := saveMultipartFileTo(fh, tmpPath); err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
			return
		}
		validated, err := imgvalidate.ValidateAndReencode(tmpPath, userFolder, imgvalidate.RandomBaseName("profile", 8), 88)
		_ = os.Remove(tmpPath)
		if err != nil {
			httputil.JSONError(w, http.StatusBadRequest, "올바른 이미지 파일이 아닙니다(jpg/png/webp/gif만 허용).")
			return
		}
		finalProfilePath := validated.Path
		filename := filepath.Base(finalProfilePath)
		if newPath, ok, optimized := a.JavaImage.ProcessUploadedImage(finalProfilePath, 512, 85); optimized && ok && newPath != "" {
			filename = filepath.Base(newPath)
			finalProfilePath = newPath
		}
		if modResult, _ := a.Moderation.Moderate(absPath(finalProfilePath)); modResult.Verdict == "block" {
			_ = os.Remove(finalProfilePath)
			httputil.JSONError(w, http.StatusBadRequest, "부적절한 콘텐츠로 판단되어 업로드가 거부되었습니다.")
			return
		}
		newPicture = fmt.Sprintf("/static/uploads/profiles/%d/%s", userID, filename)
	}

	if newPicture != "" {
		if err := models.UpdateNicknameAndPicture(a.DB, userID, newNickname, newPicture); err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
			return
		}
		sess.Set("user_picture", newPicture)
	} else {
		if err := models.UpdateNickname(a.DB, userID, newNickname); err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
			return
		}
	}
	sess.Set("user_nickname", newNickname)

	if newPicture != "" && oldPicture.Valid && oldPicture.String != "" && oldPicture.String != newPicture {
		prefix := fmt.Sprintf("/static/uploads/profiles/%d/", userID)
		if strings.HasPrefix(oldPicture.String, prefix) {
			oldAbsPath := filepath.Join(a.Cfg.StaticDir, strings.TrimPrefix(oldPicture.String, "/static/"))
			expectedDir := filepath.Join(a.Cfg.StaticDir, "uploads", "profiles", fmt.Sprintf("%d", userID))
			if realOld, err1 := filepath.EvalSymlinks(oldAbsPath); err1 == nil {
				if realDir, err2 := filepath.EvalSymlinks(expectedDir); err2 == nil && strings.HasPrefix(realOld, realDir+string(filepath.Separator)) {
					_ = os.Remove(oldAbsPath)
				}
			}
		}
	}

	picture := newPicture
	if picture == "" {
		picture = oldPicture.String
	}
	writeJSON(w, map[string]any{"success": true, "nickname": newNickname, "picture": nullableEmptyToNil(picture)})
}

func nullableEmptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func (a *App) ApiUpdateEmailHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다")
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	_ = decodeJSONBody(r, &body)
	newEmail := strings.TrimSpace(body.Email)
	if newEmail == "" || !emailPattern.MatchString(newEmail) {
		httputil.JSONError(w, http.StatusBadRequest, "올바른 이메일 형식이 아닙니다.")
		return
	}
	if len(newEmail) > 255 {
		httputil.JSONError(w, http.StatusBadRequest, "이메일이 너무 깁니다.")
		return
	}
	affected, err := models.UpdateEmail(a.DB, userID, newEmail)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if affected == 0 {
		httputil.JSONError(w, http.StatusNotFound, "계정을 찾을 수 없습니다. 다시 로그인 후 시도해주세요.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "email": newEmail})
}

func (a *App) ApiUpdateNicknameHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다")
		return
	}
	var body struct {
		Nickname string `json:"nickname"`
	}
	_ = decodeJSONBody(r, &body)
	newNickname := strings.TrimSpace(body.Nickname)
	if newNickname == "" || len([]rune(newNickname)) > 20 {
		httputil.JSONError(w, http.StatusBadRequest, "닉네임은 1~20자로 입력해주세요")
		return
	}
	if msg := validateNicknameChange(a.DB, userID, newNickname); msg != "" {
		httputil.JSONError(w, http.StatusForbidden, msg)
		return
	}
	if err := models.UpdateNickname(a.DB, userID, newNickname); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	sess.Set("user_nickname", newNickname)
	writeJSON(w, map[string]any{"success": true, "nickname": newNickname})
}

func (a *App) ApiWithdrawHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	if err := models.AdminDeleteUser(a.DB, userID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	sess.Clear()
	writeJSON(w, map[string]any{"success": true, "message": "회원 탈퇴가 완료되었습니다."})
}

func suggestAltNickname(nickname string) string {
	return fmt.Sprintf("%s_%s", nickname, randomHex(2))
}

func validateNicknameChange(d *pdb.DB, userID int64, newNickname string) string {
	currentUser, _ := models.GetUserByID(d, userID)
	currentNickname := ""
	if currentUser != nil {
		currentNickname = currentUser.NicknameOr("")
	}
	if newNickname == currentNickname {
		return ""
	}
	if !isValidNicknameContent(newNickname) {
		return "닉네임에 사용할 수 없는 문자가 포함되어 있거나 20자를 초과했습니다."
	}
	if staffNicknames[currentNickname] {
		return currentNickname + "은(는) 운영진 닉네임이므로 변경할 수 없습니다."
	}
	if staffNicknames[newNickname] {
		return "이 닉네임은 운영진 닉네임입니다. 추천된 닉네임: " + suggestAltNickname(newNickname) + " 또는 다른 닉네임을 사용해주세요."
	}
	return ""
}

// ---------------------------------------------------------------------------
// 비밀번호 찾기(재설정)
// ---------------------------------------------------------------------------

const passwordResetTokenTTL = 30 * time.Minute

// ApiForgotPasswordHandler는 이메일을 받아서(등록돼 있으면) 재설정 링크를
// 보낸다. 그 이메일로 가입된 계정이 있는지/없는지를 응답으로 구분해서
// 알려주지 않는다 - "이 이메일로 가입된 계정이 없습니다" 같은 메시지는
// 공격자가 어떤 이메일이 가입돼 있는지 하나씩 확인해보는 데(계정 존재 여부
// 스캔) 악용될 수 있어서, 항상 같은 성공 메시지로 답한다.
func (a *App) ApiForgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if !a.Email.Enabled() {
		httputil.JSONError(w, http.StatusServiceUnavailable, "이메일 발송 기능이 아직 설정되지 않았어요. 관리자에게 문의해주세요.")
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	_ = decodeJSONBody(r, &body)
	target := strings.TrimSpace(body.Email)
	const genericOK = "이 이메일로 가입된 계정이 있다면, 비밀번호 재설정 링크를 보내드렸어요. 메일함(스팸함 포함)을 확인해주세요."

	if target == "" || !emailPattern.MatchString(target) {
		httputil.JSONError(w, http.StatusBadRequest, "올바른 이메일 형식을 입력해주세요.")
		return
	}

	user, err := models.GetUserByEmail(a.DB, target)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if user == nil {
		// 계정이 없어도 있을 때와 똑같은 성공 응답 - 위 설명 참고.
		writeJSON(w, map[string]any{"success": true, "message": genericOK})
		return
	}

	token, err := models.CreatePasswordResetToken(a.DB, user.ID, passwordResetTokenTTL)
	if err != nil {
		log.Printf("비밀번호 재설정 토큰 생성 실패(user_id=%d): %v", user.ID, err)
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}

	resetURL := "https://" + a.Cfg.SiteHost + "/reset-password?token=" + token
	nickname := html.EscapeString(user.NicknameOr("회원"))
	subject := "[루미너스] 비밀번호 재설정 안내"
	bodyHTML := fmt.Sprintf(`
		<div style="font-family:'Malgun Gothic',sans-serif;max-width:480px;margin:0 auto;padding:24px;color:#222;">
			<h2 style="color:#38bdf8;">루미너스 비밀번호 재설정</h2>
			<p>%s님, 안녕하세요. 아래 버튼을 눌러 새 비밀번호를 설정해주세요.</p>
			<p style="margin:28px 0;">
				<a href="%s" style="background:#38bdf8;color:#fff;padding:12px 24px;border-radius:8px;text-decoration:none;font-weight:bold;">비밀번호 재설정하기</a>
			</p>
			<p style="font-size:13px;color:#888;">이 링크는 %d분 동안만 유효합니다. 본인이 요청하지 않았다면 이 메일을 무시하셔도 됩니다.</p>
			<p style="font-size:12px;color:#aaa;">버튼이 안 눌리면 이 주소를 복사해서 브라우저에 붙여넣어주세요:<br>%s</p>
		</div>`, nickname, resetURL, int(passwordResetTokenTTL.Minutes()), resetURL)

	if err := a.Email.Send(target, subject, bodyHTML); err != nil {
		log.Printf("비밀번호 재설정 메일 발송 실패(user_id=%d): %v", user.ID, err)
		httputil.JSONError(w, http.StatusInternalServerError, "메일 발송에 실패했어요. 잠시 후 다시 시도해주세요.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": genericOK})
}

// ApiResetPasswordHandler는 이메일 속 링크의 토큰과 새 비밀번호를 받아 실제로
// 비밀번호를 바꾼다.
func (a *App) ApiResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	_ = decodeJSONBody(r, &body)
	token := strings.TrimSpace(body.Token)
	password := body.Password
	if token == "" {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	if len([]rune(password)) < 8 {
		httputil.JSONError(w, http.StatusBadRequest, "비밀번호는 8자 이상이어야 합니다.")
		return
	}

	userID, ok, err := models.ConsumePasswordResetToken(a.DB, token)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if !ok {
		httputil.JSONError(w, http.StatusBadRequest, "링크가 만료되었거나 이미 사용된 링크예요. 비밀번호 찾기를 다시 요청해주세요.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if err := models.UpdatePasswordHash(a.DB, userID, hash); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "비밀번호가 변경됐어요. 새 비밀번호로 로그인해주세요."})
}

// ResetPasswordPageHandler는 이메일 링크(/reset-password?token=...)를 클릭했을
// 때 보여주는 새 비밀번호 입력 페이지.
func (a *App) ResetPasswordPageHandler(w http.ResponseWriter, r *http.Request) {
	if err := a.Templates.Render(w, r, "reset_password.html", map[string]any{"request": requestContext(r)}, a.GenRepImageOverrides); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
