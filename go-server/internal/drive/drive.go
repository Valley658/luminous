package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"pastellive/internal/cache"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

type Video struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
	VideoID   string `json:"videoId"`
	ID        string `json:"id"`
	IsShort   bool   `json:"is_short"`
	IsDrive   bool   `json:"is_drive"`
}

type filesListResponse struct {
	Files []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ThumbnailLink string `json:"thumbnailLink"`
	} `json:"files"`
	NextPageToken string `json:"nextPageToken"`
}

func FetchVideos(ctx context.Context, c *cache.Store, apiKey, folderID, pageToken string) (videos []Video, nextPageToken string) {
	if apiKey == "" {
		return nil, ""
	}
	cacheKey := fmt.Sprintf("drive_%s_%s", folderID, pageToken)
	if cached, ok := c.Get(cacheKey); ok {
		if r, ok := cached.([2]any); ok {
			if vs, ok := r[0].([]Video); ok {
				npt, _ := r[1].(string)
				return vs, npt
			}
		}
	}

	q := fmt.Sprintf("'%s' in parents and mimeType contains 'video/'", folderID)
	params := url.Values{}
	params.Set("q", q)
	params.Set("pageSize", "50")
	params.Set("fields", "nextPageToken, files(id, name, thumbnailLink)")
	params.Set("orderBy", "createdTime desc")
	params.Set("key", apiKey)
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	reqURL := "https://www.googleapis.com/drive/v3/files?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		log.Printf("Google Drive API Error: %v", err)
		return nil, ""
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Google Drive API Error: %v", err)
		return nil, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("Google Drive API Error: status %d", resp.StatusCode)
		return nil, ""
	}
	var out filesListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Printf("Google Drive API Error: %v", err)
		return nil, ""
	}

	videos = make([]Video, 0, len(out.Files))
	for _, f := range out.Files {
		title := f.Name
		if title == "" {
			title = "제목 없음"
		}
		var thumb string
		if f.ThumbnailLink != "" {
			thumb = "/api/drive_thumb/" + f.ID + "?src=" + url.QueryEscape(f.ThumbnailLink)
		} else {
			thumb = "/api/drive_thumb/" + f.ID
		}
		videos = append(videos, Video{Title: title, Thumbnail: thumb, VideoID: f.ID, ID: f.ID, IsShort: false, IsDrive: true})
	}
	c.Set(cacheKey, [2]any{videos, out.NextPageToken}, cache.DefaultTTL)
	return videos, out.NextPageToken
}

func GetVideoTitleFromDrive(ctx context.Context, c *cache.Store, apiKey, fileID string) (title string, ok bool) {
	if apiKey == "" {
		return "", false
	}
	cacheKey := "drive_title_" + fileID
	if cached, ok := c.Get(cacheKey); ok {
		if s, ok := cached.(string); ok {
			return s, true
		}
	}
	reqURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?fields=name&key=%s", url.PathEscape(fileID), apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("드라이브 영상 제목 조회 오류: %v", err)
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("드라이브 영상 제목 조회 오류: status %d", resp.StatusCode)
		return "", false
	}
	var out struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", false
	}
	title = out.Name
	if title == "" {
		title = "제목 없음"
	}
	c.Set(cacheKey, title, cache.DefaultTTL)
	return title, true
}

var FileIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func StreamProxyHandler(apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID := strings.TrimPrefix(r.URL.Path, "/api/drive_video_stream/")
		if !FileIDPattern.MatchString(fileID) {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		if apiKey == "" {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}

		driveURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?alt=media&key=%s", url.PathEscape(fileID), apiKey)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, driveURL, nil)
		if err != nil {
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
			return
		}
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}

		upstream, err := httpClient.Do(req)
		if err != nil {
			log.Printf("drive_video_stream upstream 요청 실패: file_id=%s - %v", fileID, err)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
			return
		}
		defer upstream.Body.Close()

		if upstream.StatusCode != 200 && upstream.StatusCode != 206 {
			log.Printf("drive_video_stream upstream 오류: file_id=%s status=%d", fileID, upstream.StatusCode)
			if upstream.StatusCode == 404 {
				http.NotFound(w, r)
			} else {
				http.Error(w, "Bad Gateway", http.StatusBadGateway)
			}
			return
		}

		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
			if v := upstream.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		if w.Header().Get("Accept-Ranges") == "" {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(upstream.StatusCode)

		buf := make([]byte, 65536)
		for {
			n, rerr := upstream.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			if rerr != nil {
				return
			}
		}
	}
}
