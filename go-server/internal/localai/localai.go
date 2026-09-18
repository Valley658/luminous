// Package localai talks to a locally-hosted LLM runtime (Ollama, run on the
// same Windows machine as this server) so the "루미" mascot can answer
// visitor questions without sending anything to a third-party cloud API.
//
// This is intentionally conservative: the production server is CPU-only and
// also has to keep serving real visitors, so at most one inference request
// runs at a time. 여러 방문자가 동시에 루미에게 물어봐도 서로 에러로 튕기지
// 않고, 한 명씩 순서대로(먼저 온 순서로) 처리되도록 뒤에 온 요청은 앞 요청이
// 끝날 때까지 기다린다(대기시간은 호출하는 쪽의 context 타임아웃으로 제한됨 -
// go-server/internal/handlers/lumi_ai.go의 lumiAIRequestTimeout 참고).
package localai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	model   string
	// client is used for normal visitor requests - bounded by the configured
	// per-request timeout. warmupClient is used only for WarmUp: the very
	// first request to a freshly-started Ollama has to load the model off
	// disk into RAM first, which can take much longer than a normal answer,
	// so it gets its own generous timeout instead of failing visitors' own
	// requests early.
	client       *http.Client
	warmupClient *http.Client
	sem          chan struct{}
	numPredict   int
}

func New(baseURL, model string, timeout time.Duration, numPredict int) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "qwen2.5:3b-instruct-q4_K_M"
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	if numPredict <= 0 {
		numPredict = 220
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		model:        model,
		client:       &http.Client{Timeout: timeout},
		warmupClient: &http.Client{Timeout: 3 * time.Minute},
		sem:          make(chan struct{}, 1), // CPU 전용 서버라 동시에 1건만 추론
		numPredict:   numPredict,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

func (c *Client) doChat(ctx context.Context, httpClient *http.Client, systemPrompt, userPrompt string, numPredict int) (string, error) {
	// 자리가 하나뿐인 세마포어라 이미 다른 요청이 쓰고 있으면 여기서 기다린다
	// (바로 에러 내지 않음) - 그래야 여러 명이 동시에 물어봐도 한 명씩 순서대로
	// 답을 받지, 뒤에 온 사람이 "지금 바쁘다"고 튕겨나가지 않는다. 너무 오래
	// 기다리게 되는 상황은 호출자의 ctx 타임아웃이 대신 끊어준다.
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: false,
		Options: map[string]any{
			"temperature": 0.6,
			"num_predict": numPredict,
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("local ai http status " + resp.Status)
	}

	var data chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.Error != "" {
		return "", errors.New(data.Error)
	}
	reply := strings.TrimSpace(data.Message.Content)
	if reply == "" {
		return "", errors.New("local ai empty reply")
	}
	return reply, nil
}

// Ask sends a system+user prompt pair to the local model and returns its
// reply. If the single inference slot is already in use, this call waits its
// turn (queued, roughly FIFO) instead of failing immediately - the caller's
// context timeout is what eventually gives up if the queue is too backed up.
func (c *Client) Ask(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return c.doChat(ctx, c.client, systemPrompt, userPrompt, c.numPredict)
}

// WarmUp sends a tiny throwaway request so Ollama loads the model into RAM
// right away, instead of the first real visitor's question paying for that
// (which can take much longer than a normal answer and would otherwise time
// out and look like Ollama is broken/off). Safe to call even if Ollama isn't
// running yet - it just returns an error, which the caller should only log.
func (c *Client) WarmUp(ctx context.Context) error {
	_, err := c.doChat(ctx, c.warmupClient, "너는 도움이 되는 비서야.", "안녕", 8)
	return err
}

// Available does a lightweight reachability check (used by health/debug
// endpoints, not on the hot path).
func (c *Client) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IsUnreachable reports whether err looks like Ollama isn't running at all
// (connection refused, DNS failure, ...) as opposed to just being slow.
func IsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connectex:")
}

// IsTimeout reports whether err looks like the request ran out of time
// (model likely still loading into memory, or just slow on CPU) as opposed
// to Ollama being unreachable.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
