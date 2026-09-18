package moderation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Detection struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

type Result struct {
	Verdict        string
	Detections     []Detection
	HighRiskScore  float64
	AmbiguousScore float64
}

type Client struct {
	baseURL string
	client  *http.Client
	enabled bool
}

func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
		enabled: baseURL != "",
	}
}

func (c *Client) Enabled() bool { return c.enabled }

func (c *Client) Moderate(absPath string) (Result, bool) {
	if !c.enabled {
		return Result{Verdict: "clear"}, false
	}
	body, err := json.Marshal(map[string]any{"path": absPath})
	if err != nil {
		return Result{Verdict: "clear"}, false
	}
	resp, err := c.client.Post(c.baseURL+"/moderate", "application/json", bytes.NewReader(body))
	if err != nil {
		return Result{Verdict: "clear"}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Result{Verdict: "clear"}, false
	}
	var data struct {
		Success        bool        `json:"success"`
		Verdict        string      `json:"verdict"`
		Detections     []Detection `json:"detections"`
		HighRiskScore  float64     `json:"highRiskScore"`
		AmbiguousScore float64     `json:"ambiguousScore"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || !data.Success {
		return Result{Verdict: "clear"}, false
	}
	return Result{
		Verdict:        data.Verdict,
		Detections:     data.Detections,
		HighRiskScore:  data.HighRiskScore,
		AmbiguousScore: data.AmbiguousScore,
	}, true
}

func (r Result) Summary() string {
	if len(r.Detections) == 0 {
		return "AI 자동 탐지"
	}
	parts := make([]string, 0, len(r.Detections))
	for _, d := range r.Detections {
		parts = append(parts, d.Label)
	}
	s := "AI 자동 탐지: " + strings.Join(parts, ", ")
	runes := []rune(s)
	if len(runes) > 100 {
		s = string(runes[:97]) + "..."
	}
	return s
}
