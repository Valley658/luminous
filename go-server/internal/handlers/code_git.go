package handlers

// code_git.go - /code 코드 탐색기의 GitHub식 부가 기능
//   /api/code/commits  커밋 목록(경로별 기록 포함)
//   /api/code/commit   커밋 한 개의 변경 내용(diff)
//   /api/code/blame    줄마다 마지막으로 바꾼 커밋(Blame)
//
// 모두 GitHub에 push된 커밋(code.go의 codeSnapshot) 기준이며, 사용자가 보낸 값은
// "이 저장소 기록에 실제로 있는 커밋 해시 / 트리에 있는 경로"인지 확인한 뒤에만 git에 넘긴다.

import (
	"bufio"
	"bytes"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pastellive/internal/httputil"
)

const (
	codeCommitsPerPage = 30
	codePatchMaxBytes  = 3 << 20   // 커밋 diff 최대 크기(넘으면 잘라서 보여줌)
	codeBlameMaxBytes  = 512 << 10 // Blame을 계산할 최대 파일 크기
)

var codeHashRe = regexp.MustCompile(`^[0-9a-f]{4,40}$`)

func (a *App) codeValidPath(snap *codeSnapshot, p string) bool {
	if p == "" {
		return true
	}
	_, isFile := snap.byPath[p]
	return isFile || snap.dirs[p]
}

// 짧은 해시도 받아서, 이 저장소 기록 안에서 정확히 하나로 정해질 때만 전체 해시를 돌려준다.
func (a *App) codeResolveCommit(short string) (string, bool) {
	short = strings.ToLower(strings.TrimSpace(short))
	if !codeHashRe.MatchString(short) {
		return "", false
	}
	h, err := a.codeHistory()
	if err != nil {
		return "", false
	}
	found := ""
	for _, c := range h.All {
		if strings.HasPrefix(c, short) {
			if found != "" && found != c {
				return "", false
			}
			found = c
		}
	}
	return found, found != ""
}

type codeCommitRow struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
	Body    string `json:"body"`
}

func parseCodeLog(out []byte) []codeCommitRow {
	var rows []codeCommitRow
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.Trim(strings.ReplaceAll(rec, "\r", ""), "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 5)
		if len(f) < 4 {
			continue
		}
		row := codeCommitRow{Hash: f[0], Author: f[1], Date: f[2], Message: f[3]}
		if len(f) == 5 {
			row.Body = strings.TrimSpace(f[4])
		}
		rows = append(rows, row)
	}
	return rows
}

// GET /api/code/commits?path=&page=
func (a *App) ApiCodeCommitsHandler(w http.ResponseWriter, r *http.Request) {
	snap, err := a.codeSnapshot()
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "지금은 커밋을 불러올 수 없어요.")
		return
	}
	p := r.URL.Query().Get("path")
	if !a.codeValidPath(snap, p) {
		httputil.JSONError(w, http.StatusNotFound, "그런 파일이나 폴더가 없어요.")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000 {
		page = 1000
	}
	args := []string{"log", "--format=%H%x1f%an%x1f%cI%x1f%s%x1f%b%x1e",
		"--skip=" + strconv.Itoa((page-1)*codeCommitsPerPage), "-n", strconv.Itoa(codeCommitsPerPage + 1), snap.Commit}
	date := r.URL.Query().Get("date") // YYYY-MM-DD: 그날(한국 시간)의 커밋만 - 업데이트 로그에서 연결
	if date != "" {
		if !codeDateRe.MatchString(date) {
			httputil.JSONError(w, http.StatusBadRequest, "날짜 형식이 올바르지 않아요.")
			return
		}
		args = append(args, "--since="+date+"T00:00:00+09:00", "--until="+date+"T23:59:59+09:00")
	}
	if p != "" {
		args = append(args, "--", p)
	}
	out, err := a.runGitTimeout(30*time.Second, args...)
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "커밋을 불러오지 못했어요.")
		return
	}
	rows := parseCodeLog(out)
	more := len(rows) > codeCommitsPerPage
	if more {
		rows = rows[:codeCommitsPerPage]
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httputil.JSONOK(w, map[string]any{"commits": rows, "page": page, "has_more": more, "path": p, "date": date})
}

type codeChangedFile struct {
	Path   string `json:"path"`
	Add    int    `json:"add"`
	Del    int    `json:"del"`
	Binary bool   `json:"binary"`
	Status string `json:"status"` // A(추가) M(수정) D(삭제) ...
}

