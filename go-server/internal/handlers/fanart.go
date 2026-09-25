package handlers

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/javaimage"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

func strToNull(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func (a *App) ApiUploadFanartHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	nickname := "익명스텔리언"
	if sess := middleware.GetSession(r); sess != nil {
		if n := sess.GetString("user_nickname"); n != "" {
			nickname = n
		}
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "이미지 파일이 없습니다.")
		return
	}
	files := r.MultipartForm.File["image"]
	if len(files) == 0 {
		httputil.JSONError(w, http.StatusBadRequest, "이미지 파일이 없습니다.")
		return
	}
	fh := files[0]
	if fh.Filename == "" {
		httputil.JSONError(w, http.StatusBadRequest, "선택된 파일이 없습니다.")
		return
	}
	title := strings.TrimSpace(sanitizeUserHTML(r.FormValue("title")))
	if len(title) > 50 {
		title = title[:50]
	}
	description := strings.TrimSpace(sanitizeUserHTML(r.FormValue("description")))

	ext := ""
	if idx := strings.LastIndex(fh.Filename, "."); idx != -1 {
		ext = strings.ToLower(fh.Filename[idx+1:])
	}
	if !javaimage.IsAllowedImageExt(ext) {
		httputil.JSONError(w, http.StatusBadRequest, "jpg/png/webp/gif/heic 이미지 파일만 업로드할 수 있습니다.")
		return
	}

	imagesDir := filepath.Join(a.Cfg.StaticDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "업로드 처리 중 오류가 발생했습니다.")
		return
	}
	filename := fmt.Sprintf("fanart_%s.%s", randomHex(16), ext)
	filePath := filepath.Join(imagesDir, filename)
	if err := saveMultipartFileTo(fh, filePath); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "업로드 처리 중 오류가 발생했습니다.")
		return
	}

	if newPath, ok, rejected := a.JavaImage.ProcessUploadedImage(filePath, 1920, 85); rejected {
		_ = os.Remove(filePath)
		httputil.JSONError(w, http.StatusBadRequest, "올바른 이미지 파일이 아닙니다.")
		return
	} else if ok && newPath != "" {
		filePath = newPath
		filename = filepath.Base(filePath)
	}

	modResult, _ := a.Moderation.Moderate(absPath(filePath))
	if modResult.Verdict == "block" {
		_ = os.Remove(filePath)
		httputil.JSONError(w, http.StatusBadRequest, "부적절한 콘텐츠로 판단되어 업로드가 거부되었습니다.")
		return
	}

	imageURL := "/static/images/" + filename
	thumbnailURL := a.ensureFanartThumbnail(imageURL)
	lqip := a.generateFanartLQIP(imageURL)

	fanartID, err := models.InsertFanart(a.DB, userID, nickname, imageURL, strToNull(title), strToNull(description), thumbnailURL, lqip, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if modResult.Verdict == "flag" {
		meta := models.FanartMeta{Title: strToNull(title), ImageURL: strToNull(imageURL), Nickname: strToNull(nickname)}
		_ = models.InsertFanartReport(a.DB, fanartID, 0, "AI 자동 탐지", modResult.Summary(), meta, "")
	}
	a.checkFanartDuplicate(fanartID, filePath, title, imageURL, nickname)
	a.Cache.Delete("api_fanart_latest")

	// [참여 유도: 포인트] AI 모더레이션이 "의심"으로 플래그한 건 검토 전이라
	// 포인트를 바로 안 주고, 문제없는 정상 업로드에만 준다.
	if modResult.Verdict != "flag" {
		if err := models.AwardPoints(a.DB, userID, "fanart_upload", models.PointsFanartUpload); err != nil {
			log.Printf("팬아트 업로드 포인트 적립 실패(user_id=%d): %v", userID, err)
		}
	}
	writeJSON(w, map[string]any{"success": true, "message": "업로드 성공!"})
}

