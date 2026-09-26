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

// [2026-09-26 보안 점검] heic/heif는 Go 표준 라이브러리로 안전하게
// decode 검증을 할 수 없어서(전용 디코더 없음) 허용 목록에서 제거했다.
// 실제 accept/reject 판단은 확장자가 아니라 internal/imgvalidate가
// 매직 바이트+전체 디코드로 수행하므로, 이 목록은 "미리 빠르게 걸러서
// 불필요한 업로드를 막는" UX용 힌트일 뿐 보안 경계가 아니다.
var allowedImageExts = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "webp": true, "gif": true,
}

func IsAllowedImageExt(ext string) bool {
	return allowedImageExts[strings.ToLower(ext)]
}

// ProcessUploadedImage는 이미 internal/imgvalidate로 안전성이 검증/재인코딩된
// filePath를 받아, 외부 Java 이미지 서비스로 리사이즈/webp·avif 변환 같은
// "최적화"만 시도한다. 이 함수는 더 이상 보안 검증을 수행하지 않는다 -
// 외부 서비스가 실패하거나 응답하지 않아도(optimized=false) 호출부는 이미
// 검증된 filePath를 그대로 쓰면 안전하다.
func (c *Client) ProcessUploadedImage(filePath string, maxDimension, quality int) (newPath string, ok bool, optimized bool) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	if ext == "gif" {
		if webpPath, ok := c.runGifJobSync(filePath, quality); ok {
			return webpPath, true, true
		}
		return "", false, false
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
	if called && webpPath != filePath {
		_ = os.Remove(filePath)
		return webpPath, true, true
	}
	return "", false, false
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
