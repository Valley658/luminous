package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

func (a *App) AdminStatsPageHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if err := a.Templates.Render(w, r, "admin_stats.html", map[string]any{}, a.GenRepImageOverrides); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false})
	}
}

func (a *App) ApiAdminStatsHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	overview, err := models.GetAdminStatsOverview(a.DB)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	topKeywords := make([]map[string]any, len(overview.TopKeywords))
	for i, k := range overview.TopKeywords {
		topKeywords[i] = map[string]any{"keyword": k.Keyword, "search_count": k.Count}
	}
	fanartDaily := make([]map[string]any, len(overview.FanartDaily))
	for i, d := range overview.FanartDaily {
		fanartDaily[i] = map[string]any{"date": d.Date, "count": d.Count}
	}
	commentsDaily := make([]map[string]any, len(overview.CommentsDaily))
	for i, d := range overview.CommentsDaily {
		commentsDaily[i] = map[string]any{"date": d.Date, "count": d.Count}
	}
	reportsByStatus := make([]map[string]any, len(overview.ReportsByStatus))
	for i, s := range overview.ReportsByStatus {
		reportsByStatus[i] = map[string]any{"status": s.Status, "c": s.Count}
	}
	cheersByMember := make([]map[string]any, len(overview.CheersByMember))
	for i, m := range overview.CheersByMember {
		cheersByMember[i] = map[string]any{"member_name": m.MemberName, "c": m.Count}
	}
	writeJSON(w, map[string]any{
		"success": true, "top_keywords": topKeywords, "fanart_daily": fanartDaily,
		"comments_daily": commentsDaily, "reports_by_status": reportsByStatus, "cheers_by_member": cheersByMember,
		"totals": map[string]any{
			"total_users": overview.TotalUsers, "new_users_14d": overview.NewUsers14d,
			"total_fanart": overview.TotalFanart, "total_comments": overview.TotalComments,
		},
	})
}

func (a *App) ApiAdminServerLogHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	errorsOnly := r.URL.Query().Get("errors_only") == "1"
	logPath := filepath.Join(a.Cfg.ProjectDir, "logs", "service.log")
	info, err := os.Stat(logPath)
	if err != nil {
		writeJSON(w, map[string]any{"success": true, "lines": []any{}, "size": 0})
		return
	}
	size := info.Size()
	const maxBytes = 300 * 1024
	f, err := os.Open(logPath)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	defer f.Close()
	if size > maxBytes {
		if _, err := f.Seek(size-maxBytes, 0); err != nil {
			httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
			return
		}

		buf := make([]byte, 1)
		for {
			n, rerr := f.Read(buf)
			if n > 0 && buf[0] == '\n' {
				break
			}
			if rerr != nil {
				break
			}
		}
	}
	content, err := readAllString(f)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if errorsOnly {
		filtered := lines[:0:0]
		for _, ln := range lines {
			if strings.Contains(ln, "[ERROR]") || strings.Contains(ln, "Traceback") || strings.Contains(ln, " Error") {
				filtered = append(filtered, ln)
			}
		}
		lines = filtered
	}
	if len(lines) > 400 {
		lines = lines[len(lines)-400:]
	}
	linesOut := make([]any, len(lines))
	for i, ln := range lines {
		linesOut[i] = ln
	}
	writeJSON(w, map[string]any{"success": true, "lines": linesOut, "size": size})
}

func readAllString(f *os.File) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String(), nil
}

var logRotatedPattern = regexp.MustCompile(`\.log\.\d+$`)

func (a *App) ApiAdminLogsResetHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	logsDir := filepath.Join(a.Cfg.ProjectDir, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		writeJSON(w, map[string]any{"success": true, "truncated": []any{}, "removed": []any{}})
		return
	}
	var truncated, removed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		fpath := filepath.Join(logsDir, name)
		if strings.HasSuffix(name, ".log") {
			if f, err := os.Create(fpath); err == nil {
				f.Close()
				truncated = append(truncated, name)
			}
		} else if logRotatedPattern.MatchString(name) {
			if os.Remove(fpath) == nil {
				removed = append(removed, name)
			}
		}
	}
	if truncated == nil {
		truncated = []string{}
	}
	if removed == nil {
		removed = []string{}
	}
	writeJSON(w, map[string]any{"success": true, "truncated": truncated, "removed": removed})
}

func (a *App) ApiAdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			page = n
		}
	}
	if page < 1 {
		page = 1
	}
	perPage := 30
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			perPage = n
		}
	}
	if perPage < 1 {
		perPage = 1
	} else if perPage > 100 {
		perPage = 100
	}

	users, total, err := models.ListAdminUsers(a.DB, q, page, perPage)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	out := make([]map[string]any, len(users))
	for i, u := range users {
		out[i] = u.ToMap()
	}
	writeJSON(w, map[string]any{"success": true, "users": out, "total": total, "page": page, "per_page": perPage})
}

func (a *App) ApiAdminDeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	targetUserID, err := strconv.ParseInt(chi.URLParam(r, "targetUserID"), 10, 64)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	email, nickname, found, err := models.GetUserBasicInfo(a.DB, targetUserID)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	if !found {
		httputil.JSONError(w, http.StatusNotFound, "존재하지 않는 회원입니다.")
		return
	}
	if err := models.AdminDeleteUser(a.DB, targetUserID); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	a.logAdminAction(r, "delete_user", "user", strconv.FormatInt(targetUserID, 10), "email="+email+", nickname="+nickname)
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) ApiAdminDBOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	groups := models.GetAdminDBOverview(a.DB)
	out := make([]map[string]any, len(groups))
	for i, g := range groups {
		tables := make([]map[string]any, len(g.Tables))
		for j, t := range g.Tables {
			var countOut any
			if t.Count != nil {
				countOut = *t.Count
			}
			tables[j] = map[string]any{"name": t.Name, "count": countOut}
		}
		out[i] = map[string]any{"label": g.Label, "tables": tables}
	}
	writeJSON(w, map[string]any{"success": true, "groups": out})
}

func (a *App) ApiAdminAuditLogHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	logs, err := models.GetAdminAuditLog(a.DB)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true, "logs": logs})
}

func (a *App) ApiGetAllCommentsHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	comments, err := models.ListAllVideoComments(a.DB)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	writeJSON(w, map[string]any{"success": true, "comments": comments})
}

func (a *App) ApiDeleteCommentsHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	var body struct {
		IDs []any `json:"ids"`
	}
	_ = decodeJSONBody(r, &body)
	if len(body.IDs) == 0 {
		httputil.JSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Empty list"})
		return
	}
	ids := make([]int64, 0, len(body.IDs))
	idStrs := make([]string, 0, len(body.IDs))
	for _, v := range body.IDs {
		if id, ok := anyToInt64(v); ok {
			ids = append(ids, id)
			idStrs = append(idStrs, strconv.FormatInt(id, 10))
		}
	}
	if err := models.DeleteVideoComments(a.DB, ids); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	a.logAdminAction(r, "delete_comments", "video_comment", strings.Join(idStrs, ","), strconv.Itoa(len(ids))+"건 삭제")
	writeJSON(w, map[string]any{"success": true})
}