func (a *App) checkFanartDuplicate(fanartID int64, filePath, title, imageURL, nickname string) {
	if !a.Phash.Enabled() {
		return
	}
	grayResult, ok := a.JavaImage.Call("/grayscale32", map[string]any{"path": absPath(filePath)})
	if !ok {
		return
	}
	grayB64, ok := grayResult["gray"].(string)
	if !ok {
		return
	}
	grayBytes, err := base64.StdEncoding.DecodeString(grayB64)
	if err != nil || len(grayBytes) != 1024 {
		return
	}
	hashHex, ok := a.Phash.Hash(grayBytes)
	if !ok {
		return
	}
	if similarID, distance, found, err := models.FindSimilarFanartPhash(a.DB, hashHex, a.Cfg.PhashDuplicateThreshold, fanartID); err == nil && found {
		meta := models.FanartMeta{Title: strToNull(title), ImageURL: strToNull(imageURL), Nickname: strToNull(nickname)}
		reason := fmt.Sprintf("AI 자동 탐지: 유사 이미지 재업로드 의심 (게시물 #%d와 유사, 거리 %d)", similarID, distance)
		_ = models.InsertFanartReport(a.DB, fanartID, 0, "AI 자동 탐지", reason, meta, "")
	}
	_ = models.InsertFanartPhash(a.DB, fanartID, hashHex)
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func (a *App) ensureFanartThumbnail(imageURL string) string {
	if !strings.HasPrefix(imageURL, "/static/images/") {
		return ""
	}
	filename := imageURL[strings.LastIndex(imageURL, "/")+1:]
	name, ext := splitExt(filename)
	if strings.ToLower(ext) == "gif" || strings.HasSuffix(name, "_thumb") {
		return ""
	}
	imagesDir := filepath.Join(a.Cfg.StaticDir, "images")
	thumbFilename := name + "_thumb.webp"
	thumbPath := filepath.Join(imagesDir, thumbFilename)
	thumbURL := "/static/images/" + thumbFilename
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbURL
	}
	fullPath := filepath.Join(imagesDir, filename)
	if _, err := os.Stat(fullPath); err != nil {
		return ""
	}
	absFull, _ := filepath.Abs(fullPath)
	absThumb, _ := filepath.Abs(thumbPath)
	if _, ok := a.JavaImage.Call("/thumbnail", map[string]any{
		"path": absFull, "outputPath": absThumb, "maxDimension": 480, "quality": 75,
		"avif": true, "avifQuality": 60,
	}); ok {
		return thumbURL
	}
	return ""
}

func (a *App) generateFanartLQIP(imageURL string) string {
	if !strings.HasPrefix(imageURL, "/static/images/") {
		return ""
	}
	filename := imageURL[strings.LastIndex(imageURL, "/")+1:]
	_, ext := splitExt(filename)
	if strings.ToLower(ext) == "gif" {
		return ""
	}
	fullPath := filepath.Join(a.Cfg.StaticDir, "images", filename)
	if _, err := os.Stat(fullPath); err != nil {
		return ""
	}
	absFull, _ := filepath.Abs(fullPath)
	result, ok := a.JavaImage.Call("/lqip", map[string]any{"path": absFull, "box": 16, "quality": 40})
	if !ok {
		return ""
	}
	if dataURI, ok := result["dataUri"].(string); ok {
		return dataURI
	}
	return ""
}

func splitExt(filename string) (name, ext string) {
	idx := strings.LastIndex(filename, ".")
	if idx == -1 {
		return filename, ""
	}
	return filename[:idx], filename[idx+1:]
}

