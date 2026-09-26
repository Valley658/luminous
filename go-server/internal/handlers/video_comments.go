package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/imgvalidate"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

func (a *App) ApiReactToCommentHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다")
		return
	}
	commentID, err := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다")
		return
	}
	var body struct {
		Reaction string `json:"reaction"`
	}
	_ = decodeJSONBody(r, &body)
	if body.Reaction != "like" && body.Reaction != "dislike" {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다")
		return
	}

	exists, err := models.CommentExists(a.DB, commentID)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if !exists {
		httputil.JSONError(w, http.StatusNotFound, "댓글을 찾을 수 없습니다")
		return
	}

	likeCount, dislikeCount, myReaction, err := models.ReactToComment(a.DB, commentID, userID, body.Reaction, httputil.GetClientIP(r))
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if myReaction == "like" {
		if ownerID, found, oerr := models.GetVideoCommentOwner(a.DB, commentID); oerr == nil && found {
			nickname := "스텔리언"
			if sess != nil {
				if n := sess.GetString("user_nickname"); n != "" {
					nickname = n
				}
			}
			_ = a.CreateNotify(ownerID, userID, nickname, "like_on_comment", "comment", commentID, "")
		}
	}
	var myReactionOut any
	if myReaction != "" {
		myReactionOut = myReaction
	}
	writeJSON(w, map[string]any{"success": true, "like_count": likeCount, "dislike_count": dislikeCount, "my_reaction": myReactionOut})
}

func (a *App) ApiGetCommentsHandler(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	comments, err := models.GetVideoComments(a.DB, videoID, userID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "처리 중 오류가 발생했습니다.", "comments": []any{}})
		return
	}
	writeJSON(w, map[string]any{"success": true, "comments": comments})
}

func (a *App) ApiAddCommentHandler(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	sess := middleware.GetSession(r)
	userID := int64(0)
	if sess != nil {
		userID = sess.GetInt64("user_id")
	}
	if userID == 0 {
		httputil.JSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "댓글을 작성하려면 로그인이 필요합니다.", "login_required": true})
		return
	}
	user, err := models.GetUserByID(a.DB, userID)
	if err != nil || user == nil {
		sess.Clear()
		httputil.JSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "로그인 정보가 만료되었습니다. 다시 로그인해주세요.", "login_required": true})
		return
	}
	nickname := user.NicknameOr("스텔리언")
	if nickname == "" {
		nickname = "스텔리언"
	}

	isMultipart := strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data")
	var content string
	if isMultipart {
		_ = r.ParseMultipartForm(32 << 20)
		content = r.FormValue("content")
	} else {
		var body struct {
			Content string `json:"content"`
		}
		_ = decodeJSONBody(r, &body)
		content = body.Content
	}
	content = strings.TrimSpace(sanitizeUserHTML(content))
	if content == "" {
		httputil.JSONError(w, http.StatusBadRequest, "내용 누락")
		return
	}

	userIP := strings.TrimSpace(httputil.GetClientIP(r))
	userEmail := user.Email.String
	isDeveloper := (devEmail != "" && userEmail == devEmail) || (userIP != "" && devBypassIPs[userIP])
	if !isDeveloper {
		if devNicknames[nickname] || strings.Contains(nickname, "개발자") || strings.Contains(nickname, "디자인담당") {
			nickname = "일반스텔리언"
		}
	}

	var imageURL sql.NullString
	var queuedGifPath string
	if isMultipart && r.MultipartForm != nil {
		if files := r.MultipartForm.File["image"]; len(files) > 0 && files[0].Filename != "" {
			fh := files[0]
			// [2026-09-26 보안 점검] 확장자/Content-Type을 신뢰하지 않고,
			// gif든 아니든 예외 없이 imgvalidate로 매직바이트+전체 디코드
			// 검증 후 재인코딩된 파일만 사용한다 (예전엔 gif 확장자면
			// 이 검증을 통째로 건너뛰고 원본을 그대로 저장/서빙했음).
			if fh.Size <= 0 || fh.Size > maxUploadImageBytes {
				writeJSON(w, map[string]any{"success": false, "message": "이미지 파일 크기가 올바르지 않습니다(최대 15MB)."})
				return
			}
			uploadDir := filepath.Join(a.Cfg.StaticDir, "uploads", "comments")
			if err := os.MkdirAll(uploadDir, 0o755); err != nil {
				httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
				return
			}
			tmpPath := filepath.Join(uploadDir, ".upload_"+imgvalidate.RandomBaseName("tmp", 16))
			if err := saveMultipartFileTo(fh, tmpPath); err != nil {
				httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
				return
			}
			validated, verr := imgvalidate.ValidateAndReencode(tmpPath, uploadDir, imgvalidate.RandomBaseName("comment", 16), 88)
			_ = os.Remove(tmpPath)
			if verr != nil {
				writeJSON(w, map[string]any{"success": false, "message": "올바른 이미지 파일이 아닙니다(jpg/png/webp/gif만 허용)."})
				return
			}
			filename := filepath.Base(validated.Path)
			if validated.Format == "gif" {
				imageURL = sql.NullString{String: "/static/uploads/comments/" + filename, Valid: true}
				queuedGifPath = validated.Path
			} else {
				finalCommentImgPath := validated.Path
				if newPath, ok, optimized := a.JavaImage.ProcessUploadedImage(finalCommentImgPath, 1920, 85); optimized && ok && newPath != "" {
					filename = filepath.Base(newPath)
					finalCommentImgPath = newPath
				}
				if modResult, _ := a.Moderation.Moderate(absPath(finalCommentImgPath)); modResult.Verdict == "block" {
					_ = os.Remove(finalCommentImgPath)
					writeJSON(w, map[string]any{"success": false, "message": "부적절한 콘텐츠로 판단되어 업로드가 거부되었습니다."})
					return
				}
				imageURL = sql.NullString{String: "/static/uploads/comments/" + filename, Valid: true}
			}

		}
	}

	commentID, err := models.InsertVideoComment(a.DB, videoID, nickname, content, user.Picture, userID, userIP, imageURL)
	if err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	if queuedGifPath != "" {
		id := commentID
		if !a.JavaImage.QueueGifToWebp(queuedGifPath, 85, func(newPath string, ok bool) {
			if !ok || newPath == "" {
				return
			}
			newURL := "/static/uploads/comments/" + filepath.Base(newPath)
			_ = models.UpdateVideoCommentImage(a.DB, id, newURL)
		}) {

			log.Printf("[정보] 댓글 GIF 변환 큐가 가득 참 - comment_id=%d는 원본 gif 유지", id)
		}
	}

	// [참여 유도: 포인트] 영상 댓글 작성 시 포인트 적립.
	if err := models.AwardPoints(a.DB, userID, "comment", models.PointsComment); err != nil {
		log.Printf("영상 댓글 포인트 적립 실패(user_id=%d): %v", userID, err)
	}

	writeJSON(w, map[string]any{"success": true, "message": "등록 완료"})
}
