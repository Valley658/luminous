package gotemplates

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/nikolalohinski/gonja/v2"
	"github.com/nikolalohinski/gonja/v2/exec"

	"pastellive/internal/data"
)

func md5File(f *os.File) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if len(sum) > 10 {
		sum = sum[:10]
	}
	return sum, nil
}

type Engine struct {
	dir        string
	staticDir  string
	siteScheme string
	siteHost   string

	mu    sync.RWMutex
	cache map[string]*exec.Template
}

// [2026-09-25 보안 점검 - Critical] gonja(Jinja2 스타일 템플릿 엔진)의
// 기본값은 AutoEscape=false다(config.New() 참고) - 즉 지금까지 {{ user.nickname }}
// 같은 템플릿 변수는 HTML 이스케이프 없이 그대로 출력되고 있었다. 닉네임은
// 사용자가 직접 20자 이내로 자유롭게 정할 수 있고(길이 제한 외에 HTML 문자
// 제한이 없음) profile.html의 <h2>{{ user.nickname }}</h2> 등에 그대로
// 렌더링되므로, 닉네임에 <script>...</script>나 <img onerror=...> 를 넣으면
// 그 프로필 페이지를 보는 모든 사람(관리자 포함)에게 그대로 실행되는 저장형
// XSS가 가능했다.
//
// 여기서 AutoEscape를 켜서 모든 {{ }} 출력을 기본적으로 HTML 이스케이프
// 하도록 고쳤다. tojson/versioned_static 필터는 gonja 내부적으로 exec.AsSafeValue로
// 결과를 감싸서 자동이스케이프를 우회하도록 이미 구현돼 있어(Jinja2의 |safe와
// 동일한 방식) 안전하게 그대로 동작한다 - 실제로 템플릿 전체에서 |safe 필터를
// 쓰는 곳도 없었으므로 이 변경으로 깨지는 기존 화면은 없어야 한다.
// gonja.FromFile()이 내부적으로 패키지 전역 변수 gonja.DefaultConfig를 그대로
// 쓰기 때문에, 서버 시작 시(첫 템플릿 컴파일 전에) 한 번만 켜주면 전체
// 템플릿에 적용된다.
func init() {
	gonja.DefaultConfig.AutoEscape = true
}

func New(templatesDir, staticDir, siteHost string) *Engine {
	return &Engine{
		dir:        templatesDir,
		staticDir:  staticDir,
		siteScheme: "https",
		siteHost:   siteHost,
		cache:      make(map[string]*exec.Template),
	}
}

func (e *Engine) get(name string) (*exec.Template, error) {
	e.mu.RLock()
	tpl, ok := e.cache[name]
	e.mu.RUnlock()
	if ok {
		return tpl, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if tpl, ok := e.cache[name]; ok {
		return tpl, nil
	}
	tpl, err := gonja.FromFile(filepath.Join(e.dir, name))
	if err != nil {
		return nil, err
	}
	e.cache[name] = tpl
	return tpl, nil
}

type assetCacheEntry struct {
	mtime  int64
	digest string
}

var assetCacheMu sync.Mutex
var assetCache = map[string]assetCacheEntry{}

func assetContentHash(staticDir, relPath string) string {
	full := filepath.Join(staticDir, filepath.FromSlash(relPath))
	info, err := os.Stat(full)
	if err != nil {
		return ""
	}
	mtime := info.ModTime().UnixNano()

	assetCacheMu.Lock()
	if e, ok := assetCache[relPath]; ok && e.mtime == mtime {
		assetCacheMu.Unlock()
		return e.digest
	}
	assetCacheMu.Unlock()

	f, err := os.Open(full)
	if err != nil {
		return ""
	}
	defer f.Close()
	digest, err := md5File(f)
	if err != nil {
		return ""
	}
	assetCacheMu.Lock()
	assetCache[relPath] = assetCacheEntry{mtime: mtime, digest: digest}
	assetCacheMu.Unlock()
	return digest
}

func (e *Engine) baseGlobals(genRepOverrides map[string]string) map[string]any {
	// 예전엔 en/ja 다국어 지원 때문에 member.<이름> locale 카탈로그를 찾아보고
	// 없으면 폴백하는 구조였는데, 사이트가 한국어 전용이 되면서(2026-09-18,
	// locales/ 폴더 자체를 제거) 그냥 MEMBER_FULL_NAMES 값을 그대로 쓰면 됨.
	memberFullNames := make(map[string]any, len(data.MEMBER_FULL_NAMES))
	for k, v := range data.MEMBER_FULL_NAMES {
		memberFullNames[k] = v
	}
	accentColors := make(map[string]any, len(data.MEMBER_ACCENT_COLORS))
	for k, v := range data.MEMBER_ACCENT_COLORS {
		accentColors[k] = v
	}
	memberImages := make(map[string]any, len(data.MemberImages))
	for k, v := range data.MemberImages {
		memberImages[k] = v
	}
	repOverrides := make(map[string]any, len(genRepOverrides))
	for k, v := range genRepOverrides {
		repOverrides[k] = v
	}

	urlFor := func(args *exec.VarArgs) *exec.Value {
		endpoint := args.First().String()
		filename := args.GetKeywordArgument("filename", "").String()
		external := args.GetKeywordArgument("_external", false).Bool()
		if endpoint != "static" {
			return exec.AsValue("")
		}
		p := "/static/" + filename
		if external {
			return exec.AsValue(e.siteScheme + "://" + e.siteHost + p)
		}
		return exec.AsValue(p)
	}

	versionedStatic := func(args *exec.VarArgs) *exec.Value {
		relPath := args.First().String()
		digest := assetContentHash(e.staticDir, relPath)
		p := "/static/" + relPath
		if digest != "" {
			p += "?v=" + digest
		}
		return exec.AsValue(p)
	}

	return map[string]any{
		"MEMBER_FULL_NAMES":              memberFullNames,
		"MEMBER_ACCENT_COLORS":           accentColors,
		"MEMBER_IMAGES":                  memberImages,
		"GENERATION_REP_IMAGE_OVERRIDES": repOverrides,
		"url_for":                        exec.AsSafeValue(urlFor),
		"versioned_static":               exec.AsSafeValue(versionedStatic),
	}
}

func (e *Engine) Render(w http.ResponseWriter, r *http.Request, name string, ctx map[string]any, genRepOverrides map[string]string) error {
	tpl, err := e.get(name)
	if err != nil {
		return fmt.Errorf("템플릿 로드 실패 (%s): %w", name, err)
	}
	merged := e.baseGlobals(genRepOverrides)
	for k, v := range ctx {
		merged[k] = v
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return tpl.Execute(w, exec.NewContext(merged))
}
