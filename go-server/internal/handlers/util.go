package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

func equalFoldTrim(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

func sanitizeUserHTML(text string) string {
	if text == "" {
		return text
	}
	return htmlTagPattern.ReplaceAllString(text, "")
}

func decodeJSONBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	_ = json.NewDecoder(r.Body).Decode(dst)
	return nil
}
