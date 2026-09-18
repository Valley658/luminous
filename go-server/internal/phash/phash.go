package phash

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	client  *http.Client
	enabled bool
}

func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
		enabled: baseURL != "",
	}
}

func (c *Client) Enabled() bool { return c.enabled }

func (c *Client) Hash(grayBytes []byte) (string, bool) {
	if !c.enabled {
		return "", false
	}
	resp, err := c.client.Post(c.baseURL+"/hash", "application/octet-stream", bytes.NewReader(grayBytes))
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	var data struct {
		Success bool   `json:"success"`
		Hash    string `json:"hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || !data.Success {
		return "", false
	}
	return data.Hash, true
}
