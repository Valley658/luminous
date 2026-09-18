package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/javaimage"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

func (a *App) GetCheersHandler(w http.ResponseWriter, r *http.Request) {
	memberName := chi.URLParam(r, "memberName")
	cheers, err := models.GetCheers(a.DB, memberName)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	out := make([]map[string]any, len(cheers))
	for i, c := range cheers {
		out[i] = map[string]any{"nickname": c.Nickname, "message": c.Message, "time": c.Time}
	}
	writeJSON(w, map[string]any{"success": true, "cheers": out})
}

func (a *App) AddCheerHandler(w http.ResponseWriter, r *http.Request) {
	memberName := chi.URLParam(r, "memberName")
	var body struct {
		Message string `json:"message"`
	}
	_ = decodeJSONBody(r, &body)
	message := strings.TrimSpace(body.Message)
	if message == "" {
		httputil.JSONError(w, http.StatusBadRequest, "메시지를 입력해주세요.")
		return
	}
	sess := middleware.GetSession(r)
	userID := int64(0)
	nickname := "익명스텔리언"
	if sess != nil {
		userID = sess.GetInt64("user_id")
		if n := sess.GetString("user_nickname"); n != "" {
			nickname = n
		}
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인 후 이용할 수 있습니다.")
		return
	}
	if err := models.AddCheer(a.DB, memberName, nickname, message, httputil.GetClientIP(r)); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) CommunityPageHandler(w http.ResponseWriter, r *http.Request) {
	memberName := chi.URLParam(r, "memberName")
	exists, err := models.MemberExists(a.DB, memberName)
	if err != nil || !exists {
		http.NotFound(w, r)
		return
	}
	a.Index(w, r)
}

func (a *App) GoToMemberHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Requested-With") != "LuminousXHR" {
		http.NotFound(w, r)
		return
	}
	memberName := strings.TrimSpace(r.URL.Query().Get("name"))
	if memberName == "" {
		httputil.JSONError(w, http.StatusBadRequest, "멤버 이름이 필요합니다.")
		return
	}
	exists, err := models.MemberExists(a.DB, memberName)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
		return
	}
	if !exists {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 멤버입니다.")
		return
	}
	writeJSON(w, map[string]any{"success": true, "redirect": "/?view_member=" + memberName})
}

var secureFilenameDisallowed = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func secureFilename(name string) string {
	name = filepath.Base(name)
	name = secureFilenameDisallowed.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._")
	if name == "" {
		return "file"
	}
	return name
}

func communityImageAllowedExt(ext string) bool {
	return javaimage.IsAllowedImageExt(ext)
}

func (a *App) saveAndProcessCommunityImage(fh *multipart.FileHeader) (webPath string, rejected bool, err error) {
	ext := ""
	if idx := strings.LastIndex(fh.Filename, "."); idx != -1 {
		ext = strings.ToLower(fh.Filename[idx+1:])
	}
	if !communityImageAllowedExt(ext) {
		return "", true, nil
	}

	uploadDir := filepath.Join(a.Cfg.ProjectDir, "static", "uploads", "community")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return "", false, err
	}
	filename := fmt.Sprintf("%s_%s", time.Now().Format("20060102150405"), secureFilename(fh.Filename))
	savePath := filepath.Join(uploadDir, filename)

	src, err := fh.Open()
	if err != nil {
		return "", false, err
	}
	defer src.Close()
	dst, err := os.Create(savePath)
	if err != nil {
		return "", false, err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return "", false, err
	}
	dst.Close()

	finalPath := savePath
	if newPath, ok, rej := a.JavaImage.ProcessUploadedImage(savePath, 1920, 85); rej {
		_ = os.Remove(savePath)
		return "", true, nil
	} else if ok && newPath != "" {
		finalPath = newPath
	}

	if modResult, _ := a.Moderation.Moderate(absPath(finalPath)); modResult.Verdict == "block" {
		_ = os.Remove(finalPath)
		return "", true, nil
	} else if modResult.Verdict == "flag" {
		log.Printf("[모더레이션] 커뮤니티 이미지 애매 판정 (수동 확인 필요): %s - %s", finalPath, modResult.Summary())
	}

	rel, err := filepath.Rel(a.Cfg.ProjectDir, finalPath)
	if err != nil {
		rel = filepath.Base(finalPath)
	}
	return "/" + filepath.ToSlash(rel), false, nil
}