// GET /api/code/commit?hash=
func (a *App) ApiCodeCommitHandler(w http.ResponseWriter, r *http.Request) {
	full, ok := a.codeResolveCommit(r.URL.Query().Get("hash"))
	if !ok {
		httputil.JSONError(w, http.StatusNotFound, "그런 커밋이 없어요.")
		return
	}
	meta, err := a.runGit("show", "-s", "--format=%H%x1f%P%x1f%an%x1f%cI%x1f%s%x1f%b", full)
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "커밋을 불러오지 못했어요.")
		return
	}
	f := strings.SplitN(strings.TrimRight(strings.ReplaceAll(string(meta), "\r", ""), "\n"), "\x1f", 6)
	for len(f) < 6 {
		f = append(f, "")
	}
	common := []string{"show", "--format=", "--no-renames", "--no-color", "--no-ext-diff", "--diff-merges=first-parent"}
	num, err := a.runGitTimeout(60*time.Second, append(append([]string{}, common...), "--numstat", full)...)
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "변경 내용을 불러오지 못했어요.")
		return
	}
	var files []codeChangedFile
	sc := bufio.NewScanner(bytes.NewReader(num))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		parts := strings.SplitN(strings.TrimRight(sc.Text(), "\r"), "\t", 3)
		if len(parts) < 3 {
			continue
		}
		cf := codeChangedFile{Path: parts[2]}
		if parts[0] == "-" {
			cf.Binary = true
		} else {
			cf.Add, _ = strconv.Atoi(parts[0])
			cf.Del, _ = strconv.Atoi(parts[1])
		}
		files = append(files, cf)
	}
	if ns, err := a.runGitTimeout(60*time.Second, append(append([]string{}, common...), "--name-status", full)...); err == nil {
		status := map[string]string{}
		for _, line := range strings.Split(strings.ReplaceAll(string(ns), "\r", ""), "\n") {
			if parts := strings.SplitN(line, "\t", 2); len(parts) == 2 && parts[0] != "" {
				status[parts[1]] = parts[0][:1]
			}
		}
		for i := range files {
			files[i].Status = status[files[i].Path]
		}
	}
	patch, err := a.runGitTimeout(60*time.Second, append(append([]string{}, common...), "--patch", "-U3", full)...)
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "변경 내용을 불러오지 못했어요.")
		return
	}
	truncated := false
	if len(patch) > codePatchMaxBytes {
		patch = patch[:codePatchMaxBytes]
		if i := bytes.LastIndexByte(patch, '\n'); i > 0 {
			patch = patch[:i+1]
		}
		truncated = true
	}
	parents := strings.Fields(f[1])
	w.Header().Set("Cache-Control", "public, max-age=3600")
	httputil.JSONOK(w, map[string]any{
		"hash": f[0], "parents": parents, "author": f[2], "date": f[3], "message": f[4],
		"body": strings.TrimSpace(f[5]), "files": files, "patch": string(bytes.ToValidUTF8(patch, []byte("?"))), "truncated": truncated,
	})
}

// GET /api/code/blame?path=
func (a *App) ApiCodeBlameHandler(w http.ResponseWriter, r *http.Request) {
	snap, err := a.codeSnapshot()
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "지금은 Blame을 계산할 수 없어요.")
		return
	}
	e, ok := snap.byPath[r.URL.Query().Get("path")]
	if !ok || e.Kind != "file" {
		httputil.JSONError(w, http.StatusNotFound, "파일을 찾을 수 없어요.")
		return
	}
	if e.Size > codeBlameMaxBytes {
		httputil.JSONError(w, http.StatusRequestEntityTooLarge, "파일이 너무 커서 Blame을 볼 수 없어요.")
		return
	}
	out, err := a.runGitTimeout(60*time.Second, "blame", "--porcelain", snap.Commit, "--", e.Path)
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "Blame을 계산하지 못했어요.")
		return
	}
	type blameCommit struct {
		Hash    string `json:"hash"`
		Author  string `json:"author"`
		Date    string `json:"date"`
		Message string `json:"message"`
	}
	var commits []blameCommit
	index := map[string]int{}
	var ranges [][2]int // [커밋 번호, 연속된 줄 수]
	cur := -1
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\t") {
			if cur < 0 {
				continue
			}
			if n := len(ranges); n > 0 && ranges[n-1][0] == cur {
				ranges[n-1][1]++
			} else {
				ranges = append(ranges, [2]int{cur, 1})
			}
			continue
		}
		sp := strings.IndexByte(line, ' ')
		if sp == 40 && codeHashRe.MatchString(line[:40]) {
			h := line[:40]
			i, seen := index[h]
			if !seen {
				commits = append(commits, blameCommit{Hash: h})
				i = len(commits) - 1
				index[h] = i
			}
			cur = i
			continue
		}
		if cur < 0 || sp < 0 {
			continue
		}
		key, val := line[:sp], strings.TrimRight(line[sp+1:], "\r")
		c := &commits[cur]
		switch key {
		case "author":
			c.Author = val
		case "summary":
			c.Message = val
		case "committer-time":
			if t, err := strconv.ParseInt(val, 10, 64); err == nil {
				c.Date = time.Unix(t, 0).Format(time.RFC3339)
			}
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httputil.JSONOK(w, map[string]any{"commits": commits, "ranges": ranges})
}
