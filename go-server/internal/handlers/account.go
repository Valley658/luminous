package handlers

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pastellive/internal/auth"
	pdb "pastellive/internal/db"
	"pastellive/internal/httputil"
	"pastellive/internal/javaimage"
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

	if msg := validateNewCredentials(nickname, loginID, password, currentNickname); msg != "" {
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
		ext := ""
		if idx := strings.LastIndex(fh.Filename, "."); idx != -1 {
			ext = strings.ToLower(fh.Filename[idx+1:])
		}
		if !javaimage.IsAllowedImageExt(ext) {
			httputil.JSONError(w, http.StatusBadRequest, "jpg/png/webp/gif/heic 이미지 파일만 업로드할 수 있습니다.")
			return
		}
		userFolder := filepath.Join(a.Cfg.StaticDir, "uploads", "profiles", fmt.Sprintf("%d", userID))
		if err := os.MkdirAll(userFolder, 0o755); err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
			return
		}
		filename := fmt.Sprintf("profile_%s.%s", randomHex(4), ext)
		savedPath := filepath.Join(userFolder, filename)
		if err := saveMultipartFileTo(fh, savedPath); err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
			return
		}
		finalProfilePath := savedPath
		if newPath, ok, rejected := a.JavaImage.ProcessUploadedImage(savedPath, 512, 85); rejected {
			_ = os.Remove(savedPath)
			httputil.JSONError(w, http.StatusBadRequest, "올바른 이미지 파일이 아닙니다.")
			return
		} else if ok && newPath != "" {
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
