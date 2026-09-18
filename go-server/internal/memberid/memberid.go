// Package memberid talks to a locally-hosted image-identification service
// (Python, see services/member-id-service/ at the project root) so 루미 AI
// 채팅이 방문자가 올린 사진 속 스텔라이브 멤버를 알아맞힐 수 있게 해준다.
//
// 그 서비스는 CLIP 이미지 임베딩으로 업로드된 사진과 각 멤버의 대표 사진을
// 비교해서 제일 가까운 멤버(또는 확실치 않으면 없음)를 돌려준다 - 자세한
// 설명은 services/member-id-service/src/member_id_server.py 상단 주석 참고.
package memberid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  &http.Client{Timeout: timeout},
		enabled: strings.TrimSpace(baseURL) != "",
	}
}

func (c *Client) Enabled() bool { return c.enabled }

type identifyResponse struct {
	Member string  `json:"member"`
	Score  float64 `json:"score"`
	Phash  string  `json:"phash"`
	Error  string  `json:"error"`
}

// Identify는 사진(원본 바이트)을 보내서 가장 닮은 멤버 이름과 유사도 점수,
// 그리고 지각적 해시(phash, 16진수 문자열)를 받아온다. 확실히 닮은 멤버가
// 없으면 member는 빈 문자열("")로 온다(에러가 아님 - 정상적으로 "모르겠다"고
// 판단한 것). phash는 호출하는 쪽(lumi_ai.go)이 "완전히 같은/거의 같은
// 사진으로 같은 질문을 또 받으면 LLM 응답 자체를 캐시해서 재사용"하는 데 씀.
func (c *Client) Identify(ctx context.Context, imageBytes []byte) (member string, score float64, phash string, err error) {
	if !c.enabled {
		return "", 0, "", errors.New("memberid disabled")
	}
	if len(imageBytes) == 0 {
		return "", 0, "", errors.New("empty image")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/identify", bytes.NewReader(imageBytes))
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()

	var parsed identifyResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&parsed); decodeErr != nil {
		return "", 0, "", decodeErr
	}
	if resp.StatusCode != http.StatusOK {
		msg := parsed.Error
		if msg == "" {
			msg = resp.Status
		}
		return "", 0, "", errors.New("memberid: " + msg)
	}
	return parsed.Member, parsed.Score, parsed.Phash, nil
}

// Available은 health/디버그용 가벼운 확인.
func (c *Client) Available(ctx context.Context) bool {
	if !c.enabled {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
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
