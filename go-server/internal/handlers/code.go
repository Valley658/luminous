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
	"os"
	"os/exec"
	"path"
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
	Entries   []codeEntry
	byPath    map[string]codeEntry
	checkedAt time.Time
}

type codeBrowser struct {
	mu        sync.Mutex
	snap      *codeSnapshot
	blobMu    sync.Mutex
	blobs     map[string][]byte
	blobBytes int
}

var codeRepo = &codeBrowser{blobs: map[string][]byte{}}

func codeGitBinary() string {
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	for _, p := range []string{
		`C:\Program Files\Git\cmd\git.exe`,
		`C:\Program Files\Git\bin\git.exe`,
		`C:\Program Files (x86)\Git\cmd\git.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "git"
}

func (a *App) runGit(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), codeGitTimeout)
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
	snap := &codeSnapshot{Commit: commit, Ref: ref, byPath: map[string]codeEntry{}, checkedAt: time.Now()}
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
	}
	if info, err := a.runGit("log", "-1", "--format=%cI%x00%s", commit); err == nil {
		parts := strings.SplitN(strings.TrimRight(string(info), "\r\n"), "\x00", 2)
		snap.Date = parts[0]
		if len(parts) > 1 {
			snap.Message = parts[1]
		}
	}
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

// GET /api/code/raw?path=... - 이미지 미리보기 전용(이미지 외에는 내려주지 않음)
func (a *App) ApiCodeRawHandler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	snap, err := a.codeSnapshot()
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	e, ok := snap.byPath[p]
	ctype, isImg := codeImageTypes[strings.ToLower(path.Ext(p))]
	if !ok || !isImg || e.Kind != "file" || e.Size > 8<<20 {
		http.NotFound(w, r)
		return
	}
	b, err := a.codeBlob(e.hash)
	if err != nil || bytes.HasPrefix(b, []byte("version https://git-lfs")) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(b)
}
