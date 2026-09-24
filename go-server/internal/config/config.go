package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBBackend  string
	SQLitePath string
	MySQLHost  string
	MySQLPort  int
	MySQLUser  string
	MySQLPass  string
	MySQLName  string

	// 루미(AI 마스코트) 대화 기록 전용 DB - 사이트 본 DB(MySQLName/SQLitePath)와
	// 완전히 분리된 별도 데이터베이스. 같은 MySQL 서버/계정을 재사용하되 DB
	// 이름만 다르다(MySQL은 DB 단위로만 분리 가능 - 별도 서버까지는 아님).
	LumiDBName     string
	LumiSQLitePath string

	SecretKey string

	DiscordClientID     string
	DiscordClientSecret string
	DiscordRedirectURI  string

	ListenAddr string

	ProjectDir   string
	StaticDir    string
	TemplatesDir string
	SiteHost     string

	AdminNickname string
	AdminHostname string

	CookieSecure bool
	CookieDomain string

	YoutubeAPIKey string

	GoogleDriveAPIKey string

	JavaImageServiceURL     string
	JavaImageServiceTimeout time.Duration

	ModerationServiceURL     string
	ModerationServiceTimeout time.Duration

	PhashServiceURL         string
	PhashServiceTimeout     time.Duration
	PhashDuplicateThreshold int

	MemberIDServiceURL     string
	MemberIDServiceTimeout time.Duration

	OllamaURL        string
	OllamaModel      string
	OllamaTimeout    time.Duration
	OllamaNumPredict int

	WebSearchEnabled bool
	WebSearchTimeout time.Duration

	RateLimitEnabled     bool
	RateLimitWindowSec   int
	RateLimitMaxRequests int
	RateLimitBanMinutes  int

	AdminDriveAllowedEmail string

	MeilisearchURL string
	MeilisearchKey string

	SchedulerEnabled   bool
	LogsDir            string
	InternalAPIKey     string
	WebSubCallbackBase string
	WebSubSecret       string
	N8NNewVideoWebhook string
	RedisURL           string

	StaffNicknames  map[string]bool
	DevNicknames    map[string]bool
	DevEmail        string
	DevBypassIPs    map[string]bool
	StaffRoleLabels map[string]string
}

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getenvBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func getenvSet(key string) map[string]bool {
	out := map[string]bool{}
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func getenvLabelMap(key string) map[string]string {
	out := map[string]string{}
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		name := strings.TrimSpace(kv[0])
		label := strings.TrimSpace(kv[1])
		if name != "" && label != "" {
			out[name] = label
		}
	}
	return out
}

func getenvFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func randomHexSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "fallback-secret-not-random"
	}
	return hex.EncodeToString(b)
}

// defaultCookieDomain은 로그인 세션 쿠키를 admin.pastellive.co.kr 같은
// 서브도메인에도 같이 보낼 수 있게 기본 쿠키 Domain 값을 계산한다.
//
// [2026-09-22: "리도" 계정으로 pastellive.co.kr에 로그인해도 admin.pastellive.co.kr
// 접속 시 관리자 권한이 인식되지 않는 버그의 원인 - 예전엔 세션 쿠키에 Domain을
// 아예 안 지정했는데, 브라우저는 Domain이 없는 쿠키를 "호스트 전용"으로 취급해서
// 로그인한 그 정확한 호스트(pastellive.co.kr)에만 쿠키를 돌려보내고 다른
// 서브도메인(admin.pastellive.co.kr)에는 보내지 않는다 - 그래서 관리자 페이지에선
// 항상 비로그인 상태로 보였던 것. 앞에 점(".")을 붙인 Domain을 지정하면 같은
// 루트 도메인의 모든 서브도메인에 쿠키가 공유된다.]
//
// localhost/사설 IP로 로컬 개발 중일 땐 Domain을 지정하면 오히려 쿠키가 전혀
// 안 먹으므로(브라우저가 그런 값은 거부함) 빈 문자열(옛날처럼 호스트 전용)로 둔다.
func defaultCookieDomain(siteHost string) string {
	h := strings.TrimSpace(siteHost)
	if h == "" || h == "localhost" || !strings.Contains(h, ".") {
		return ""
	}
	isIP := true
	for _, r := range h {
		if (r < '0' || r > '9') && r != '.' {
			isIP = false
			break
		}
	}
	if isIP {
		return ""
	}
	return "." + h
}

