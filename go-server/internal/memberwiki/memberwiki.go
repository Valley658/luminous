// Package memberwiki는 루미가 로컬에 정보가 없는 멤버(주로 강지/김블루 같은
// "기타" 항목이나 앞으로 새로 추가될 멤버)에 대해 질문받았을 때, 나무위키에서
// 그 문서를 자동으로 가져와서 기본적인 사실(본명/생일/소속/경력 등)을 뽑아오는
// 아주 가벼운 스크레이퍼다.
//
// 셀레니움 같은 브라우저 자동화는 일부러 안 썼다 - 나무위키는 방문했을 때 이미
// 완성된 HTML을 그대로 내려주는 서버 렌더링 사이트라(자바스크립트를 실행해야만
// 보이는 내용이 아님), 그냥 HTTP GET 하나로 문서 전체를 받아올 수 있다. 무거운
// 크롬/셀레니움 실행파일을 새로 설치·관리할 필요가 없어서 훨씬 가볍고 빠르다.
//
// 정확도에 대한 주의: 나무위키는 누구나 수정 가능한 위키라 잘못된 정보나
// 장난이 섞여 있을 수 있다. 그래서 (1) 문서 전체 본문이 아니라 상단의
// "정보 상자"(본명/생일/소속 같은 표 형태 요약)만 뽑아오고, (2) 가져온 값은
// 항상 "나무위키에서 자동으로 가져온 정보"라고 출처를 표시해서 루미가 그걸
// 100% 확정된 사실처럼 단정하지 않게 하며, (3) 리도님이 member_profiles.json을
// 직접 고치면 그 값이 항상 우선이다(이 스크레이퍼는 "아직 아무것도 없을 때"만
// 채워 넣고, 이미 적힌 값은 절대 덮어쓰지 않음).
package memberwiki

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	fetchTimeout   = 5 * time.Second
	maxBodyBytes   = 4 << 20 // 4MB - 나무위키 문서가 아무리 길어도 이 정도면 충분
	userAgent      = "Mozilla/5.0 (compatible; LuminousLumiBot/1.0; +https://pastellive.co.kr) - 팬사이트 마스코트 루미가 멤버 기본 정보를 확인할 때만 씀"
	sourceLabelFmt = "(나무위키 %s 문서에서 방금 자동으로 가져온 정보 - 틀리거나 오래됐을 수 있음, 확정된 사실처럼 단정하지 말고 참고만 해)"
)

// 정보 상자에서 뽑아올 라벨 화이트리스트. 이 목록에 없는 행(발매 음반 표,
// 관련 문서 목록 같은 다른 표들)은 전부 무시한다 - 표 자체를 구조적으로
// 특정하는 대신(나무위키 프론트엔드가 자동 생성한 CSS 클래스 이름이라 언제든
// 바뀔 수 있어 불안정함), 알려진 라벨 텍스트로 필요한 행만 골라내는 방식이라
// 나무위키 쪽 디자인이 바뀌어도 비교적 안정적으로 동작한다.
// 종교/신체(체중 포함) 등 민감할 수 있는 개인 신상 항목은 일부러 화이트리스트에서
// 뺐다 - 실존 인물(강지/김블루 등)일 경우 위키에 있다고 해서 챗봇이 그대로
// 퍼나르기엔 조심스러운 정보라, 리도님이 member_profiles.json에 직접 적은
// 항목들과 같은 기준(신상 정보 최소화)으로 걸러낸다.
var knownInfoboxLabels = map[string]bool{
	"본명": true, "예명": true, "출생": true, "생일": true, "나이": true,
	"국적": true, "종족": true, "혈액형": true, "MBTI": true,
	"데뷔": true, "데뷔일": true, "첫 방송일": true, "경력": true,
	"소속": true, "소속사": true, "직업": true, "별명": true, "상징 색": true,
}

var (
	reOgDescription = regexp.MustCompile(`<meta property="og:description" content="([^"]*)"`)
	reRow           = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td[^>]*>\s*<div[^>]*>\s*<strong[^>]*>([^<]+)</strong>\s*</div>\s*</td>\s*<td[^>]*>(.*?)</td>\s*</tr>`)
	reTag           = regexp.MustCompile(`<[^>]+>`)
	reFootnote      = regexp.MustCompile(`\[\d+\]`)
	reSpace         = regexp.MustCompile(`\s+`)
	reStrayPipes    = regexp.MustCompile(`\s*\|\s*(\|\s*)*`)
)

// 정보 상자 행 안에 표(예: 플랫폼 팔로워/구독자 통계)가 중첩된 경우, 태그를
// 지우고 나면 의미 없는 "|" 구분자만 남는 경우가 있다 - 그런 잔여물을 정리해서
// 깨끗한 텍스트만 남긴다.
func stripTags(s string) string {
	s = reTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = reFootnote.ReplaceAllString(s, "")
	s = reStrayPipes.ReplaceAllString(s, " ")
	s = reSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// Result는 나무위키에서 뽑아낸, member_profiles.json Profile과 같은 모양으로
// 바로 쓸 수 있는 결과.
type Result struct {
	Bio     string
	Extra   string
	PageURL string
}

var httpClient = &http.Client{Timeout: fetchTimeout}

// FetchFromNamuwiki는 나무위키에서 name 문서를 가져와 정보 상자를 파싱한다.
// 문서가 없거나, 정보 상자를 하나도 못 찾았거나, 네트워크/시간 초과 등으로
// 실패하면 ok=false를 돌려준다(이 경우 호출 쪽은 그냥 조용히 넘어가면 됨 -
// 없는 정보를 억지로 지어낼 필요는 없음).
func FetchFromNamuwiki(ctx context.Context, name string) (result Result, ok bool) {
	pageURL := "https://namu.wiki/w/" + url.PathEscape(name)

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return Result{}, false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Result{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Result{}, false
	}
	text := string(body)

	var bio string
	if m := reOgDescription.FindStringSubmatch(text); len(m) == 2 {
		bio = html.UnescapeString(strings.TrimSpace(m[1]))
	}

	var parts []string
	for _, m := range reRow.FindAllStringSubmatch(text, -1) {
		label := strings.TrimSpace(html.UnescapeString(m[1]))
		if !knownInfoboxLabels[label] {
			continue
		}
		value := stripTags(m[2])
		if value == "" {
			continue
		}
		parts = append(parts, label+": "+value)
	}

	if bio == "" && len(parts) == 0 {
		return Result{}, false // 문서가 없거나(404), 알아볼 수 있는 형태가 아님
	}

	sourceNote := fmt.Sprintf(sourceLabelFmt, name)
	if bio != "" {
		bio = bio + " " + sourceNote
	} else {
		bio = sourceNote
	}

	const maxExtraLen = 800 // 프롬프트가 한없이 길어지지 않도록 상한
	extra := strings.Join(parts, " | ")
	if len(extra) > maxExtraLen {
		extra = extra[:maxExtraLen] + "..."
	}

	return Result{Bio: bio, Extra: extra, PageURL: pageURL}, true
}
