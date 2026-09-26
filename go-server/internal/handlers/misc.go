package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/data"
	"pastellive/internal/httputil"
)

const siteURL = "https://pastellive.co.kr"

func (a *App) ApiStaffRolesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	httputil.JSON(w, 200, a.Cfg.StaffRoleLabels)
}

func (a *App) UpdatesPageHandler(w http.ResponseWriter, r *http.Request) {
	err := a.Templates.Render(w, r, "updates.html", map[string]any{
		"request": requestContext(r),
	}, a.GenRepImageOverrides)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (a *App) ServiceWorkerHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(a.Cfg.StaticDir, "sw.js"))
}

func (a *App) RobotsTxtHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprint(w, "User-agent: *\nAllow: /\nSitemap: https://pastellive.co.kr/sitemap.xml\n")
}

func (a *App) SitemapXMLHandler(w http.ResponseWriter, r *http.Request) {
	type urlEntry struct{ loc, changefreq, priority string }
	urls := []urlEntry{
		{siteURL + "/", "hourly", "1.0"},
		{siteURL + "/gallery", "hourly", "0.8"},
		{siteURL + "/highlights", "hourly", "0.8"},
	}
	rows, err := a.DB.Query("SELECT member_name FROM members")
	if err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil && name != "" {
				urls = append(urls, urlEntry{siteURL + "/" + name + "/community", "daily", "0.6"})
			}
		}
		rows.Close()
	}

	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
	for _, v := range a.VideoPool.Get() {
		if v.VideoID == "" {
			continue
		}
		urls = append(urls, urlEntry{siteURL + "/watch/" + v.VideoID, "weekly", "0.5"})
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, u := range urls {
		b.WriteString(fmt.Sprintf("<url><loc>%s</loc><changefreq>%s</changefreq><priority>%s</priority></url>\n", u.loc, u.changefreq, u.priority))
	}
	b.WriteString("</urlset>")

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	fmt.Fprint(w, b.String())
}

func (a *App) PrivacyPolicyHandler(w http.ResponseWriter, r *http.Request) {
	if err := a.Templates.Render(w, r, "privacy.html", map[string]any{"request": requestContext(r)}, a.GenRepImageOverrides); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (a *App) TermsOfServiceHandler(w http.ResponseWriter, r *http.Request) {
	if err := a.Templates.Render(w, r, "terms.html", map[string]any{"request": requestContext(r)}, a.GenRepImageOverrides); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

var linkPreviewBots = []string{
	"discordbot", "telegrambot", "twitterbot", "facebookexternalhit",
	"slackbot", "whatsapp", "kakaotalk-scrap", "line-poker", "vkshare",
	"skypeuripreview", "redditbot", "pinterest", "linkedinbot", "embedly",
}

func (a *App) SharePageHandler(w http.ResponseWriter, r *http.Request) {
	contentType := chi.URLParam(r, "contentType")
	contentID := chi.URLParam(r, "contentID")

	title := "루미너스"
	description := "스텔라이브(StelLive) 팬이 운영하는 비공식 2차 창작 팬 사이트, 루미너스"
	image := siteURL + "/static/logo/logo.png"
	redirectTarget := "/"

	switch contentType {
	case "fanart":
		var t, desc, imgURL, nickname sql.NullString
		err := a.DB.QueryRow("SELECT title, description, image_url, nickname FROM fanart_gallery WHERE id = ? AND status = 'active'", contentID).
			Scan(&t, &desc, &imgURL, &nickname)
		if err == nil {
			if t.Valid && t.String != "" {
				title = t.String
			} else {
				title = "팬 갤러리 사진"
			}
			nick := "팬"
			if nickname.Valid && nickname.String != "" {
				nick = nickname.String
			}
			if desc.Valid && desc.String != "" {
				description = desc.String
			} else {
				description = nick + "님이 루미너스 팬 갤러리에 올린 사진입니다."
			}
			rawImage := image
			if imgURL.Valid && imgURL.String != "" {
				rawImage = imgURL.String
			}
			if strings.HasPrefix(rawImage, "http") {
				image = rawImage
			} else {
				image = siteURL + rawImage
			}
		}
		redirectTarget = "/?image=" + contentID

	case "video":
		a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
		for _, v := range a.VideoPool.Get() {
			if v.VideoID == contentID {
				if v.Title != "" {
					title = v.Title
				} else {
					title = "영상"
				}
				description = "루미너스에서 이 영상을 시청해보세요."
				if v.Thumbnail != "" {
					image = v.Thumbnail
				}
				break
			}
		}
		redirectTarget = "/watch/" + contentID + "?from=share"

	default:
		http.NotFound(w, r)
		return
	}

	ua := strings.ToLower(r.Header.Get("User-Agent"))
	isBot := false
	for _, b := range linkPreviewBots {
		if strings.Contains(ua, b) {
			isBot = true
			break
		}
	}

	if !isBot {
		http.Redirect(w, r, redirectTarget, http.StatusFound)
		return
	}

	err := a.Templates.Render(w, r, "share.html", map[string]any{
		"title":           title,
		"description":     description,
		"image":           image,
		"redirect_target": redirectTarget,
		"site_url":        siteURL,
		"share_url":       fmt.Sprintf("%s/share/%s/%s", siteURL, contentType, contentID),
	}, a.GenRepImageOverrides)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func computeLiveStatus(client *http.Client) map[string]bool {
	status := make(map[string]bool, len(data.SIDEBAR_MEMBERS))
	for _, m := range data.SIDEBAR_MEMBERS {
		if m.ChzzkID == "" {
			status[m.Name] = false
			continue
		}
		status[m.Name] = fetchChzzkLiveStatus(client, m.ChzzkID)
	}
	return status
}

func fetchChzzkLiveStatus(client *http.Client, chzzkID string) bool {
	url := "https://api.chzzk.naver.com/polling/v2/channels/" + chzzkID + "/live-status"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Content struct {
			Status string `json:"status"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	return body.Content.Status == "OPEN"
}

var liveStatusHTTPClient = &http.Client{Timeout: 5 * time.Second}

const liveStatusCacheKey = "live_status"
const liveStatusCacheTTL = 60 * time.Second

func (a *App) ApiLiveStatusHandler(w http.ResponseWriter, r *http.Request) {
	if cached, ok := a.Cache.Get(liveStatusCacheKey); ok {
		writeJSON(w, cached)
		return
	}
	status := computeLiveStatus(liveStatusHTTPClient)
	a.Cache.Set(liveStatusCacheKey, status, liveStatusCacheTTL)
	writeJSON(w, status)
}

func (a *App) ApiCspReportHandler(w http.ResponseWriter, r *http.Request) {
	body := make([]byte, 1000)
	n, _ := r.Body.Read(body)
	if n > 0 {
		log.Printf("[CSP 위반 보고] %s", string(body[:n]))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) FaviconHandler(w http.ResponseWriter, r *http.Request) {
	icoPath := filepath.Join(a.Cfg.StaticDir, "favicon.ico")
	if _, err := os.Stat(icoPath); err == nil {
		w.Header().Set("Content-Type", "image/vnd.microsoft.icon")
		http.ServeFile(w, r, icoPath)
		return
	}
	pngPath := filepath.Join(a.Cfg.StaticDir, "icons", "icon-192.png")
	if _, err := os.Stat(pngPath); err == nil {
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, pngPath)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