func Load(projectDir string) *Config {
	dbDir := filepath.Join(projectDir, "db")
	staticDir := filepath.Join(projectDir, "static")
	siteHost := getenv("SITE_HOST", "pastellive.co.kr")

	return &Config{
		DBBackend:  strings.ToLower(strings.TrimSpace(getenv("DB_BACKEND", "sqlite"))),
		SQLitePath: getenv("SQLITE_DB_PATH", filepath.Join(dbDir, "pastellive_db.db")),
		MySQLHost:  getenv("DB_HOST", "127.0.0.1"),
		MySQLPort:  getenvInt("DB_PORT", 3306),
		MySQLUser:  getenv("DB_USER", "root"),
		MySQLPass:  getenv("DB_PASSWORD", ""),
		MySQLName:  getenv("DB_NAME", "stelive_db"),

		LumiDBName:     getenv("LUMI_DB_NAME", "lumi_db"),
		LumiSQLitePath: getenv("LUMI_SQLITE_DB_PATH", filepath.Join(dbDir, "lumi_db.db")),

		SecretKey: getenv("SECRET_KEY", ""),

		DiscordClientID:     getenv("DISCORD_OAUTH_CLIENT_ID", ""),
		DiscordClientSecret: getenv("DISCORD_OAUTH_CLIENT_SECRET", ""),
		DiscordRedirectURI:  getenv("DISCORD_OAUTH_REDIRECT_URI", ""),

		ListenAddr:   getenv("LISTEN_ADDR", "127.0.0.1:8081"),
		ProjectDir:   projectDir,
		StaticDir:    staticDir,
		TemplatesDir: getenv("TEMPLATES_DIR", filepath.Join(projectDir, "templates")),
		SiteHost:     siteHost,

		AdminNickname: getenv("ADMIN_NICKNAME", ""),
		AdminHostname: getenv("ADMIN_HOSTNAME", "admin.pastellive.co.kr"),

		CookieSecure: getenvBool("COOKIE_SECURE", true),
		CookieDomain: getenv("COOKIE_DOMAIN", defaultCookieDomain(siteHost)),

		YoutubeAPIKey:     getenv("YOUTUBE_API_KEY", ""),
		GoogleDriveAPIKey: getenv("GOOGLE_DRIVE_API_KEY", ""),

		JavaImageServiceURL:     getenv("JAVA_IMAGE_SERVICE_URL", "http://127.0.0.1:8091"),
		JavaImageServiceTimeout: time.Duration(getenvInt("JAVA_IMAGE_SERVICE_TIMEOUT_SEC", 8)) * time.Second,

		ModerationServiceURL:     getenv("MODERATION_SERVICE_URL", "http://127.0.0.1:8095"),
		ModerationServiceTimeout: time.Duration(getenvInt("MODERATION_SERVICE_TIMEOUT_SEC", 8)) * time.Second,

		PhashServiceURL:         getenv("PHASH_SERVICE_URL", "http://127.0.0.1:8096"),
		PhashServiceTimeout:     time.Duration(getenvInt("PHASH_SERVICE_TIMEOUT_SEC", 5)) * time.Second,
		PhashDuplicateThreshold: getenvInt("PHASH_DUPLICATE_THRESHOLD", 6),

		// 루미 AI 채팅에 방문자가 사진을 올렸을 때, 그 사진 속 스텔라이브
		// 멤버가 누구인지 알아맞혀주는 로컬 전용 서비스(services/member-id-service,
		// 설치는 사진인식_설치.bat). 꺼져 있으면 사진 없이 텍스트 질문만
		// 계속 정상 동작함(lumi_ai.go 참고).
		MemberIDServiceURL:     getenv("MEMBER_ID_SERVICE_URL", "http://127.0.0.1:8098"),
		MemberIDServiceTimeout: time.Duration(getenvInt("MEMBER_ID_SERVICE_TIMEOUT_SEC", 25)) * time.Second,

		// 루미 마스코트 AI 대화 기능용 로컬 LLM(Ollama) 연결 설정.
		// 외부 API 키 없이 같은 서버에서 돌아가는 Ollama를 사용함.
		OllamaURL:     getenv("OLLAMA_URL", "http://127.0.0.1:11434"),
		OllamaModel:   getenv("OLLAMA_MODEL", "qwen2.5:3b-instruct-q4_K_M"),
		OllamaTimeout: time.Duration(getenvInt("OLLAMA_TIMEOUT_SEC", 90)) * time.Second,
		// 답변을 길게 하도록(3~6문장) 프롬프트를 바꾸면서 토큰을 더 많이 생성하게
		// 됐는데, CPU 전용 서버에서는 토큰 수만큼 그대로 추론 시간/CPU 사용량이
		// 늘어난다. 하드웨어 사정에 맞게 재배포 없이 .env에서 바로 조절할 수
		// 있게 환경변수로 뺌 - CPU가 버거우면 이 값을 줄이면 됨(예: 140).
		// [2026-09-22: 220이었을 때 파이썬 코드처럼 조금만 길어져도 답변이
		// 중간에 뚝 끊기는 문제가 있었음(마크다운 코드블록도 닫는 ``` 가 안
		// 나와서 렌더링이 깨짐). num_predict는 "최대 길이"일 뿐 모델이 스스로
		// 끝났다고 판단하면 그 전에 멈추니, 짧은 답은 그대로 짧게 끝나고 긴
		// 답만 끝까지 나올 여유가 생긴다 - 값을 올려도 짧은 질문의 응답 속도는
		// 거의 그대로임.]
		OllamaNumPredict: getenvInt("OLLAMA_NUM_PREDICT", 700),

		// 루미 AI가 답변 전에 짧게 웹 검색을 해서 최신/정확한 정보를 참고하게 할지.
		// Selenium 같은 브라우저 자동화 없이 순수 HTTP로만 동작함 (자세한 설명은
		// internal/websearch 패키지 참고).
		WebSearchEnabled: getenvBool("WEB_SEARCH_ENABLED", true),
		WebSearchTimeout: time.Duration(getenvInt("WEB_SEARCH_TIMEOUT_SEC", 6)) * time.Second,

		RateLimitEnabled:     getenvBool("RATE_LIMIT_ENABLED", true),
		RateLimitWindowSec:   getenvInt("RATE_LIMIT_WINDOW_SEC", 10),
		RateLimitMaxRequests: getenvInt("RATE_LIMIT_MAX_REQUESTS", 150),
		RateLimitBanMinutes:  getenvInt("RATE_LIMIT_BAN_MINUTES", 15),

		AdminDriveAllowedEmail: getenv("ADMIN_DRIVE_ALLOWED_EMAIL", ""),

		MeilisearchURL: getenv("MEILISEARCH_URL", "http://127.0.0.1:7700"),
		MeilisearchKey: getenv("MEILISEARCH_KEY", ""),

		SchedulerEnabled:   getenvBool("SCHEDULER_ENABLED", true),
		LogsDir:            getenv("LOGS_DIR", filepath.Join(projectDir, "logs")),
		InternalAPIKey:     getenv("INTERNAL_API_KEY", ""),
		WebSubCallbackBase: strings.TrimRight(getenv("WEBSUB_CALLBACK_BASE", "https://pastellive.co.kr"), "/"),
		WebSubSecret:       webSubSecretOrRandom(),
		N8NNewVideoWebhook: strings.TrimSpace(getenv("N8N_NEW_VIDEO_WEBHOOK_URL", "")),
		RedisURL:           strings.TrimSpace(getenv("REDIS_URL", "")),

		StaffNicknames:  getenvSet("STAFF_NICKNAMES"),
		DevNicknames:    getenvSet("DEV_NICKNAMES"),
		StaffRoleLabels: getenvLabelMap("STAFF_ROLE_LABELS"),
		DevEmail:        getenv("DEV_EMAIL", ""),
		DevBypassIPs:    getenvSet("DEV_BYPASS_IPS"),
	}
}

func webSubSecretOrRandom() string {
	v := strings.TrimSpace(os.Getenv("WEBSUB_SECRET"))
	if v != "" {
		return v
	}
	return randomHexSecret(20)
}

func LoadOrCreateSecretKey(projectDir string) string {
	if v := strings.TrimSpace(os.Getenv("SECRET_KEY")); v != "" {
		return v
	}
	keyPath := filepath.Join(projectDir, ".secret_key")
	if b, err := os.ReadFile(keyPath); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	newKey := randomHexSecret(32)
	_ = os.WriteFile(keyPath, []byte(newKey), 0o600)
	return newKey
}
