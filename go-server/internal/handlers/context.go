package handlers

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"pastellive/internal/cache"
	"pastellive/internal/config"
	"pastellive/internal/data"
	pdb "pastellive/internal/db"
	"pastellive/internal/email"
	"pastellive/internal/gotemplates"
	"pastellive/internal/javaimage"
	"pastellive/internal/localai"
	"pastellive/internal/meilisearch"
	"pastellive/internal/memberid"
	"pastellive/internal/middleware"
	"pastellive/internal/moderation"
	"pastellive/internal/phash"
	"pastellive/internal/realtime"
	"pastellive/internal/session"
	"pastellive/internal/video"
	"pastellive/internal/websearch"
)

type App struct {
	DB           *pdb.DB
	LumiDB       *pdb.DB
	Cfg          *config.Config
	SessionStore *session.Store
	VideoPool    *video.Pool
	Cache        *cache.Store
	Templates    *gotemplates.Engine
	JavaImage    *javaimage.Client
	Moderation   *moderation.Client
	Phash        *phash.Client
	Meili        *meilisearch.Client
	RateLimiter  *middleware.RateLimiter
	LocalAI      *localai.Client
	WebSearch    *websearch.Client
	MemberID     *memberid.Client
	Email        *email.Client
	Notify       *realtime.Hub

	GenRepImageOverrides map[string]string
}

const meiliIndexName = "pastellive_search"

func New(db *pdb.DB, lumiDB *pdb.DB, cfg *config.Config, store *session.Store) *App {
	initDevAccess(cfg)
	meili := meilisearch.New(cfg.MeilisearchURL, cfg.MeilisearchKey)
	if meili.Available() {
		meili.Configure(meiliIndexName,
			[]string{"title", "description", "nickname", "content", "member_name", "author", "choseong"},
			[]string{"type"}, data.MemberSearchSynonyms, 20000)
	}
	return &App{
		DB:                   db,
		LumiDB:               lumiDB,
		Cfg:                  cfg,
		SessionStore:         store,
		VideoPool:            video.NewPool(filepath.Join(cfg.ProjectDir, "data", "video_pool_cache.json")),
		Cache:                cache.New(),
		Templates:            gotemplates.New(cfg.TemplatesDir, cfg.StaticDir, cfg.SiteHost),
		JavaImage:            javaimage.New(cfg.JavaImageServiceURL, cfg.JavaImageServiceTimeout),
		Moderation:           moderation.New(cfg.ModerationServiceURL, cfg.ModerationServiceTimeout),
		Phash:                phash.New(cfg.PhashServiceURL, cfg.PhashServiceTimeout),
		Meili:                meili,
		LocalAI:              localai.New(cfg.OllamaURL, cfg.OllamaModel, cfg.OllamaTimeout, cfg.OllamaNumPredict),
		WebSearch:            websearch.New(cfg.WebSearchEnabled, cfg.WebSearchTimeout),
		MemberID:             memberid.New(cfg.MemberIDServiceURL, cfg.MemberIDServiceTimeout),
		Email:                email.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom, cfg.SMTPFromName),
		Notify:               realtime.NewHub(),
		GenRepImageOverrides: data.BuildGenerationRepImageOverrides(cfg.StaticDir),
	}
}

// WarmUpLocalAI asks Ollama to load the model into RAM right away in the
// background, instead of making the first real visitor's question pay for
// that cold start (which can take a lot longer than a normal answer and
// would otherwise time out and look like Ollama is broken). Safe to call
// even if Ollama isn't installed/running yet - failures are just logged.
func (a *App) WarmUpLocalAI() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := a.LocalAI.WarmUp(ctx); err != nil {
			log.Printf("[루미 AI] 예열 요청 실패 (Ollama가 아직 안 켜져 있을 수 있음): %v", err)
		} else {
			log.Printf("[루미 AI] 로컬 모델 예열 완료 - 이제 방문자 질문에 바로 답할 수 있음")
		}
	}()
}
