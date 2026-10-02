package handlers

// code.go - 사이트 안에서 GitHub 저장소와 똑같은 폴더/파일을 둘러보는 코드 탐색기 (/code)
//
// 데이터는 서버에 있는 이 프로젝트의 git 저장소에서 직접 읽는다.
//   - 기준 커밋: GitHub에 마지막으로 push된 커밋(refs/remotes/github/master).
//     원격 추적 브랜치가 없으면 로컬 HEAD를 쓴다.
//   - 파일 목록: git ls-tree, 파일 내용: git cat-file blob <해시>
//
// 디스크의 작업 폴더를 읽는 게 아니라 "커밋된 내용"만 읽기 때문에, .gitignore로 막힌
// .env, DB, 로그, frontend/ 원본 JS 같은 파일은 절대 노출되지 않는다(= GitHub와 동일).
// 요청으로 받은 경로는 ls-tree 결과 목록에 있는지 먼저 확인하고, git에는 경로가 아니라
// blob 해시만 넘기므로 경로 조작(../, 옵션 주입)이 불가능하다.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pastellive/internal/httputil"
)

const (
	codeRefCheckInterval = 30 * time.Second // GitHub push 반영 확인 주기
	codeMaxViewBytes     = 1 << 20          // 화면에 보여줄 최대 파일 크기(1MB)
	codeBlobCacheBytes   = 32 << 20         // 파일 내용 캐시 최대 용량
	codeGitTimeout       = 15 * time.Second
	codeRawMaxBytes      = 20 << 20 // Raw로 직접 내려줄 최대 크기(넘으면 GitHub로)
	codeGitHubURL        = "https://github.com/Valley658/luminous"
)

// GitHub와 같은 기준을 쓰기 위해 이 순서대로 찾는다.
var codeRefCandidates = []string{
	"refs/remotes/github/master",
	"refs/remotes/origin/master",
	"refs/remotes/origin/main",
	"HEAD",
}

type codeEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Kind string `json:"kind"` // file | link | submodule
	hash string
}

type codeSnapshot struct {
	Commit    string
	Ref       string
	Date      string
	Message   string
	Author    string
	Count     int
	Languages []codeLang
	Entries   []codeEntry
	byPath    map[string]codeEntry
	dirs      map[string]bool
	checkedAt time.Time
}

type codeLang struct {
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	Bytes   int64   `json:"bytes"`
	Percent float64 `json:"percent"`
}

type codeCommitInfo struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

// 파일/폴더별 "마지막으로 바뀐 커밋" (GitHub 파일 목록 오른쪽에 나오는 커밋 메시지와 시간)
type codeHistory struct {
	Commit       string
	Commits      []codeCommitInfo
	Last         map[string]int
	All          []string          // 이 커밋까지의 전체 커밋 해시(최신순) - 커밋 주소 검증용
	Activity     []string          // 커밋 날짜(최신순) - 활동 그래프용
	Contributors []codeContributor // 기여자별 커밋 수
}

type codeContributor struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

type codeBrowser struct {
	mu        sync.Mutex
	snap      *codeSnapshot
	histMu    sync.Mutex
	hist      *codeHistory
	blobMu    sync.Mutex
	blobs     map[string][]byte
	blobBytes int
}

var codeRepo = &codeBrowser{blobs: map[string][]byte{}}

func codeGitBinary() string {
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	for _, p := range []string{"/usr/bin/git", "/usr/local/bin/git"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "git"
}

func (a *App) runGit(args ...string) ([]byte, error) {
	return a.runGitTimeout(codeGitTimeout, args...)
}

func (a *App) runGitTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// safe.directory: 서버가 다른 계정(작업 스케줄러 등)으로 돌 때 "dubious ownership" 거부 방지
	full := append([]string{"-c", "safe.directory=*", "-c", "core.quotepath=off"}, args...)
	cmd := exec.CommandContext(ctx, codeGitBinary(), full...)
	cmd.Dir = a.Cfg.ProjectDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New(strings.TrimSpace(err.Error() + " " + stderr.String()))
	}
	return out, nil
}

func (a *App) codeResolveRef() (string, string, error) {
	for _, ref := range codeRefCandidates {
		out, err := a.runGit("rev-parse", "--verify", "--quiet", ref+"^{commit}")
		if err == nil {
			if c := strings.TrimSpace(string(out)); len(c) >= 40 {
				return c, ref, nil
			}
		}
	}
	return "", "", errors.New("커밋을 찾을 수 없음")
}

