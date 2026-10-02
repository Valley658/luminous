package handlers

import (
	"path/filepath"

	"pastellive/internal/banlist"
	"pastellive/internal/cache"
	"pastellive/internal/config"
	"pastellive/internal/data"
	pdb "pastellive/internal/db"
	"pastellive/internal/email"
	"pastellive/internal/gotemplates"
	"pastellive/internal/javaimage"
	"pastellive/internal/meilisearch"
	"pastellive/internal/middleware"
	"pastellive/internal/moderation"
	"pastellive/internal/phash"
	"pastellive/internal/realtime"
	"pastellive/internal/session"
	"pastellive/internal/video"
)

type App struct {
	DB           *pdb.DB
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
	BanList      *banlist.List
	Email        *email.Client
	Notify       *realtime.Hub

	GenRepImageOverrides map[string]string
}

const meiliIndexName = "pastellive_search"

func New(db *pdb.DB, cfg *config.Config, store *session.Store) *App {
	initDevAccess(cfg)
	meili := meilisearch.New(cfg.MeilisearchURL, cfg.MeilisearchKey)
	if meili.Available() {
		meili.Configure(meiliIndexName,
			[]string{"title", "description", "nickname", "content", "member_name", "author", "choseong"},
			[]string{"type"}, data.MemberSearchSynonyms, 20000)
	}
	return &App{
		DB:                   db,
		Cfg:                  cfg,
		SessionStore:         store,
		VideoPool:            video.NewPool(filepath.Join(cfg.ProjectDir, "data", "video_pool_cache.json")),
		Cache:                cache.New(),
		Templates:            gotemplates.New(cfg.TemplatesDir, cfg.StaticDir, cfg.SiteHost),
		JavaImage:            javaimage.New(cfg.JavaImageServiceURL, cfg.JavaImageServiceTimeout),
		Moderation:           moderation.New(cfg.ModerationServiceURL, cfg.ModerationServiceTimeout),
		Phash:                phash.New(cfg.PhashServiceURL, cfg.PhashServiceTimeout),
		Meili:                meili,
		Email:                email.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom, cfg.SMTPFromName),
		Notify:               realtime.NewHub(),
		BanList:              banlist.New(),
		GenRepImageOverrides: data.BuildGenerationRepImageOverrides(cfg.StaticDir),
	}
}
