// Package websearch gives the local AI a way to ground its answers in real,
// current information instead of only what a small local model happens to
// know. It does NOT use a headless browser (Selenium/Chrome etc.) - that
// would need a whole extra runtime and a lot more CPU/RAM on a server that's
// already CPU-only and shared with the live site. Search-engine results
// pages are plain server-rendered HTML, so a normal HTTP GET plus some
// light text extraction is enough; nothing here executes JavaScript.
package websearch

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Result struct {
	Title   string
	Snippet string
}

type Client struct {
	httpClient *http.Client
	enabled    bool
}

func New(enabled bool, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		enabled:    enabled,
	}
}

func (c *Client) Enabled() bool { return c.enabled }

var (
	tagPattern      = regexp.MustCompile(`<[^>]*>`)
	snippetPattern  = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</a>`)
	titlePattern    = regexp.MustCompile(`(?s)class="result__a"[^>]*>(.*?)</a>`)
	whitespacePatt2 = regexp.MustCompile(`\s+`)
)

func cleanText(s string) string {
	s = tagPattern.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(whitespacePatt2.ReplaceAllString(s, " "))
}

// Search does a lightweight, JS-free HTML scrape of DuckDuckGo's plain HTML
// results endpoint and pulls out the top few titles/snippets. Best-effort:
// any failure (network, timeout, markup change) just returns nil so the
// caller falls back to the model's own knowledge instead of erroring out.
func (c *Client) Search(ctx context.Context, query string, maxResults int) []Result {
	if !c.enabled || strings.TrimSpace(query) == "" {
		return nil
	}
	reqURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) LuminousBot/1.0 (+https://pastellive.co.kr)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	if err != nil {
		return nil
	}
	pageHTML := string(body)

	titles := titlePattern.FindAllStringSubmatch(pageHTML, -1)
	snippets := snippetPattern.FindAllStringSubmatch(pageHTML, -1)

	n := len(snippets)
	if len(titles) < n {
		n = len(titles)
	}
	if maxResults > 0 && n > maxResults {
		n = maxResults
	}
	if n <= 0 {
		return nil
	}
	out := make([]Result, 0, n)
	for i := 0; i < n; i++ {
		title := cleanText(titles[i][1])
		snippet := cleanText(snippets[i][1])
		if title == "" && snippet == "" {
			continue
		}
		out = append(out, Result{Title: title, Snippet: snippet})
	}
	return out
}