func (a *App) codeSnapshot() (*codeSnapshot, error) {
	codeRepo.mu.Lock()
	defer codeRepo.mu.Unlock()
	if s := codeRepo.snap; s != nil && time.Since(s.checkedAt) < codeRefCheckInterval {
		return s, nil
	}
	commit, ref, err := a.codeResolveRef()
	if err != nil {
		if codeRepo.snap != nil { // git이 잠깐 실패해도 이전 결과로 계속 보여줌
			return codeRepo.snap, nil
		}
		return nil, err
	}
	if s := codeRepo.snap; s != nil && s.Commit == commit {
		s.checkedAt = time.Now()
		s.Ref = ref
		return s, nil
	}

	out, err := a.runGit("ls-tree", "-r", "-z", "-l", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	snap := &codeSnapshot{Commit: commit, Ref: ref, byPath: map[string]codeEntry{}, dirs: map[string]bool{}, checkedAt: time.Now()}
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		// "<mode> <type> <hash> <size>\t<path>"
		tab := bytes.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(string(rec[:tab]))
		if len(meta) < 4 {
			continue
		}
		e := codeEntry{Path: string(rec[tab+1:]), hash: meta[2], Kind: "file"}
		switch {
		case meta[1] == "commit":
			e.Kind = "submodule"
		case meta[0] == "120000":
			e.Kind = "link"
		}
		if n, err := strconv.ParseInt(meta[3], 10, 64); err == nil {
			e.Size = n
		}
		snap.Entries = append(snap.Entries, e)
		snap.byPath[e.Path] = e
		for d := path.Dir(e.Path); d != "." && d != "/"; d = path.Dir(d) {
			if snap.dirs[d] {
				break
			}
			snap.dirs[d] = true
		}
	}
	if info, err := a.runGit("log", "-1", "--format=%cI%x00%s%x00%an", commit); err == nil {
		parts := strings.SplitN(strings.TrimRight(string(info), "\r\n"), "\x00", 3)
		snap.Date = parts[0]
		if len(parts) > 1 {
			snap.Message = parts[1]
		}
		if len(parts) > 2 {
			snap.Author = parts[2]
		}
	}
	if n, err := a.runGit("rev-list", "--count", commit); err == nil {
		snap.Count, _ = strconv.Atoi(strings.TrimSpace(string(n)))
	}
	snap.Languages = codeLanguages(snap.Entries)
	codeRepo.snap = snap
	return snap, nil
}

func (a *App) codeBlob(hash string) ([]byte, error) {
	codeRepo.blobMu.Lock()
	if b, ok := codeRepo.blobs[hash]; ok {
		codeRepo.blobMu.Unlock()
		return b, nil
	}
	codeRepo.blobMu.Unlock()

	b, err := a.runGit("cat-file", "blob", hash)
	if err != nil {
		return nil, err
	}
	codeRepo.blobMu.Lock()
	if codeRepo.blobBytes+len(b) > codeBlobCacheBytes {
		codeRepo.blobs = map[string][]byte{}
		codeRepo.blobBytes = 0
	}
	if len(b) <= codeBlobCacheBytes/4 {
		codeRepo.blobs[hash] = b
		codeRepo.blobBytes += len(b)
	}
	codeRepo.blobMu.Unlock()
	return b, nil
}

var codeImageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
	".ico": "image/x-icon", ".bmp": "image/bmp",
}

func codeLooksBinary(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	// 8000바이트에서 자르면 마지막 한글 글자가 반쯤 잘릴 수 있으니 끝 3바이트까지는 봐준다
	for i := 0; i < 4 && len(head) > 0; i++ {
		if utf8.Valid(head) {
			return false
		}
		if len(b) <= 8000 {
			return true
		}
		head = head[:len(head)-1]
	}
	return true
}

