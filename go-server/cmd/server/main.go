package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"pastellive/internal/config"
	pdb "pastellive/internal/db"
	driveproxy "pastellive/internal/drive"
	"pastellive/internal/handlers"
	plmw "pastellive/internal/middleware"
	"pastellive/internal/models"
	"pastellive/internal/session"
	"pastellive/internal/video"
)

// adminHostnameGate guards the admin subdomain (cfg.AdminHostname, e.g.
// admin.pastellive.co.kr). A logged-in admin hitting "/" there is sent
// straight to the admin dashboard; anyone else hitting that hostname is
// bounced back to the main site with a flag that triggers a "비정상적인
// 접근 시도입니다" alert there. Requests to the main hostname are untouched.
func adminHostnameGate(app *handlers.App, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if idx := strings.IndexByte(host, ':'); idx >= 0 {
				host = host[:idx]
			}
			if !strings.EqualFold(host, cfg.AdminHostname) {
				next.ServeHTTP(w, r)
				return
			}
			if !app.IsAdmin(r) {
				http.Redirect(w, r, "https://"+cfg.SiteHost+"/?admin_denied=1", http.StatusFound)
				return
			}
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/admin/stats", http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func main() {

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("실행 파일 경로를 알 수 없음: %v", err)
	}
	exeDir := filepath.Dir(exePath)

	projectDir := os.Getenv("PROJECT_DIR")
	if projectDir == "" {
		projectDir = filepath.Dir(filepath.Dir(exeDir))
	}

	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = filepath.Join(projectDir, ".env")
	}
	config.LoadEnvFile(envFile)

	cfg := config.Load(projectDir)
	if cfg.SecretKey == "" {
		cfg.SecretKey = config.LoadOrCreateSecretKey(projectDir)
	}
	database, err := pdb.Open(cfg)
	if err != nil {
		log.Fatalf("DB 연결 실패: %v", err)
	}
	defer database.Close()

	if err := models.InitUsersTable(database); err != nil {
		log.Fatalf("users 테이블 초기화 실패: %v", err)
	}
	if err := models.InitHistoryTable(database); err != nil {
		log.Fatalf("watch_history 테이블 초기화 실패: %v", err)
	}
	if err := models.InitCommentLikesTable(database); err != nil {
		log.Fatalf("comment_likes 테이블 초기화 실패: %v", err)
	}
	if err := models.InitPlaylistBookmarkTables(database); err != nil {
		log.Fatalf("user_playlists/user_bookmarks 테이블 초기화 실패: %v", err)
	}
	if err := models.InitChannelTables(database); err != nil {
		log.Fatalf("channel_videos/channel_subscriptions 테이블 초기화 실패: %v", err)
	}
	if err := models.InitLiveCheersTable(database); err != nil {
		log.Fatalf("live_cheers 테이블 초기화 실패: %v", err)
	}
	if err := models.InitAdminAuditLogTable(database); err != nil {
		log.Fatalf("admin_audit_log 테이블 초기화 실패: %v", err)
	}
	if err := models.InitFanartTables(database); err != nil {
		log.Fatalf("fanart_* 테이블 초기화 실패: %v", err)
	}
	if err := models.InitHighlightClipTable(database); err != nil {
		log.Fatalf("highlight_clips 테이블 초기화 실패: %v", err)
	}
	if err := models.InitAttendanceTable(database); err != nil {
		log.Fatalf("user_attendance 테이블 초기화 실패: %v", err)
	}
	if err := models.InitMemberVideoArchiveTable(database); err != nil {
		log.Fatalf("member_video_archive 테이블 초기화 실패: %v", err)
	}
	if err := models.InitSearchTrendsTable(database); err != nil {
		log.Fatalf("search_trends 테이블 초기화 실패: %v", err)
	}
	if err := models.InitSchedulesTable(database); err != nil {
		log.Fatalf("member_schedules 테이블 초기화 실패: %v", err)
	}
	if err := models.InitKirinukiChannelsTable(database); err != nil {
		log.Fatalf("kirinuki_channels 테이블 초기화 실패: %v", err)
	}
	if err := video.InitVideoShortsCacheTable(database); err != nil {
		log.Fatalf("video_shorts_cache 테이블 초기화 실패: %v", err)
	}
	if _, err := database.Exec("DROP TABLE IF EXISTS public_error_events"); err != nil {
		log.Printf("public_error_events 테이블 정리 실패: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS fanart_phash (
		fanart_id INT PRIMARY KEY,
		hash VARCHAR(16) NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Printf("fanart_phash 테이블 초기화 실패: %v", err)
	}
	if err := models.InitNotificationsTable(database); err != nil {
		log.Fatalf("notifications 테이블 초기화 실패: %v", err)
	}
	if err := models.InitInquiriesTable(database); err != nil {
		log.Fatalf("inquiries 테이블 초기화 실패: %v", err)
	}
	if err := models.InitPlaylistVideoItemsTable(database); err != nil {
		log.Fatalf("user_playlist_videos 테이블 초기화 실패: %v", err)
	}

	sessionStore := session.NewStore(cfg.SecretKey, cfg.CookieSecure)
	app := handlers.New(database, cfg, sessionStore)
	app.WarmUpLocalAI()

	rateLimiter := plmw.NewRateLimiter(cfg.RateLimitEnabled, cfg.RateLimitWindowSec, cfg.RateLimitMaxRequests, cfg.RateLimitBanMinutes)
	app.RateLimiter = rateLimiter

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(rateLimiter.Middleware)
	r.Use(plmw.SessionMiddleware(sessionStore))
	r.Use(plmw.CSRFGuard)
	r.Use(adminHostnameGate(app, cfg))

	fileServer := http.FileServer(http.Dir(cfg.StaticDir))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	r.Post("/api/register", app.Register)
	r.Post("/api/login", app.Login)
	r.Get("/api/me", app.Me)
	r.Get("/api/staff-roles", app.ApiStaffRolesHandler)
	r.Get("/logout", app.Logout)
	r.Get("/login/discord", app.DiscordLoginStart)
	r.Get("/login/discord/callback", app.DiscordLoginCallback)
	r.Post("/api/account/set-password", app.ApiSetPasswordHandler)
	r.Post("/api/me/profile", app.ApiUpdateProfileHandler)
	r.Post("/api/me/email", app.ApiUpdateEmailHandler)
	r.Post("/api/me/nickname", app.ApiUpdateNicknameHandler)
	r.Post("/api/me/withdraw", app.ApiWithdrawHandler)

	r.Get("/", app.Index)
	r.Get("/api/m/shorts", app.ApiMShorts)
	r.Get("/api/videos/random_scroll", app.ApiRandomScroll)
	r.Post("/api/videos/random_scroll", app.ApiRandomScroll)
	r.Get("/api/videos/recommend_random", app.ApiRecommendRandom)
	r.Get("/api/videos/{memberName}", app.ApiGetVideosHandler)

	r.Get("/api/drive_thumb/{fileID}", app.DriveThumb)
	r.Get("/api/drive_video_title/{fileID}", app.DriveVideoTitle)
	r.Get("/api/drive_video_stream/*", func(w http.ResponseWriter, r *http.Request) {
		driveproxy.StreamProxyHandler(cfg.GoogleDriveAPIKey)(w, r)
	})

	r.Get("/channel/{channelID}", app.ChannelPage)
	r.Get("/api/channel/{channelID}", app.ApiGetChannel)
	r.Post("/api/channel/subscribe", app.ApiChannelSubscribe)

	r.Get("/api/cheers/{memberName}", app.GetCheersHandler)
	r.Post("/api/cheers/{memberName}", app.AddCheerHandler)
	r.Get("/{memberName}/community", app.CommunityPageHandler)
	r.Get("/member", app.GoToMemberHandler)
	r.Post("/api/community/post", app.CreateCommunityPostHandler)
	r.Get("/api/community/{memberName}/posts", app.GetCommunityPostsHandler)
	r.Post("/api/community/like", app.ApiCommunityLikeHandler)
	r.Get("/api/video/{videoID}/reaction", app.ApiGetVideoReactionHandler)
	r.Get("/api/community/comments/{postID}", app.ApiGetCommunityCommentsHandler)
	r.Post("/api/community/comments/{postID}", app.ApiAddCommunityCommentHandler)
	r.Post("/api/community/comments/{commentID}/delete", app.ApiDeleteCommunityCommentHandler)
	r.Get("/comments", app.AllCommentsPageHandler)

	r.Post("/api/fanart/upload", app.ApiUploadFanartHandler)
	r.Post("/api/fanart/update", app.ApiUpdateFanartHandler)
	r.Post("/api/fanart/{fanartID}/react", app.ApiReactFanartHandler)
	r.Get("/api/fanart/{fanartID}/comments", app.ApiGetFanartCommentsHandler)
	r.Post("/api/fanart/{fanartID}/comments", app.ApiAddFanartCommentHandler)
	r.Post("/api/fanart/comments/{commentID}/delete", app.ApiDeleteFanartCommentHandler)
	r.Get("/gallery", app.GalleryPageHandler)
	r.Get("/api/fanart", app.ApiGetFanartHandler)
	r.Get("/api/fanart/my", app.ApiGetMyFanartHandler)

	r.Post("/api/highlights/tag", app.ApiHighlightTagHandler)
	r.Get("/api/highlights/{clipID}", app.ApiHighlightStatusHandler)
	r.Get("/api/highlights", app.ApiHighlightsListHandler)
	r.Get("/highlights", app.HighlightsGalleryPageHandler)

	r.Post("/api/history/add", app.ApiAddHistoryHandler)
	r.Get("/api/history", app.ApiGetHistoryHandler)
	r.Post("/api/history/delete", app.ApiDeleteHistoryItemsHandler)
	r.Post("/api/history/clear", app.ApiClearHistoryHandler)

	r.Get("/playlists", app.PlaylistsPageHandler)
	r.Get("/bookmarks", app.BookmarksPageHandler)
	r.Get("/api/bookmarks/check", app.ApiCheckBookmarkHandler)
	r.Get("/api/bookmarks", app.ApiBookmarksHandler)
	r.Post("/api/bookmarks", app.ApiBookmarksHandler)
	r.Get("/api/playlists", app.ApiPlaylistsHandler)
	r.Post("/api/playlists", app.ApiPlaylistsHandler)
	r.Post("/api/playlists/{playlistID}/videos", app.ApiAddVideoToPlaylistHandler)

	r.Get("/api/attendance/status", app.ApiAttendanceStatusHandler)
	r.Post("/api/attendance/mark", app.ApiMarkAttendanceHandler)

	r.Get("/profile", app.ProfilePageHandler)
	r.Get("/api/profile/my_videos", app.ApiProfileMyVideosHandler)
	r.Get("/api/profile/liked_videos", app.ApiProfileLikedVideosHandler)
	r.Get("/api/profile/my_posts", app.ApiProfileMyPostsHandler)

	r.Get("/api/search/suggest", app.ApiSearchSuggestHandler)
	r.Get("/api/search/local_index", app.ApiSearchLocalIndexHandler)
	r.Get("/api/search", app.ApiSearchHandler)
	r.Get("/api/search/trending", app.ApiTrendingSearchesHandler)
	r.Post("/api/search/trending/delete", app.ApiDeleteTrendingHandler)

	go app.SyncMeilisearchIndex()

	go app.VideoPool.RefreshIfEmpty(context.Background(), database, cfg.YoutubeAPIKey)
	if cfg.SchedulerEnabled {
		go video.ResubscribeAllChannels(context.Background(), database, cfg.WebSubCallbackBase, cfg.WebSubSecret)
	}
	app.StartBackgroundScheduler()

	r.Get("/admin/stats", app.AdminStatsPageHandler)
	r.Get("/api/admin/stats", app.ApiAdminStatsHandler)
	r.Get("/api/admin/server-log", app.ApiAdminServerLogHandler)
	r.Post("/api/admin/logs/reset", app.ApiAdminLogsResetHandler)
	r.Post("/api/admin/backfill-webp", app.ApiAdminBackfillWebpHandler)
	r.Get("/api/admin/users", app.ApiAdminUsersHandler)
	r.Delete("/api/admin/users/{targetUserID}", app.ApiAdminDeleteUserHandler)
	r.Get("/api/admin/db-overview", app.ApiAdminDBOverviewHandler)
	r.Get("/api/admin/audit-log", app.ApiAdminAuditLogHandler)
	r.Get("/api/admin/rate-limit", app.ApiAdminRateLimitHandler)
	r.Get("/api/all_comments", app.ApiGetAllCommentsHandler)
	r.Post("/api/delete_comments", app.ApiDeleteCommentsHandler)

	r.Get("/api/notifications", app.ApiGetNotificationsHandler)
	r.Post("/api/notifications/read", app.ApiMarkNotificationsReadHandler)

	r.Get("/api/quiz/questions", app.ApiQuizQuestionsHandler)
	r.Post("/api/quiz/check", app.ApiQuizCheckHandler)

	r.Get("/api/schedules", app.ApiGetSchedulesHandler)
	r.Post("/api/schedules", app.ApiCreateScheduleHandler)
	r.Put("/api/schedules/{scheduleID}", app.ApiUpdateScheduleHandler)
	r.Delete("/api/schedules/{scheduleID}", app.ApiDeleteScheduleHandler)

	r.Post("/api/lumi/ask", app.ApiLumiAskHandler)

	r.Get("/share/{contentType}/{contentID}", app.SharePageHandler)
	r.Get("/sw.js", app.ServiceWorkerHandler)
	r.Get("/robots.txt", app.RobotsTxtHandler)
	r.Get("/sitemap.xml", app.SitemapXMLHandler)
	r.Get("/favicon.ico", app.FaviconHandler)
	r.Get("/privacy", app.PrivacyPolicyHandler)
	r.Get("/terms", app.TermsOfServiceHandler)
	r.Get("/api/live_status", app.ApiLiveStatusHandler)

	r.Get("/watch/{videoID}", app.WatchVideoHandler)
	r.Get("/watch/{videoID}/comments", app.WatchVideoCommentsRedirectHandler)
	r.Get("/api/videos/home_shorts", app.ApiGetHomeShortsHandler)
	r.Get("/__sentry_verify__", app.SentryVerifyHandler)
	r.Get("/api/kirinuki/videos", app.ApiKirinukiVideosHandler)

	r.Post("/api/comments/{commentID}/react", app.ApiReactToCommentHandler)
	r.Get("/api/comments/{videoID}", app.ApiGetCommentsHandler)
	r.Post("/api/comments/{videoID}", app.ApiAddCommentHandler)
	r.Get("/api/youtube_comments/{videoID}", app.ApiYoutubeCommentsHandler)
	r.Get("/api/video_storyboard/{videoID}", app.ApiVideoStoryboardHandler)
	r.Get("/api/comment_preview/{videoID}", app.ApiCommentPreviewHandler)
	r.Get("/api/comment_previews", app.ApiCommentPreviewsBatchHandler)

	r.Post("/api/csp-report", app.ApiCspReportHandler)
	r.Get("/reports", app.ReportsPageHandler)
	r.Get("/api/reports", app.ApiGetReportsHandler)
	r.Post("/api/reports/{reportID}/resolve", app.ApiResolveReportHandler)

	r.Post("/api/inquiries", app.ApiSubmitInquiryHandler)
	r.Get("/api/admin/inquiries", app.ApiListInquiriesHandler)
	r.Post("/api/admin/inquiries/{inquiryID}/resolve", app.ApiResolveInquiryHandler)

	r.Get("/api/pubsub/callback", app.PubsubVerifyHandler)
	r.Post("/api/pubsub/callback", app.PubsubNotifyHandler)

	log.Printf("pastellive-go 서버 시작: %s (DB backend=%s)", cfg.ListenAddr, cfg.DBBackend)
	if err := http.ListenAndServe(cfg.ListenAddr, r); err != nil {
		log.Fatal(err)
	}
}
