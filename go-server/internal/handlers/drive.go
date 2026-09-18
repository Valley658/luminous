package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pastellive/internal/drive"
)

const drivePlaceholderSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180" viewBox="0 0 320 180">` +
	`<rect width="320" height="180" fill="#141414"/>` +
	`<g fill="none" stroke="#444" stroke-width="2">` +
	`<rect x="120" y="62" width="80" height="56" rx="6"/>` +
	`<circle cx="140" cy="80" r="6"/>` +
	`<path d="M120 108l22-20 18 14 20-18 20 24"/>` +
	`</g></svg>`

func drivePlaceholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=21600")
	_, _ = w.Write([]byte(drivePlaceholderSVG))
}

func (a *App) DriveThumb(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "fileID")
	if !drive.FileIDPattern.MatchString(fileID) {
		http.NotFound(w, r)
		return
	}

	thumbLink := r.URL.Query().Get("src")
	if thumbLink != "" {
		if u, err := url.Parse(thumbLink); err == nil {
			host := strings.ToLower(u.Hostname())
			if host != "googleusercontent.com" && !strings.HasSuffix(host, ".googleusercontent.com") {
				thumbLink = ""
			}
		} else {
			thumbLink = ""
		}
	}

	if thumbLink == "" {
		if a.Cfg.GoogleDriveAPIKey == "" {
			drivePlaceholder(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		link, ok := fetchDriveThumbnailLink(ctx, a.Cfg.GoogleDriveAPIKey, fileID)
		if !ok || link == "" {

			drivePlaceholder(w)
			return
		}
		thumbLink = link
	}

	if idx := strings.LastIndex(thumbLink, "=s"); idx != -1 {
		thumbLink = thumbLink[:idx] + "=s600"
	}
	http.Redirect(w, r, thumbLink, http.StatusFound)
}

func fetchDriveThumbnailLink(ctx context.Context, apiKey, fileID string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.googleapis.com/drive/v3/files/"+url.PathEscape(fileID)+"?fields=thumbnailLink&key="+apiKey, nil)
	if err != nil {
		return "", false
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	var out struct {
		ThumbnailLink string `json:"thumbnailLink"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", false
	}
	return out.ThumbnailLink, true
}

func (a *App) DriveVideoTitle(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "fileID")
	if !drive.FileIDPattern.MatchString(fileID) {
		writeJSON(w, map[string]any{"title": nil})
		return
	}

	var title, description, tags sql.NullString
	err := a.DB.QueryRow("SELECT title, description, tags FROM channel_videos WHERE video_url = ? LIMIT 1", fileID).
		Scan(&title, &description, &tags)
	if err == nil {
		t := title.String
		if t == "" {
			t = "제목 없음"
		}
		writeJSON(w, map[string]any{"title": t, "description": description.String, "tags": tags.String})
		return
	}

	if a.Cfg.GoogleDriveAPIKey == "" {
		writeJSON(w, map[string]any{"title": nil})
		return
	}
	driveTitle, ok := drive.GetVideoTitleFromDrive(r.Context(), a.Cache, a.Cfg.GoogleDriveAPIKey, fileID)
	if !ok {
		writeJSON(w, map[string]any{"title": nil})
		return
	}
	writeJSON(w, map[string]any{"title": driveTitle, "description": "", "tags": ""})
}
