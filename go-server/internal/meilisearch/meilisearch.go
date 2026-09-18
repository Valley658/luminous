package meilisearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var choseongList = []rune{'ㄱ', 'ㄲ', 'ㄴ', 'ㄷ', 'ㄸ', 'ㄹ', 'ㅁ', 'ㅂ', 'ㅃ', 'ㅅ', 'ㅆ',
	'ㅇ', 'ㅈ', 'ㅉ', 'ㅊ', 'ㅋ', 'ㅌ', 'ㅍ', 'ㅎ'}

func ExtractChoseong(text string) string {
	var parts []string
	for _, word := range strings.Fields(text) {
		var cho strings.Builder
		for _, ch := range word {
			if ch >= 0xAC00 && ch <= 0xD7A3 {
				cho.WriteRune(choseongList[(ch-0xAC00)/588])
			}
		}
		if cho.Len() > 0 {
			parts = append(parts, cho.String())
		}
	}
	full := strings.Join(parts, "")
	if full != "" {
		found := false
		for _, p := range parts {
			if p == full {
				found = true
				break
			}
		}
		if !found {
			parts = append(parts, full)
		}
	}
	return strings.Join(parts, " ")
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client

	healthy bool
}

func New(url, apiKey string) *Client {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if url == "" {
		return nil
	}
	c := &Client{baseURL: url, apiKey: apiKey, http: &http.Client{Timeout: 5 * time.Second}}
	c.healthy = c.checkHealth()
	return c
}

func (c *Client) checkHealth() bool {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func (c *Client) Available() bool {
	return c != nil && c.healthy
}

func (c *Client) CheckHealthNow() bool {
	if c == nil {
		return false
	}
	c.healthy = c.checkHealth()
	return c.healthy
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return c.http.Do(req)
}

func (c *Client) Configure(index string, searchable, filterable []string, synonyms map[string][]string, maxTotalHits int) {
	if !c.Available() {
		return
	}
	base := fmt.Sprintf("/indexes/%s/settings", index)
	if resp, err := c.do(http.MethodPut, base+"/searchable-attributes", searchable); err == nil {
		resp.Body.Close()
	}
	if resp, err := c.do(http.MethodPut, base+"/filterable-attributes", filterable); err == nil {
		resp.Body.Close()
	}
	if resp, err := c.do(http.MethodPut, base+"/synonyms", synonyms); err == nil {
		resp.Body.Close()
	}
	if resp, err := c.do(http.MethodPut, base+"/pagination", map[string]any{"maxTotalHits": maxTotalHits}); err == nil {
		resp.Body.Close()
	}
}

func (c *Client) AddDocuments(index string, documents []map[string]any) error {
	if !c.Available() || len(documents) == 0 {
		return nil
	}
	resp, err := c.do(http.MethodPost, fmt.Sprintf("/indexes/%s/documents?primaryKey=id", index), documents)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("meilisearch add_documents status %d", resp.StatusCode)
	}
	return nil
}

type SearchHit = map[string]any

type searchResponse struct {
	Hits []SearchHit `json:"hits"`
}

func (c *Client) Search(index, query string, limit int) ([]SearchHit, error) {
	if !c.Available() {
		return nil, fmt.Errorf("meilisearch not available")
	}
	resp, err := c.do(http.MethodPost, fmt.Sprintf("/indexes/%s/search", index), map[string]any{"q": query, "limit": limit})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meilisearch search status %d", resp.StatusCode)
	}
	var out searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Hits, nil
}