func (a *App) CreateCommunityPostHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		writeJSON(w, map[string]any{"success": false, "message": "로그인 후 이용할 수 있습니다."})
		return
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {
		writeJSON(w, map[string]any{"success": false, "message": "로그인 정보가 만료되었습니다. 다시 로그인해주세요."})
		return
	}
	authorName := user.NicknameOr("스텔리언")
	authorPicture := user.Picture.String

	if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
		writeJSON(w, map[string]any{"success": false, "message": "요청을 처리할 수 없습니다."})
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if len(title) > 200 {
		title = title[:200]
	}
	content := strings.TrimSpace(r.FormValue("content"))
	memberName := r.FormValue("member_name")
	if content == "" {
		writeJSON(w, map[string]any{"success": false, "message": "내용을 입력해주세요."})
		return
	}
	if memberName == "" {
		writeJSON(w, map[string]any{"success": false, "message": "게시물을 올릴 멤버를 선택해주세요."})
		return
	}
	exists, err := models.MemberExists(a.DB, memberName)
	if err != nil || !exists {
		writeJSON(w, map[string]any{"success": false, "message": "존재하지 않는 멤버입니다."})
		return
	}

	var imagePaths []string
	if r.MultipartForm != nil {
		files := r.MultipartForm.File["images"]
		if len(files) > 10 {
			files = files[:10]
		}
		for _, fh := range files {
			webPath, rejected, err := a.saveAndProcessCommunityImage(fh)
			if err != nil {
				writeJSON(w, map[string]any{"success": false, "message": "이미지 처리 중 오류가 발생했습니다."})
				return
			}
			if rejected {
				writeJSON(w, map[string]any{"success": false, "message": "이미지는 jpg/png/webp/gif/heic 형식만 업로드할 수 있습니다."})
				return
			}
			imagePaths = append(imagePaths, webPath)
		}
		if videoFiles := r.MultipartForm.File["video"]; len(videoFiles) > 0 {
			httputil.JSONError(w, http.StatusServiceUnavailable, "동영상 첨부는 현재 잠시 서비스를 이용할 수 없습니다.")
			return
		}
	}
	imageJSON, _ := json.Marshal(imagePaths)

	if err := models.CreateCommunityPost(a.DB, memberName, userID, authorName, authorPicture, title, content, string(imageJSON), sql.NullString{}, httputil.GetClientIP(r)); err != nil {
		writeJSON(w, map[string]any{"success": false, "message": "글 작성 중 오류가 발생했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "게시물이 작성되었습니다."})
}

func (a *App) GetCommunityPostsHandler(w http.ResponseWriter, r *http.Request) {
	memberName := chi.URLParam(r, "memberName")
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "latest"
	}
	viewerID := sessionUserID(r)
	posts, err := models.GetCommunityPosts(a.DB, memberName, sortBy, viewerID)
	if err != nil {
		writeJSON(w, map[string]any{"success": false, "posts": []any{}})
		return
	}
	writeJSON(w, map[string]any{"success": true, "posts": posts})
}

func (a *App) ApiCommunityLikeHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}
	var body struct {
		TargetType string `json:"target_type"`
		Reaction   string `json:"reaction"`
		TargetID   any    `json:"target_id"`
	}
	_ = decodeJSONBody(r, &body)
	targetType := body.TargetType
	if targetType == "" {
		targetType = "post"
	}
	if targetType != "post" && targetType != "comment" && targetType != "video" {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	if body.Reaction != "like" && body.Reaction != "dislike" {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}

	var targetIDInt int64
	var targetIDStr string
	if targetType == "video" {
		targetIDStr = strings.TrimSpace(fmt.Sprintf("%v", body.TargetID))
		if targetIDStr == "" || targetIDStr == "<nil>" || len(targetIDStr) > 100 {
			httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
			return
		}
	} else {
		switch v := body.TargetID.(type) {
		case float64:
			targetIDInt = int64(v)
		case string:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
				return
			}
			targetIDInt = n
		default:
			httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
			return
		}
		exists, err := models.CommunityTargetExists(a.DB, targetType, targetIDInt)
		if err != nil {
			httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
			return
		}
		if !exists {
			httputil.JSONError(w, http.StatusNotFound, "대상을 찾을 수 없습니다.")
			return
		}
	}

	likeCount, dislikeCount, myReaction, err := models.CommunityLikeTarget(a.DB, targetType, targetIDInt, targetIDStr, userID, body.Reaction, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if myReaction == "like" && targetType == "post" {
		if ownerID, found, oerr := models.GetCommunityPostOwner(a.DB, targetIDInt); oerr == nil && found {
			nickname := "스텔리언"
			if u, uerr := models.GetUserByID(a.DB, userID); uerr == nil && u != nil {
				nickname = u.NicknameOr("스텔리언")
			}
			_ = models.CreateNotification(a.DB, ownerID, userID, nickname, "like_on_post", "post", targetIDInt, "")
		}
	}
	var myReactionOut any
	if myReaction != "" {
		myReactionOut = myReaction
	}
	writeJSON(w, map[string]any{"success": true, "like_count": likeCount, "dislike_count": dislikeCount, "my_reaction": myReactionOut})
}

