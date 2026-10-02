package handlers

// code_extra.go - /code 코드 탐색기 추가 기능
//   /api/code/search  코드 전체 검색 (git grep, GitHub에 push된 커밋 기준)
//   /api/code/github  GitHub 저장소의 스타/포크/워치 수와 열린 이슈 (GitHub API, 10분 캐시)

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pastellive/internal/httputil"
)

const (
	codeSearchMaxMatches = 400 // 전체 결과 최대 줄 수
	codeSearchMaxFiles   = 100 // 결과 파일 최대 개수
	codeSearchPerFile    = 12  // 파일 하나에서 보여줄 최대 줄 수
	codeSearchLineMax    = 300 // 한 줄 최대 글자 수
	codeGitHubCacheTTL   = 10 * time.Minute
)

// 검색에서 빼는 경로: GitHub도 vendored로 취급하는 외부 라이브러리 + 압축된 JS(한 줄이 너무 길어 결과가 지저분함)
var codeSearchExcludes = []string{
	":(exclude)services/nsfw-service/runtime",
	":(exclude)services/c-image-service/tools",
	":(exclude)static/js",
	":(exclude)static/wasm",
	":(exclude)*.min.js",
	":(exclude)*.lock",
	":(exclude)**/package-lock.json",
	":(exclude)go.sum",
	":(exclude)**/go.sum",
}

type codeSearchHit struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

type codeSearchFile struct {
	Path  string          `json:"path"`
	Total int             `json:"total"`
	Hits  []codeSearchHit `json:"hits"`
}

type codeSearchResult struct {
	Query     string           `json:"query"`
	Files     []codeSearchFile `json:"files"`
	Matches   int              `json:"matches"`
	Truncated bool             `json:"truncated"`
}

var (
	codeSearchMu    sync.Mutex
	codeSearchCache = map[string]*codeSearchResult{}
	codeGrepLineRe  = regexp.MustCompile(`^(.*?):(\d+):(.*)$`)
	codeDateRe      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// GET /api/code/search?q=
func (a *App) ApiCodeSearchHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 100 {
		httputil.JSONError(w, http.StatusBadRequest, "검색어는 2~100글자로 입력해 주세요.")
		return
	}
	snap, err := a.codeSnapshot()
	if err != nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "지금은 검색할 수 없어요.")
		return
	}
	key := snap.Commit + "\x00" + strings.ToLower(q)
	codeSearchMu.Lock()
	cached := codeSearchCache[key]
	codeSearchMu.Unlock()
	if cached == nil {
		cached, err = a.codeGrep(snap.Commit, q)
		if err != nil {
			httputil.JSONError(w, http.StatusServiceUnavailable, "검색하지 못했어요. 잠시 후 다시 시도해 주세요.")
			return
		}
		codeSearchMu.Lock()
		if len(codeSearchCache) > 200 {
			codeSearchCache = map[string]*codeSearchResult{}
		}
		codeSearchCache[key] = cached
		codeSearchMu.Unlock()
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httputil.JSONOK(w, map[string]any{"query": cached.Query, "files": cached.Files, "matches": cached.Matches, "truncated": cached.Truncated})
}

// git grep 결과를 한 줄씩 읽다가 한도를 넘으면 바로 멈춘다(흔한 검색어로 서버가 오래 붙잡히지 않게).
func (a *App) codeGrep(commit, q string) (*codeSearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args := []string{"-c", "safe.directory=*", "-c", "core.quotepath=off",
		"grep", "-n", "-I", "-i", "-F", "--no-color", "-e", q, commit, "--", "."}
	args = append(args, codeSearchExcludes...)
	cmd := exec.CommandContext(ctx, codeGitBinary(), args...)
	cmd.Dir = a.Cfg.ProjectDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	res := &codeSearchResult{Query: q}
	index := map[string]int{}
	prefix := commit + ":"
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), prefix)
		m := codeGrepLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		text := strings.TrimRight(m[3], "\r")
		if utf8.RuneCountInString(text) > codeSearchLineMax {
			text = string([]rune(text)[:codeSearchLineMax]) + "…"
		}
		i, ok := index[m[1]]
		if !ok {
			if len(res.Files) >= codeSearchMaxFiles {
				res.Truncated = true
				break
			}
			res.Files = append(res.Files, codeSearchFile{Path: m[1]})
			i = len(res.Files) - 1
			index[m[1]] = i
		}
		f := &res.Files[i]
		f.Total++
		res.Matches++
		if len(f.Hits) < codeSearchPerFile {
			f.Hits = append(f.Hits, codeSearchHit{Line: n, Text: text})
		}
		if res.Matches >= codeSearchMaxMatches {
			res.Truncated = true
			break
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait() // 결과가 없으면 git grep은 1로 끝나는데, 그건 오류가 아니다
	if ctx.Err() != nil && res.Matches == 0 {
		return nil, ctx.Err()
	}
	return res, nil
}

