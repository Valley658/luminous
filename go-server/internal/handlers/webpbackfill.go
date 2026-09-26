package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pastellive/internal/httputil"
)

type webpBackfillItem struct {
	Table  string `json:"table"`
	ID     int64  `json:"id"`
	Old    string `json:"old"`
	New    string `json:"new,omitempty"`
	Status string `json:"status"`
}

func (a *App) resolveStaticPath(relURL string) (string, bool) {
	relURL = strings.TrimSpace(relURL)
	if !strings.HasPrefix(relURL, "/static/") {
		return "", false
	}
	return filepath.Join(a.Cfg.ProjectDir, filepath.FromSlash(strings.TrimPrefix(relURL, "/"))), true
}

var webpBackfillConvertibleExt = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "heic": true, "heif": true, "gif": true,
}

func (a *App) backfillConvertOne(relURL string, maxDimension, quality int) (newURL string, status string) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(relURL), "."))
	if ext == "" || ext == "webp" {
		return relURL, "skip_already_webp"
	}
	if !webpBackfillConvertibleExt[ext] {
		return relURL, "skip_unsupported_ext"
	}
	absPath, ok := a.resolveStaticPath(relURL)
	if !ok {
		return relURL, "skip_bad_path"
	}
	if _, err := os.Stat(absPath); err != nil {
		return relURL, "skip_file_missing"
	}
	newPath, ok, optimized := a.JavaImage.ProcessUploadedImage(absPath, maxDimension, quality)
	if !optimized || !ok || newPath == "" {
		return relURL, "failed_conversion"
	}
	return strings.TrimSuffix(relURL, filepath.Ext(relURL)) + ".webp", "converted"
}

func (a *App) BackfillNonWebpImages(dryRun bool) []webpBackfillItem {
	var results []webpBackfillItem

	type idURLRow struct {
		id  int64
		url string
	}
	fetch := func(query string) []idURLRow {
		rows, err := a.DB.Query(query)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var out []idURLRow
		for rows.Next() {
			var rr idURLRow
			if rows.Scan(&rr.id, &rr.url) == nil {
				out = append(out, rr)
			}
		}
		return out
	}

	for _, rr := range fetch("SELECT id, image_url FROM fanart_gallery WHERE image_url IS NOT NULL AND image_url != '' AND image_url NOT LIKE '%.webp'") {
		newURL, status := a.backfillConvertOne(rr.url, 1920, 85)
		results = append(results, webpBackfillItem{"fanart_gallery.image_url", rr.id, rr.url, newURL, status})
		if status == "converted" && !dryRun {
			if _, err := a.DB.Exec("UPDATE fanart_gallery SET image_url=? WHERE id=?", newURL, rr.id); err == nil {
				_, _ = a.DB.Exec("UPDATE fanart_reports SET fanart_image_url=? WHERE fanart_image_url=?", newURL, rr.url)
			}
		}
	}

	for _, rr := range fetch("SELECT id, picture FROM users WHERE picture IS NOT NULL AND picture != '' AND picture NOT LIKE '%.webp'") {
		newURL, status := a.backfillConvertOne(rr.url, 512, 85)
		results = append(results, webpBackfillItem{"users.picture", rr.id, rr.url, newURL, status})
		if status == "converted" && !dryRun {
			if _, err := a.DB.Exec("UPDATE users SET picture=? WHERE id=?", newURL, rr.id); err == nil {
				_, _ = a.DB.Exec("UPDATE fanart_comments SET picture=? WHERE picture=?", newURL, rr.url)
				_, _ = a.DB.Exec("UPDATE community_posts SET picture=? WHERE picture=?", newURL, rr.url)
				_, _ = a.DB.Exec("UPDATE community_comments SET picture=? WHERE picture=?", newURL, rr.url)
				_, _ = a.DB.Exec("UPDATE video_comments SET picture=? WHERE picture=?", newURL, rr.url)
			}
		}
	}

	for _, rr := range fetch("SELECT id, image_url FROM video_comments WHERE image_url IS NOT NULL AND image_url != '' AND image_url NOT LIKE '%.webp'") {
		newURL, status := a.backfillConvertOne(rr.url, 1920, 85)
		results = append(results, webpBackfillItem{"video_comments.image_url", rr.id, rr.url, newURL, status})
		if status == "converted" && !dryRun {
			_, _ = a.DB.Exec("UPDATE video_comments SET image_url=? WHERE id=?", newURL, rr.id)
		}
	}

	postRows, err := a.DB.Query("SELECT id, image_url FROM community_posts WHERE image_url IS NOT NULL AND image_url != ''")
	if err == nil {
		type postRow struct {
			id  int64
			raw string
		}
		var posts []postRow
		for postRows.Next() {
			var pr postRow
			if postRows.Scan(&pr.id, &pr.raw) == nil {
				posts = append(posts, pr)
			}
		}
		postRows.Close()
		for _, pr := range posts {
			var arr []string
			if json.Unmarshal([]byte(pr.raw), &arr) != nil || len(arr) == 0 {
				continue
			}
			changed := false
			for i, u := range arr {
				newURL, status := a.backfillConvertOne(u, 1920, 85)
				if status == "converted" {
					results = append(results, webpBackfillItem{"community_posts.image_url[]", pr.id, u, newURL, status})
					arr[i] = newURL
					changed = true
				} else if status != "skip_already_webp" {
					results = append(results, webpBackfillItem{"community_posts.image_url[]", pr.id, u, "", status})
				}
			}
			if changed && !dryRun {
				if b, err := json.Marshal(arr); err == nil {
					_, _ = a.DB.Exec("UPDATE community_posts SET image_url=? WHERE id=?", string(b), pr.id)
				}
			}
		}
	}

	return results
}

func (a *App) runWebpBackfillJob() {
	results := a.BackfillNonWebpImages(false)
	converted, failed := 0, 0
	for _, it := range results {
		switch it.Status {
		case "converted":
			converted++
		case "failed_conversion", "skip_file_missing", "skip_bad_path":
			failed++
		}
	}
	if converted > 0 || failed > 0 {
		log.Printf("WebP 백필: 변환 %d건, 실패/건너뜀 %d건 (전체 %d건 검토)", converted, failed, len(results))
	}
}

func (a *App) ApiAdminBackfillWebpHandler(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	var body struct {
		Apply bool `json:"apply"`
	}
	_ = decodeJSONBody(r, &body)
	results := a.BackfillNonWebpImages(!body.Apply)
	converted, failed := 0, 0
	for _, it := range results {
		if it.Status == "converted" {
			converted++
		} else if it.Status != "skip_already_webp" {
			failed++
		}
	}
	writeJSON(w, map[string]any{
		"success":   true,
		"dry_run":   !body.Apply,
		"converted": converted,
		"issues":    failed,
		"items":     results,
	})
}
