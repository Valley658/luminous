package httputil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchRemoteImage는 사용자가 붙여넣은 "이미지 링크"를 서버가 대신 다운로드해서
// 바이트로 돌려준다(루미에게 사진 인식을 시키려면 결국 원본 바이트가 있어야
// 하는데, 브라우저에서 <img>로 미리보기는 되어도 캔버스로 픽셀을 뽑아내는 건
// CORS 헤더가 없는 대부분의 이미지 CDN에서는 막혀있기 때문).
//
// 사용자가 준 URL을 서버가 그대로 가져오는 기능은 SSRF(서버가 공격자 대신
// 내부망/로컬호스트에 요청을 보내게 만드는 공격)에 취약해지기 쉬워서, 아래
// 안전장치를 전부 통과해야만 실제로 요청을 보낸다:
//   - http/https 스킴만 허용
//   - 호스트를 실제로 연결하는 "그 순간"에 IP를 다시 확인해서 사설/루프백/
//     링크로컬 대역이면 거부 (DNS에 한 번 물어보고 통과시킨 뒤 다시 다른 IP로
//     연결되는 "DNS 리바인딩" 우회를 막기 위해, LookupIP와 실제 다이얼을
//     한 번에 같은 DialContext 안에서 처리함)
//   - 리다이렉트도 매 홉마다 같은 검사를 다시 적용(최대 3홉)
//   - 응답 Content-Type이 image/*가 아니면 거부
//   - 응답 바이트 수 상한(기본 8MB) 초과 시 중단
var ErrRemoteImageBlocked = errors.New("이 링크는 가져올 수 없어(내부 주소이거나 이미지가 아니야)")

const remoteImageMaxRedirects = 3

func isDisallowedHostIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsMulticast() {
		return true
	}
	// 클라우드 메타데이터 엔드포인트(169.254.169.254 등)는 위 링크로컬 체크로
	// 이미 걸러지지만, 혹시 몰라 명시적으로 한 번 더 확인.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 169 && ip4[1] == 254 {
		return true
	}
	return false
}

func newSafeImageHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// 이미 IP 리터럴이면 그대로 검사, 아니면 지금 이 순간 다시 조회.
			var candidates []net.IP
			if ip := net.ParseIP(host); ip != nil {
				candidates = []net.IP{ip}
			} else {
				addrs, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
				if lookupErr != nil {
					return nil, lookupErr
				}
				for _, a := range addrs {
					candidates = append(candidates, a.IP)
				}
			}
			if len(candidates) == 0 {
				return nil, ErrRemoteImageBlocked
			}
			var chosen net.IP
			for _, ip := range candidates {
				if isDisallowedHostIP(ip) {
					return nil, ErrRemoteImageBlocked
				}
				if chosen == nil {
					chosen = ip
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(chosen.String(), port))
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= remoteImageMaxRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return ErrRemoteImageBlocked
			}
			return nil
		},
	}
}

// FetchRemoteImage는 검증을 통과하면 (이미지 바이트, Content-Type, nil)을,
// 아니면 (nil, "", err)를 돌려준다.
func FetchRemoteImage(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, "", errors.New("empty url")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, "", ErrRemoteImageBlocked
	}
	if parsed.Hostname() == "" {
		return nil, "", ErrRemoteImageBlocked
	}
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}

	client := newSafeImageHTTPClient(10 * time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	// 일부 이미지 CDN(구글 gstatic 등)은 브라우저스러운 User-Agent가 아니면
	// 막거나 다른 응답을 준다.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PastelliveBot/1.0; +https://pastellive.co.kr)")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("remote image fetch failed: %s", resp.Status)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return nil, "", errors.New("이 링크는 이미지가 아닌 것 같아")
	}

	limited := io.LimitReader(resp.Body, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxBytes {
		return nil, "", errors.New("이미지가 너무 커")
	}
	return body, contentType, nil
}