// ---------- GitHub 저장소 정보 / 이슈 ----------

type codeGitHubIssue struct {
	Number   int      `json:"number"`
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Author   string   `json:"author"`
	Avatar   string   `json:"avatar"`
	Created  string   `json:"created"`
	Comments int      `json:"comments"`
	Labels   []string `json:"labels"`
	Colors   []string `json:"colors"`
}

type codeGitHubInfo struct {
	Stars    int               `json:"stars"`
	Forks    int               `json:"forks"`
	Watchers int               `json:"watchers"`
	Issues   []codeGitHubIssue `json:"issues"`
	OpenPRs  int               `json:"open_prs"`
	Topics   []string          `json:"topics"`
	Fetched  string            `json:"fetched"`
}

var (
	codeGHMu     sync.Mutex
	codeGHInfo   *codeGitHubInfo
	codeGHAt     time.Time
	codeGHClient = &http.Client{Timeout: 8 * time.Second}
)

func codeGitHubGet(url string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pastellive-code-browser")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" { // 있으면 호출 한도가 시간당 60회 → 5000회
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := codeGHClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &codeHTTPError{resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(dst)
}

type codeHTTPError struct{ code int }

func (e *codeHTTPError) Error() string { return "GitHub API 응답 " + strconv.Itoa(e.code) }

func codeFetchGitHub() (*codeGitHubInfo, error) {
	api := "https://api.github.com/repos/" + strings.TrimPrefix(codeGitHubURL, "https://github.com/")
	var repo struct {
		Stars    int      `json:"stargazers_count"`
		Forks    int      `json:"forks_count"`
		Watchers int      `json:"subscribers_count"`
		Topics   []string `json:"topics"`
	}
	if err := codeGitHubGet(api, &repo); err != nil {
		return nil, err
	}
	var raw []struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		URL      string `json:"html_url"`
		Created  string `json:"created_at"`
		Comments int    `json:"comments"`
		User     struct {
			Login  string `json:"login"`
			Avatar string `json:"avatar_url"`
		} `json:"user"`
		Labels []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"labels"`
		PR *json.RawMessage `json:"pull_request"`
	}
	info := &codeGitHubInfo{Stars: repo.Stars, Forks: repo.Forks, Watchers: repo.Watchers, Topics: repo.Topics,
		Issues: []codeGitHubIssue{}, Fetched: time.Now().Format(time.RFC3339)}
	if err := codeGitHubGet(api+"/issues?state=open&per_page=50", &raw); err == nil {
		for _, it := range raw {
			if it.PR != nil { // GitHub API는 PR도 이슈 목록에 섞어서 준다
				info.OpenPRs++
				continue
			}
			is := codeGitHubIssue{Number: it.Number, Title: it.Title, URL: it.URL, Author: it.User.Login,
				Avatar: it.User.Avatar, Created: it.Created, Comments: it.Comments}
			for _, l := range it.Labels {
				is.Labels = append(is.Labels, l.Name)
				is.Colors = append(is.Colors, l.Color)
			}
			info.Issues = append(info.Issues, is)
		}
	}
	return info, nil
}

// GET /api/code/github
func (a *App) ApiCodeGitHubHandler(w http.ResponseWriter, r *http.Request) {
	codeGHMu.Lock()
	defer codeGHMu.Unlock()
	if codeGHInfo == nil || time.Since(codeGHAt) > codeGitHubCacheTTL {
		info, err := codeFetchGitHub()
		switch {
		case err == nil:
			codeGHInfo, codeGHAt = info, time.Now()
		case codeGHInfo != nil:
			codeGHAt = time.Now().Add(-codeGitHubCacheTTL + time.Minute) // 실패하면 1분 뒤 다시 시도, 그동안은 예전 값
		default:
			httputil.JSONError(w, http.StatusServiceUnavailable, "GitHub 정보를 불러오지 못했어요.")
			return
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httputil.JSONOK(w, map[string]any{"github": codeGHInfo})
}
