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

func Load(projectDir string) *Config {
	dbDir := filepath.Join(projectDir, "db")
	staticDir := filepath.Join(projectDir, "static")

	return &Config{
		DBBackend:  strings.ToLower(strings.TrimSpace(getenv("DB_BACKEND", "sqlite"))),
		SQLitePath: getenv("SQLITE_DB_PATH", filepath.Join(dbDir, "pastellive_db.db")),
		MySQLHost:  getenv("DB_HOST", "127.0.0.1"),
		MySQLPort:  getenvInt("DB_PORT", 3306),
		MySQLUser:  getenv("DB_USER", "root"),
		MySQLPass:  getenv("DB_PASSWORD", ""),
		MySQLName:  getenv("DB_NAME", "stelive_db"),

		SecretKey: getenv("SECRET_KEY", ""),

		DiscordClientID:     getenv("DISCORD_OAUTH_CLIENT_ID", ""),
		DiscordClientSecret: getenv("DISCORD_OAUTH_CLIENT_SECRET", ""),
		DiscordRedirectURI:  getenv("DISCORD_OAUTH_REDIRECT_URI", ""),

		ListenAddr:   getenv("LISTEN_ADDR", "127.0.0.1:8081"),
		ProjectDir:   projectDir,
		StaticDir:    staticDir,
		TemplatesDir: getenv("TEMPLATES_DIR", filepath.Join(projectDir, "templates")),
		SiteHost:     getenv("SITE_HOST", "pastellive.co.kr"),

		AdminNickname: getenv("ADMIN_NICKNAME", ""),
		AdminHostname: getenv("ADMIN_HOSTNAME", "admin.pastellive.co.kr"),

		CookieSecure: getenvBool("COOKIE_SECURE", true),

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
		OllamaNumPredict: getenvInt("OLLAMA_NUM_PREDICT", 220),

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
