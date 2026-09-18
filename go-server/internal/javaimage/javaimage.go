package javaimage

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: timeout}}
}

func (c *Client) Call(endpoint string, payload map[string]any) (map[string]any, bool) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	resp, err := c.client.Post(c.baseURL+endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, false
	}
	success, _ := data["success"].(bool)
	if !success {
		return nil, false
	}
	return data, true
}

var allowedImageExts = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "webp": true, "gif": true, "heic": true, "heif": true,
}

func IsAllowedImageExt(ext string) bool {
	return allowedImageExts[strings.ToLower(ext)]
}

func (c *Client) ProcessUploadedImage(filePath string, maxDimension, quality int) (newPath string, ok bool, rejected bool) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	if ext == "gif" {

		if webpPath, ok := c.runGifJobSync(filePath, quality); ok {
			return webpPath, true, false
		}
		return "", true, false
	}

	webpPath := filePath
	if ext != "webp" {
		webpPath = strings.TrimSuffix(filePath, filepath.Ext(filePath)) + ".webp"
	}
	_, called := c.Call("/process-image", map[string]any{
		"path": absPath(filePath), "outputPath": absPath(webpPath),
		"maxDimension": maxDimension, "quality": quality,
		"avif": true, "avifQuality": quality,
	})
	if called {
		if webpPath != filePath {
			_ = os.Remove(filePath)
			return webpPath, true, false
		}
		return "", true, false
	}

	return "", true, false
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