func (a *App) ApiGetVideoReactionHandler(w http.ResponseWriter, r *http.Request) {
	videoID := strings.TrimSpace(chi.URLParam(r, "videoID"))
	if len(videoID) > 100 {
		videoID = videoID[:100]
	}
	if videoID == "" {
		writeJSON(w, map[string]any{"success": false, "like_count": 0, "dislike_count": 0, "my_reaction": nil})
		return
	}
	userID := sessionUserID(r)
	likeCount, dislikeCount, myReaction, err := models.GetVideoReaction(a.DB, videoID, userID)
	if err != nil {
		writeJSON(w, map[string]any{"success": false, "like_count": 0, "dislike_count": 0, "my_reaction": nil})
		return
	}
	var myReactionOut any
	if myReaction != "" {
		myReactionOut = myReaction
	}
	writeJSON(w, map[string]any{"success": true, "like_count": likeCount, "dislike_count": dislikeCount, "my_reaction": myReactionOut})
}

func (a *App) ApiGetCommunityCommentsHandler(w http.ResponseWriter, r *http.Request) {
	postID, err := strconv.ParseInt(chi.URLParam(r, "postID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	viewerID := sessionUserID(r)
	comments, err := models.GetCommunityComments(a.DB, postID, viewerID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "comments": []any{}})
		return
	}
	writeJSON(w, map[string]any{"success": true, "comments": comments})
}

func (a *App) ApiAddCommunityCommentHandler(w http.ResponseWriter, r *http.Request) {
	postID, err := strconv.ParseInt(chi.URLParam(r, "postID"), 10, 64)
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
	if content == "" {
		httputil.JSONError(w, http.StatusBadRequest, "내용을 입력해주세요.")
		return
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인 정보가 만료되었습니다.")
		return
	}
	newID, postExists, err := models.AddCommunityComment(a.DB, postID, userID, user.NicknameOr("스텔리언"), user.Picture.String, content, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "댓글 작성 중 오류가 발생했습니다.")
		return
	}
	if !postExists {
		httputil.JSONError(w, http.StatusNotFound, "게시물을 찾을 수 없습니다.")
		return
	}
	if ownerID, found, oerr := models.GetCommunityPostOwner(a.DB, postID); oerr == nil && found {
		_ = models.CreateNotification(a.DB, ownerID, userID, user.NicknameOr("스텔리언"),
			"comment_on_post", "post", postID, content)
	}
	writeJSON(w, map[string]any{"success": true, "id": newID, "message": "댓글이 등록되었습니다."})
}

func (a *App) ApiDeleteCommunityCommentHandler(w http.ResponseWriter, r *http.Request) {
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
	ownerID, found, err := models.GetCommunityCommentOwner(a.DB, commentID)
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
	if err := models.DeleteCommunityComment(a.DB, commentID); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "삭제 중 오류가 발생했습니다.")
		return
	}
	if isAdminOverride {
		a.logAdminAction(r, "delete_community_comment", "community_comment", strconv.FormatInt(commentID, 10), "")
	}
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) AllCommentsPageHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/stats", http.StatusFound)
}