// GET /code
func (a *App) CodePageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := map[string]any{"request": requestContext(r), "github_url": codeGitHubURL}
	if err := a.Templates.Render(w, r, "code.html", ctx, a.GenRepImageOverrides); err != nil {
		log.Printf("코드 탐색기 페이지 렌더 실패: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// GET /api/code/tree - 전체 파일 목록(폴더 구조는 프론트에서 경로로 조립)
func (a *App) ApiCodeTreeHandler(w http.ResponseWriter, r *http.Request) {
	snap, err := a.codeSnapshot()
	if err != nil {
		log.Printf("[코드 탐색기] git 읽기 실패: %v", err)
		httputil.JSONError(w, http.StatusServiceUnavailable, "지금은 코드를 불러올 수 없어요. 잠시 후 다시 시도해 주세요.")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	httputil.JSONOK(w, map[string]any{
		"commit":     snap.Commit,
		"ref":        snap.Ref,
		"date":       snap.Date,
		"message":    snap.Message,
		"author":     snap.Author,
		"count":      snap.Count,
		"languages":  snap.Languages,
		"github_url": codeGitHubURL,
		"files":      snap.Entries,
	})
}

// GET /api/code/file?path=... - 파일 하나의 내용
func (a *App) ApiCodeFileHandler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	snap, err := a.codeSnapshot()
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "지금은 코드를 불러올 수 없어요.")
		return
	}
	e, ok := snap.byPath[p]
	if !ok {
		httputil.JSONError(w, http.StatusNotFound, "파일을 찾을 수 없어요.")
		return
	}
	resp := map[string]any{"path": e.Path, "size": e.Size, "kind": e.Kind, "commit": snap.Commit}
	if e.Kind == "submodule" {
		httputil.JSONOK(w, resp)
		return
	}
	ext := strings.ToLower(path.Ext(e.Path))
	if _, isImg := codeImageTypes[ext]; isImg && e.Size <= 8<<20 {
		resp["image"] = true
	}
	if e.Size > codeMaxViewBytes {
		resp["too_large"] = true
		httputil.JSONOK(w, resp)
		return
	}
	b, err := a.codeBlob(e.hash)
	if err != nil {
		log.Printf("[코드 탐색기] blob 읽기 실패 %s: %v", e.Path, err)
		httputil.JSONError(w, http.StatusServiceUnavailable, "파일을 읽지 못했어요.")
		return
	}
	switch {
	case bytes.HasPrefix(b, []byte("version https://git-lfs.github.com/spec/")):
		resp["lfs"] = true // 큰 파일(Git LFS)은 GitHub에서도 내용 대신 포인터만 저장됨
		resp["content"] = string(b)
	case codeLooksBinary(b):
		resp["binary"] = true
	default:
		resp["content"] = string(b)
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httputil.JSONOK(w, resp)
}

// GET /api/code/raw?path=...[&download=1] - 파일 원본 (GitHub의 Raw / 다운로드 버튼)
// 이미지는 이미지로, 그 외에는 전부 text/plain 또는 octet-stream 으로만 내려서
// 저장소 안의 HTML/SVG가 이 사이트 주소에서 실행되는 일이 없게 한다(+ sandbox CSP).
func (a *App) ApiCodeRawHandler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	snap, err := a.codeSnapshot()
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	e, ok := snap.byPath[p]
	if !ok || e.Kind != "file" {
		http.NotFound(w, r)
		return
	}
	ghRaw := codeGitHubURL + "/raw/" + snap.Commit + "/" + codeEscapePath(e.Path)
	if e.Size > codeRawMaxBytes {
		http.Redirect(w, r, ghRaw, http.StatusFound)
		return
	}
	b, err := a.codeBlob(e.hash)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if bytes.HasPrefix(b, []byte("version https://git-lfs")) {
		// LFS 파일의 실제 내용은 GitHub LFS 저장소에만 있다
		http.Redirect(w, r, ghRaw, http.StatusFound)
		return
	}
	ctype, isImg := codeImageTypes[strings.ToLower(path.Ext(p))]
	switch {
	case isImg:
	case codeLooksBinary(b):
		ctype = "application/octet-stream"
	default:
		ctype = "text/plain; charset=utf-8"
	}
	name := path.Base(e.Path)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	} else {
		w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(name))
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(b)
}

func codeEscapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// GET /api/code/history - 파일/폴더마다 마지막으로 바뀐 커밋
func (a *App) ApiCodeHistoryHandler(w http.ResponseWriter, r *http.Request) {
	h, err := a.codeHistory()
	if err != nil {
		log.Printf("[코드 탐색기] 커밋 기록 읽기 실패: %v", err)
		httputil.JSONError(w, http.StatusServiceUnavailable, "커밋 기록을 불러오지 못했어요.")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httputil.JSONOK(w, map[string]any{"commit": h.Commit, "commits": h.Commits, "last": h.Last,
		"activity": h.Activity, "contributors": h.Contributors, "total": len(h.All)})
}

func (a *App) codeHistory() (*codeHistory, error) {
	snap, err := a.codeSnapshot()
	if err != nil {
		return nil, err
	}
	codeRepo.histMu.Lock()
	defer codeRepo.histMu.Unlock()
	if h := codeRepo.hist; h != nil && h.Commit == snap.Commit {
		return h, nil
	}
	out, err := a.runGitTimeout(90*time.Second, "log", "--no-renames", "--name-only",
		"--format=%x1e%H%x1f%an%x1f%cI%x1f%s", snap.Commit)
	if err != nil {
		return nil, err
	}
	h := &codeHistory{Commit: snap.Commit, Last: map[string]int{}}
	remaining := len(snap.byPath)
	byAuthor := map[string]int{}
	for _, chunk := range strings.Split(string(out), "\x1e") {
		lines := strings.Split(strings.ReplaceAll(chunk, "\r", ""), "\n")
		head := strings.SplitN(lines[0], "\x1f", 4)
		if len(head) < 4 {
			continue
		}
		h.All = append(h.All, head[0])
		h.Activity = append(h.Activity, head[2])
		byAuthor[head[1]]++
		if remaining == 0 {
			continue
		}
		idx := -1
		for _, f := range lines[1:] {
			if f == "" {
				continue
			}
			if _, inTree := snap.byPath[f]; !inTree {
				continue
			}
			if _, done := h.Last[f]; done {
				continue
			}
			if idx < 0 {
				h.Commits = append(h.Commits, codeCommitInfo{Hash: head[0], Author: head[1], Date: head[2], Message: head[3]})
				idx = len(h.Commits) - 1
			}
			h.Last[f] = idx
			remaining--
			for d := path.Dir(f); d != "." && d != "/"; d = path.Dir(d) {
				if _, done := h.Last[d]; done {
					break
				}
				h.Last[d] = idx
			}
		}
	}
	for name, n := range byAuthor {
		h.Contributors = append(h.Contributors, codeContributor{Name: name, Commits: n})
	}
	sort.Slice(h.Contributors, func(i, j int) bool { return h.Contributors[i].Commits > h.Contributors[j].Commits })
	codeRepo.hist = h
	return h, nil
}

// GitHub(linguist)처럼 저장소 언어 비율을 계산한다. .gitattributes에서 vendored/generated로
// 표시한 폴더는 GitHub도 빼고 세므로 똑같이 뺀다.
var codeVendoredPrefixes = []string{
	"services/nsfw-service/runtime/",
	"services/c-image-service/tools/",
	"static/js/dist/",
}

var codeLangByExt = map[string][2]string{
	".go": {"Go", "#00ADD8"}, ".html": {"HTML", "#e34c26"}, ".htm": {"HTML", "#e34c26"},
	".js": {"JavaScript", "#f1e05a"}, ".mjs": {"JavaScript", "#f1e05a"}, ".ts": {"TypeScript", "#3178c6"},
	".css": {"CSS", "#663399"}, ".rs": {"Rust", "#dea584"}, ".py": {"Python", "#3572A5"},
	".bat": {"Batchfile", "#C1F12E"}, ".cmd": {"Batchfile", "#C1F12E"}, ".ps1": {"PowerShell", "#012456"},
	".zig": {"Zig", "#ec915c"}, ".sh": {"Shell", "#89e051"}, ".c": {"C", "#555555"}, ".h": {"C", "#555555"},
	".java": {"Java", "#b07219"}, ".sql": {"SQL", "#e38c00"},
}

func codeLanguages(entries []codeEntry) []codeLang {
	sums := map[string]*codeLang{}
	var total int64
next:
	for _, e := range entries {
		if e.Kind != "file" {
			continue
		}
		for _, pre := range codeVendoredPrefixes {
			if strings.HasPrefix(e.Path, pre) {
				continue next
			}
		}
		info, ok := codeLangByExt[strings.ToLower(path.Ext(e.Path))]
		if !ok {
			continue
		}
		l := sums[info[0]]
		if l == nil {
			l = &codeLang{Name: info[0], Color: info[1]}
			sums[info[0]] = l
		}
		l.Bytes += e.Size
		total += e.Size
	}
	out := make([]codeLang, 0, len(sums))
	for _, l := range sums {
		if total > 0 {
			l.Percent = float64(l.Bytes) * 100 / float64(total)
		}
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}