func (a *App) ApiUpdateFanartHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	var body struct {
		ID          any    `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	_ = decodeJSONBody(r, &body)
	fanartID, ok := anyToInt64(body.ID)
	if !ok || fanartID == 0 {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	newTitle := strings.TrimSpace(body.Title)
	if len(newTitle) > 50 {
		newTitle = newTitle[:50]
	}
	newDesc := strings.TrimSpace(body.Description)

	ownerID, found, err := models.GetFanartOwner(a.DB, fanartID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if !found {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 게시물입니다.")
		return
	}
	if ownerID != userID {
		httputil.JSONError(w, http.StatusForbidden, "본인이 업로드한 사진만 수정할 수 있습니다.")
		return
	}
	if err := models.UpdateFanart(a.DB, fanartID, strToNull(newTitle), strToNull(newDesc)); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "수정되었습니다."})
}

func (a *App) deleteFanartImageFiles(imageURL string) {
	if !strings.HasPrefix(imageURL, "/static/images/") {
		return
	}
	fullFilePath := filepath.Join(a.Cfg.ProjectDir, strings.TrimPrefix(imageURL, "/"))
	_ = os.Remove(fullFilePath)
	filename := imageURL[strings.LastIndex(imageURL, "/")+1:]
	name, ext := splitExt(filename)
	if strings.ToLower(ext) != "gif" && !strings.HasSuffix(name, "_thumb") {
		thumbPath := filepath.Join(a.Cfg.StaticDir, "images", name+"_thumb.webp")
		_ = os.Remove(thumbPath)
	}
}

func (a *App) ApiReactFanartHandler(w http.ResponseWriter, r *http.Request) {
	fanartID, err := strconv.ParseInt(chi.URLParam(r, "fanartID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	myNickname := ""
	if sess := middleware.GetSession(r); sess != nil {
		myNickname = sess.GetString("user_nickname")
	}
	var body struct {
		Reaction string `json:"reaction"`
		Reason   string `json:"reason"`
	}
	_ = decodeJSONBody(r, &body)

	if body.Reaction == "delete" {
		if !a.isAdmin(r) {
			httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
			return
		}
		if imageURL, found, err := models.FanartImageURL(a.DB, fanartID); err == nil && found {
			a.deleteFanartImageFiles(imageURL)
		}
		if err := models.DeleteFanartFully(a.DB, fanartID); err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
			return
		}
		a.Cache.Delete("api_fanart_latest")
		a.logAdminAction(r, "delete_fanart", "fanart", strconv.FormatInt(fanartID, 10), "")
		writeJSON(w, map[string]any{"success": true, "message": "서버와 DB에서 완전히 삭제되었습니다."})
		return
	}

	if body.Reaction != "like" && body.Reaction != "dislike" && body.Reaction != "report" {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	exists, err := models.FanartReactionExists(a.DB, fanartID, userID, body.Reaction)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if exists {
		writeJSON(w, map[string]any{"success": false, "message": "이미 반응을 남기셨습니다."})
		return
	}
	if err := models.InsertFanartReaction(a.DB, fanartID, userID, body.Reaction, httputil.GetClientIP(r)); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if body.Reaction == "like" {
		if ownerID, found, oerr := models.GetFanartOwner(a.DB, fanartID); oerr == nil && found {
			nickname := myNickname
			if nickname == "" {
				nickname = "스텔리언"
			}
			_ = a.CreateNotify(ownerID, userID, nickname, "like_on_fanart", "fanart", fanartID, "")
		}
	}
	if body.Reaction == "report" {
		reason := strings.TrimSpace(body.Reason)
		if reason == "" {
			reason = "사유 미입력"
		}
		if len(reason) > 100 {
			reason = reason[:100]
		}
		meta, _, _ := models.GetFanartMeta(a.DB, fanartID)
		_ = models.InsertFanartReport(a.DB, fanartID, userID, myNickname, reason, meta, httputil.GetClientIP(r))
		cnt, err := models.CountFanartReports(a.DB, fanartID)
		if err == nil && cnt >= 3 {
			if imageURL, found, err := models.FanartImageURL(a.DB, fanartID); err == nil && found {
				a.deleteFanartImageFiles(imageURL)
			}
			_ = models.DeleteFanartFully(a.DB, fanartID)
			_ = models.MarkFanartReportsAutoDeleted(a.DB, fanartID)
		}
	}
	a.Cache.Delete("api_fanart_latest")
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiGetFanartCommentsHandler(w http.ResponseWriter, r *http.Request) {
	fanartID, err := strconv.ParseInt(chi.URLParam(r, "fanartID"), 10, 64)
	if err != nil {
		httputil.JSON(w, http.StatusBadRequest, map[string]any{"success": false, "comments": []any{}})
		return
	}
	rows, err := models.GetFanartComments(a.DB, fanartID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "comments": []any{}})
		return
	}
	viewerID := sessionUserID(r)
	comments := make([]map[string]any, len(rows))
	for i, c := range rows {
		nickname := c.Nickname.String
		if nickname == "" {
			nickname = "스텔리언"
		}
		var userIDOut any
		if c.UserID.Valid {
			userIDOut = c.UserID.Int64
		}
		comments[i] = map[string]any{
			"id": c.ID, "user_id": userIDOut, "nickname": nickname,
			"picture": c.Picture.String, "content": c.Content, "date": formatDateShortLocal(c.CreatedAt),
			"is_mine": viewerID != 0 && c.UserID.Valid && c.UserID.Int64 == viewerID,
		}
	}
	writeJSON(w, map[string]any{"success": true, "comments": comments})
}

func (a *App) ApiAddFanartCommentHandler(w http.ResponseWriter, r *http.Request) {
	fanartID, err := strconv.ParseInt(chi.URLParam(r, "fanartID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	_ = decodeJSONBody(r, &body)
	content := strings.TrimSpace(body.Content)
	if len(content) > 500 {
		content = content[:500]
	}
	if content == "" {
		httputil.JSONError(w, http.StatusBadRequest, "내용을 입력해주세요.")
		return
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인 정보가 만료되었습니다.")
		return
	}
	exists, err := models.FanartExists(a.DB, fanartID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "댓글 작성 중 오류가 발생했습니다.")
		return
	}
	if !exists {
		httputil.JSONError(w, http.StatusNotFound, "게시물을 찾을 수 없습니다.")
		return
	}
	newID, err := models.AddFanartComment(a.DB, fanartID, userID, user.NicknameOr("스텔리언"), user.Picture.String, content, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "댓글 작성 중 오류가 발생했습니다.")
		return
	}
	if ownerID, found, oerr := models.GetFanartOwner(a.DB, fanartID); oerr == nil && found {
		_ = a.CreateNotify(ownerID, userID, user.NicknameOr("스텔리언"),
			"comment_on_fanart", "fanart", fanartID, content)
	}

	// [참여 유도: 포인트] 팬아트 댓글 작성 시 포인트 적립.
	if err := models.AwardPoints(a.DB, userID, "comment", models.PointsComment); err != nil {
		log.Printf("팬아트 댓글 포인트 적립 실패(user_id=%d): %v", userID, err)
	}

	writeJSON(w, map[string]any{"success": true, "id": newID, "message": "댓글이 등록되었습니다."})
}

func (a *App) ApiDeleteFanartCommentHandler(w http.ResponseWriter, r *http.Request) {
	commentID, err := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	ownerID, found, err := models.GetFanartCommentOwner(a.DB, commentID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "삭제 중 오류가 발생했습니다.")
		return
	}
	if !found {
		httputil.JSONError(w, http.StatusNotFound, "댓글을 찾을 수 없습니다.")
		return
	}
	isAdminOverride := ownerID != userID
	if isAdminOverride && !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "본인 댓글만 삭제할 수 있습니다.")
		return
	}
	if err := models.DeleteFanartComment(a.DB, commentID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "삭제 중 오류가 발생했습니다.")
		return
	}
	if isAdminOverride {
		a.logAdminAction(r, "delete_fanart_comment", "fanart_comment", strconv.FormatInt(commentID, 10), "")
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) GalleryPageHandler(w http.ResponseWriter, r *http.Request) {
	a.Index(w, r)
}

func (a *App) ApiGetFanartHandler(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "api_fanart_latest"
	if cached, ok := a.Cache.Get(cacheKey); ok {
		writeJSON(w, cached)
		return
	}
	fanarts, err := models.GetFanartLatest(a.DB)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	result := map[string]any{"success": true, "fanarts": fanarts}
	a.Cache.Set(cacheKey, result, 60*time.Second)
	writeJSON(w, result)
}

func (a *App) ApiGetMyFanartHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	fanarts, err := models.GetFanartByUser(a.DB, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true, "fanarts": fanarts})
}

func anyToInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func saveMultipartFileTo(fh *multipart.FileHeader, destPath string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func formatDateShortLocal(s string) string {
	s = strings.TrimSuffix(s, "Z")
	s = strings.Replace(s, "T", " ", 1)
	if len(s) > 16 {
		s = s[:16]
	}
	return s
}
