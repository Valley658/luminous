package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	pdb "pastellive/internal/db"

	"pastellive/internal/data"
	"pastellive/internal/httputil"
	"pastellive/internal/localai"
	"pastellive/internal/lumidialogue"
	"pastellive/internal/lumiprofiles"
	"pastellive/internal/memberwiki"
	"pastellive/internal/models"
)

// 루미(마스코트) AI 채팅 기능: 사이트 서버에서 직접 돌아가는 로컬 LLM(Ollama)에게
// 물어보고 대답을 받아온다. 질문 텍스트는 로컬호스트의 Ollama 프로세스로만 전달됨.
// 답변 전에 짧게 웹 검색을 한 번 해서(외부로 나가는 건 이 검색 요청 하나뿐) 최신/
// 정확한 정보를 참고하게 한다 - 자세한 내용은 internal/websearch 패키지 참고.

const (
	lumiAICooldown       = 4 * time.Second // 답변이 끝난 직후 연타만 막는 짧은 디바운스 - 동시 다발 CPU 과부하 자체는 localai.Client가 여러 명의 요청을 한 명씩 순서대로 처리해서 이미 막아줌
	lumiAIMaxPromptRunes = 200
	lumiAIRequestTimeout = 80 * time.Second // Ollama가 막 켜져서 모델을 메모리에 올리는 중이면 오래 걸릴 수 있음
	lumiAISearchBudget   = 6 * time.Second  // 전체 시간 중 검색에 쓸 수 있는 최대 시간 (나머지는 LLM 추론용)
)

var (
	lumiAICooldownMu sync.Mutex
	lumiAICooldownAt = map[string]time.Time{}
)

// 사진과 같이 온 질문은, 완전히 같은/거의 같은 사진(지각적 해시로 판단)에
// 똑같은 질문이 다시 오면 LLM을 또 돌리지 않고 예전 답을 그대로 재사용한다.
// member-id-service 쪽 캐시(phash -> 멤버 이름)와는 다른 계층 - 이건 최종
// 답변 문장 전체를 캐시해서 CPU를 제일 많이 잡아먹는 Ollama 추론 자체를
// 건너뛰기 위한 것. 여러 사람이 같은 팬아트를 올리고 "이거 누구야?" 라고
// 물어보는 경우가 흔해서(질문 문구까지 기본값으로 똑같이 맞춰짐) 실효성이 있음.
const lumiImageReplyCacheTTL = 30 * time.Minute
const lumiImageReplyCacheMax = 500

type lumiCachedReply struct {
	reply     string
	expiresAt time.Time
}

var (
	lumiImageReplyCacheMu sync.Mutex
	lumiImageReplyCache   = map[string]lumiCachedReply{}
)

func lumiImageReplyCacheGet(key string) (string, bool) {
	lumiImageReplyCacheMu.Lock()
	defer lumiImageReplyCacheMu.Unlock()
	entry, ok := lumiImageReplyCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.reply, true
}

func lumiImageReplyCacheSet(key, reply string) {
	lumiImageReplyCacheMu.Lock()
	defer lumiImageReplyCacheMu.Unlock()
	if len(lumiImageReplyCache) > lumiImageReplyCacheMax {
		now := time.Now()
		for k, v := range lumiImageReplyCache {
			if now.After(v.expiresAt) {
				delete(lumiImageReplyCache, k)
			}
		}
	}
	lumiImageReplyCache[key] = lumiCachedReply{reply: reply, expiresAt: time.Now().Add(lumiImageReplyCacheTTL)}
}

// 사진 없는 일반 텍스트 질문도, 다른 방문자가 방금 물어본 것과 사실상 같은
// 질문이면(문장부호/공백/자주 붙는 ㅋㅋㅠㅠ 같은 표현 차이만 있는 정도) LLM을
// 또 돌리지 않고 그 답을 그대로 재사용한다 - CPU 추론이 느린 서버에서 같은
// 질문(예: 방금 스케줄 공지가 뜬 직후 "오늘 스케줄 뭐야?"처럼 여러 명이 몰려서
// 물어보는 경우)에 매번 새로 생각하지 않고 바로 답해줄 수 있게. 라이브
// 상태/스케줄처럼 시간이 지나면 바뀌는 정보가 답변에 섞여 있을 수 있어서
// TTL을 이미지 캐시보다 훨씬 짧게(몇 분) 잡는다.
const lumiTextReplyCacheTTL = 2 * time.Minute
const lumiTextReplyCacheMax = 300

var (
	lumiTextReplyCacheMu sync.Mutex
	lumiTextReplyCache   = map[string]lumiCachedReply{}
)

func lumiTextReplyCacheGet(key string) (string, bool) {
	lumiTextReplyCacheMu.Lock()
	defer lumiTextReplyCacheMu.Unlock()
	entry, ok := lumiTextReplyCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.reply, true
}

func lumiTextReplyCacheSet(key, reply string) {
	lumiTextReplyCacheMu.Lock()
	defer lumiTextReplyCacheMu.Unlock()
	if len(lumiTextReplyCache) > lumiTextReplyCacheMax {
		now := time.Now()
		for k, v := range lumiTextReplyCache {
			if now.After(v.expiresAt) {
				delete(lumiTextReplyCache, k)
			}
		}
	}
	lumiTextReplyCache[key] = lumiCachedReply{reply: reply, expiresAt: time.Now().Add(lumiTextReplyCacheTTL)}
}

// normalizeLumiQuestion은 "같은 질문"을 판단하기 위해 문장부호/공백/자주 붙는
// 감탄사(ㅋㅋ, ㅠㅠ, ~, !, ? 등)를 지우고 소문자로 맞춘다. 진짜 의미 기반
// 유사도까지는 아니지만, "오늘 스케줄 알려줘"와 "오늘 스케줄 알려줘!!ㅋㅋ" 같은
// 흔한 변형을 같은 질문으로 묶어주기엔 충분하고, 로컬 CPU만으로 가볍게 계산
// 가능하다는 장점이 있다.
func normalizeLumiQuestion(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var sb strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				sb.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		if strings.ContainsRune("?!.,~♥♡…ㅠㅜㅎㅋ", r) {
			continue
		}
		sb.WriteRune(r)
	}
	return strings.TrimSpace(sb.String())
}

// 로컬 3B 모델은 시스템 프롬프트에 "반말만 써" 규칙을 아무리 강하게 넣어도
// (특히 잘 모르는 걸 인정하거나 사과할 때) 가끔 "~습니다", "~입니다",
// "죄송합니다" 같은 존댓말/격식체로 새는 경우가 있다 - 이건 프롬프트만으로는
// 100% 못 막는 작은 모델의 한계라, 마지막 안전망으로 자주 나오는 존댓말
// 어미 패턴을 코드에서 기계적으로 반말로 바꿔치기한다. 문법적으로 완벽하진
// 않을 수 있지만(모든 한국어 존댓말→반말 변환을 정규식으로 완벽히 처리할 순
// 없음), 실제로 관찰된 위반 패턴(-습니다/-입니다/-합니다/-됩니다/죄송합니다/
// -죠)은 확실히 잡아낸다. 갤러리/사이트제작시기/제작자 같은 확정 대사
// (lumi_dialogue.json에서 온 것)는 이미 반말이 보장돼 있으니 이 함수를 거치지
// 않고, 로컬 LLM이 직접 생성한 답변에만 적용한다.
var (
	lumiFormalJoesonghamnida = regexp.MustCompile(`죄송합니다`)
	lumiFormalSayoDoebnida   = regexp.MustCompile(`것으로 사료됩니다`)
	lumiFormalHapnidaTail    = regexp.MustCompile(`합니다([.!?~,\n]|$)`)
	lumiFormalDoenidaTail    = regexp.MustCompile(`됩니다([.!?~,\n]|$)`)
	lumiFormalIpnidaTail     = regexp.MustCompile(`입니다([.!?~,\n]|$)`)
	lumiFormalSeupnidaTail   = regexp.MustCompile(`습니다([.!?~,\n]|$)`)
	lumiFormalJyoTail        = regexp.MustCompile(`죠([.!?~,\n]|$)`)
)

func sanitizeLumiBanmal(reply string) string {
	s := reply
	s = lumiFormalJoesonghamnida.ReplaceAllString(s, "미안해")
	s = lumiFormalSayoDoebnida.ReplaceAllString(s, "것 같아")
	s = lumiFormalHapnidaTail.ReplaceAllString(s, "해$1")
	s = lumiFormalDoenidaTail.ReplaceAllString(s, "돼$1")
	s = lumiFormalIpnidaTail.ReplaceAllString(s, "이야$1")
	s = lumiFormalSeupnidaTail.ReplaceAllString(s, "어$1")
	s = lumiFormalJyoTail.ReplaceAllString(s, "지$1")
	return s
}

// lumiWeekdayNamesKR은 한국어 요일 이름 - time.Weekday(0=일요일)와 순서를 맞춤.
var lumiWeekdayNamesKR = [...]string{"일요일", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일"}

// buildCurrentDateTimeFact는 실제 현재 날짜/시각(한국 시간 기준)을 매 질문마다
// 새로 계산해서 프롬프트에 박아넣는다. 이게 없으면 모델이 데뷔일/생일/졸업일
// 관련 계산("데뷔한 지 몇 년째야?" 같은 질문)이나 "오늘 며칠이야?" 같은 질문에
// 자기가 학습됐던 시점 기준으로 대충 추측해서 틀린 날짜를 지어내는 문제가
// 있었음 - buildLiveStatusFact와 같은 이유로, 사실을 직접 계산해서 알려준다.
func buildCurrentDateTimeFact() string {
	now := time.Now().In(models.KST)
	weekday := lumiWeekdayNamesKR[int(now.Weekday())]
	return fmt.Sprintf(
		"[지금 이 순간 실제 날짜/시각 - 한국 시간(KST) 기준]\n"+
			"오늘은 %d년 %d월 %d일 %s, 지금 시각은 %02d시 %02d분이야. "+
			"멤버 데뷔일/생일/졸업일처럼 날짜 계산이 필요한 질문(예: \"데뷔한 지 몇 년 됐어?\", "+
			"\"오늘 며칠이야?\")에는 반드시 이 실제 날짜를 기준으로 정확히 계산해서 답해 - "+
			"네가 예전에 학습했던 지식 속 날짜나 추측으로 답하지 마, 이 값이 항상 맞는 최신 기준이야.",
		now.Year(), int(now.Month()), now.Day(), weekday, now.Hour(), now.Minute(),
	)
}

// buildLiveStatusFact는 "지금 누가 방송 중이야?" 같은 질문에 루미가 사이드바의
// 실제 LIVE 표시와 다른 대답(예: 아무도 방송 안 한다고 하거나, 이미 끝난 방송을
// 하는 중이라고 하는 등)을 지어내지 않도록, /api/live_status가 쓰는 것과 같은
// 실시간 CHZZK 라이브 상태(misc.go의 computeLiveStatus/liveStatusCacheKey)를
// 매 질문마다 새로 읽어서 시스템 프롬프트에 사실 그대로 박아넣는다. 이전엔 이
// 정보가 전혀 전달되지 않아서 모델이 그냥 지어내거나 "모른다"고 답했었음.
func (a *App) buildLiveStatusFact() string {
	var status map[string]bool
	if cached, ok := a.Cache.Get(liveStatusCacheKey); ok {
		if m, ok2 := cached.(map[string]bool); ok2 {
			status = m
		}
	}
	if status == nil {
		status = computeLiveStatus(liveStatusHTTPClient)
		a.Cache.Set(liveStatusCacheKey, status, liveStatusCacheTTL)
	}

	var live []string
	for _, m := range data.SIDEBAR_MEMBERS {
		if m.ChzzkID == "" {
			continue // 방송 채널이 없는 사이드바 항목(로고 등)
		}
		if status[m.Name] {
			full := data.MEMBER_FULL_NAMES[m.Name]
			if full == "" {
				full = m.Name
			}
			live = append(live, full)
		}
	}

	if len(live) == 0 {
		return "[지금 이 순간 실제 라이브 상태]\n지금 라이브 중인 멤버는 아무도 없음(전부 오프라인)."
	}
	return "[지금 이 순간 실제 라이브 상태]\n지금 라이브(방송) 중: " + strings.Join(live, ", ") + ". 나머지 멤버는 지금 오프라인."
}

// buildScheduleFact는 사이트의 방송 스케줄표(멤버들이 직접 등록한 오늘 일정)를
// 그대로 읽어서 프롬프트에 넣어준다. "오늘 누구 방송해?" 같은 질문에도 실제
// 등록된 일정 기준으로 답하게 하기 위함. DB에서 못 읽으면(스케줄표가 비어
// 있거나 오류) 그냥 이 블록 없이 진행 - 필수 정보는 아니라서 실패해도
// 전체 답변 자체가 막히면 안 됨.
func (a *App) buildScheduleFact() string {
	if a.DB == nil {
		return ""
	}
	today := time.Now().In(models.KST).Format("2006-01-02")
	rows, err := models.GetSchedules(a.DB, today, today)
	if err != nil || len(rows) == 0 {
		return "[오늘(" + today + ") 등록된 방송 스케줄]\n등록된 일정 없음(그렇다고 아무도 방송 안 한다는 뜻은 아니고, 그냥 스케줄표에 안 적혀 있을 뿐일 수 있음 - 확실친 않으면 실제 라이브 상태로 판단해)."
	}
	var lines []string
	for _, row := range rows {
		name, _ := row["member_name"].(string)
		title, _ := row["title"].(string)
		isDayOff, _ := row["is_day_off"].(bool)
		var eventTime string
		if v, ok := row["event_time"].(string); ok {
			eventTime = v
		}
		switch {
		case isDayOff:
			lines = append(lines, name+": 휴방")
		case eventTime != "":
			lines = append(lines, name+" "+eventTime+" - "+title)
		default:
			lines = append(lines, name+" - "+title)
		}
	}
	return "[오늘(" + today + ") 등록된 방송 스케줄]\n" + strings.Join(lines, "\n")
}

// buildImageIdentifyFact는 방문자가 올린 사진을 member-id-service(CLIP 이미지
// 비교 서비스, services/member-id-service/ 참고)로 보내서 어떤 멤버와 제일 닮았는지
// 확인하고, 그 결과를 사실로 프롬프트에 실어준다. 모델 자신이 사진을 직접
// "보고" 맞히는 게 아니라(텍스트 전용 모델이라 애초에 못 봄), 이미 식별된
// 이름을 텍스트로 알려주는 방식.
// phash(지각적 해시)도 같이 돌려준다 - 완전히 같은/거의 같은 사진으로 같은
// 질문이 또 들어오면 LLM을 다시 돌리지 않고 이전 답을 재사용하기 위한 캐시 키로
// 씀(ApiLumiAskHandler 참고). 식별 자체가 실패했거나 서비스가 꺼져 있으면
// phash는 빈 문자열("")로 오고, 이 경우 캐시는 그냥 안 쓰인다.
func (a *App) buildImageIdentifyFact(ctx context.Context, imageBytes []byte) (fact string, phash string) {
	if a.MemberID == nil || !a.MemberID.Enabled() {
		return "[사진 인식 결과]\n지금은 사진 인식 기능이 꺼져 있어서 사진 속 인물을 확인할 수 없어. 사진을 못 봤다고 솔직히 말하고, 텍스트로 물어본 부분에는 최대한 답해.", ""
	}
	name, score, ph, err := a.MemberID.Identify(ctx, imageBytes)
	if err != nil {
		return "[사진 인식 결과]\n사진을 분석하는 데 실패했어(형식이 안 맞거나 서비스에 일시적 문제가 있을 수 있음). 사진 인식은 안 됐다고 솔직히 말해.", ""
	}
	if name == "" {
		return fmt.Sprintf(
			"[사진 인식 결과]\n등록된 스텔라이브 멤버 중 확실히 닮은 사람을 못 찾음(유사도 %.2f, 기준 미달).\n"+
				"**중요: 반드시 이 사진에 대한 답변만 해.** 아무 멤버나 추측해서 이름을 지어내지 말고, "+
				"위에 있는 [스텔라이브 멤버 목록]이나 [멤버별 상세 정보]에서 다른 멤버를 골라 소개하거나 "+
				"나열하지도 마 - 그건 이 사진과 아무 상관 없는 정보라 답변에 넣으면 안 돼. "+
				"그냥 \"음... 이 사진은 누구인지 잘 모르겠어\" 정도로 짧고 솔직하게만 답해.", score), ph
	}
	return fmt.Sprintf(
		"[사진 인식 결과]\n사진 속 인물은 '%s'로 인식됨(유사도 %.2f). 이 결과를 사실로 받아들이고, "+
			"위 [멤버별 상세 정보]나 [스텔라이브 멤버 목록]에 '%s' 관련 내용이 있으면 그 내용을 답변 문장 안에 "+
			"직접 풀어서 소개해(기수, 정식 이름, 소개/취향 등). \"위에 나와있는 정보를 참고해봐\" 같은 식으로 "+
			"안내만 하고 넘어가지 마 - 방문자는 위쪽 자료를 볼 수 없으니, 네가 직접 요약해서 말해주는 게 답이야.",
		name, score, name), ph
}

// buildMemberProfileFact는 리도님이 루미데이터_편집.bat(로컬 전용 페이지)으로
// 직접 적어둔 멤버별 상세 정보(소개/취향/기타 사실)를 그대로 프롬프트에
// 실어준다. 아직 아무것도 안 적었으면(파일이 없거나 비어있으면) 빈 문자열을
// 돌려주고, 이 경우 시스템 프롬프트 쪽에서 "모르면 지어내지 말라"는 규칙이
// 이미 있으니 별문제 없음.
func (a *App) buildMemberProfileFact() string {
	profiles, err := lumiprofiles.Load(lumiprofiles.DefaultPath())
	if err != nil || len(profiles) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[멤버별 상세 정보 - 리도님이 직접 정리해둔 자료, 신뢰도 높음]\n")
	for _, name := range lumiprofiles.SortedNames(profiles) {
		p := profiles[name]
		if p.IsEmpty() {
			continue
		}
		fmt.Fprintf(&sb, "- %s: ", name)
		var parts []string
		if p.Bio != "" {
			parts = append(parts, "소개: "+p.Bio)
		}
		if p.Likes != "" {
			parts = append(parts, "좋아하는 것: "+p.Likes)
		}
		if p.Dislikes != "" {
			parts = append(parts, "싫어하는 것: "+p.Dislikes)
		}
		if p.Extra != "" {
			parts = append(parts, "기타: "+p.Extra)
		}
		sb.WriteString(strings.Join(parts, " / "))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// lumiWikiAttempted는 나무위키 자동 조회가 실패했던(또는 최근에 이미 시도했던)
// 멤버 이름을 잠깐 기억해서, 같은 이름이 계속 물어봐져도 매번 나무위키에 다시
// 요청을 보내지 않게 막는다 - 예의상으로도, 불필요한 응답 지연을 막기 위해서도
// 필요함. 성공한 조회는 member_profiles.json에 바로 저장되니 이 캐시에 넣을
// 필요가 없고(다음 buildMemberProfileFact가 파일에서 바로 읽어옴), 실패한
// 경우만 기억한다.
var (
	lumiWikiAttemptedMu sync.Mutex
	lumiWikiAttempted   = map[string]time.Time{}
)

const lumiWikiRetryCooldown = 30 * time.Minute

// autoFetchMissingMemberProfile은 질문에 등장한 멤버 이름 중 아직 로컬에
// 상세 정보가 없는 게 있으면 나무위키에서 자동으로 가져와 member_profiles.json에
// 저장한다(memberwiki 패키지 참고 - 셀레니움 없이 가벼운 HTTP+정규식 방식).
// 리도님이 이미 직접 적어둔 값은 절대 덮어쓰지 않고, "아무것도 없을 때만"
// 채워 넣는다. 이 함수 자체는 아무것도 리턴하지 않는다 - 저장에 성공하면
// 바로 다음에 실행되는 buildMemberProfileFact()가 파일에서 새로 읽어서
// 자연스럽게 이번 요청의 답변에도 반영되기 때문(캐싱 없이 매번 새로 읽으니
// 별도로 값을 넘겨줄 필요가 없음).
func (a *App) autoFetchMissingMemberProfile(ctx context.Context, prompt string) {
	profiles, err := lumiprofiles.Load(lumiprofiles.DefaultPath())
	if err != nil {
		profiles = map[string]lumiprofiles.Profile{}
	}
	for _, m := range data.SIDEBAR_MEMBERS {
		if m.Name == "스텔라이브" || !strings.Contains(prompt, m.Name) {
			continue
		}
		if existing, ok := profiles[m.Name]; ok && !existing.IsEmpty() {
			continue // 이미 정보 있음(리도님이 적었거나 예전에 자동으로 채워짐)
		}

		lumiWikiAttemptedMu.Lock()
		lastTry, tried := lumiWikiAttempted[m.Name]
		lumiWikiAttemptedMu.Unlock()
		if tried && time.Since(lastTry) < lumiWikiRetryCooldown {
			continue // 최근에 이미 시도했다가 실패했음 - 당분간 재시도 안 함
		}

		result, ok := memberwiki.FetchFromNamuwiki(ctx, m.Name)
		lumiWikiAttemptedMu.Lock()
		lumiWikiAttempted[m.Name] = time.Now()
		lumiWikiAttemptedMu.Unlock()
		if !ok {
			continue
		}

		profiles[m.Name] = lumiprofiles.Profile{Bio: result.Bio, Extra: result.Extra}
		if err := lumiprofiles.Save(lumiprofiles.DefaultPath(), profiles); err != nil {
			log.Printf("lumi: 나무위키에서 가져온 %s 프로필 저장 실패: %v", m.Name, err)
		}
		return // 한 요청당 한 명만 - 여러 명을 동시에 새로 긁어오면 응답이 느려짐
	}
}

// buildGroundedPrompt는 질문 앞에 웹 검색 결과를 붙여서, 모델 자체 지식만으론
// 부족하거나 틀리기 쉬운 사실(연도, 최신 소식 등)을 검색 결과로 보완해준다.
// 검색이 실패하거나 결과가 없으면 그냥 원래 질문만 그대로 보낸다.
func (a *App) buildGroundedPrompt(ctx context.Context, question string) string {
	if a.WebSearch == nil || !a.WebSearch.Enabled() {
		return question
	}
	searchCtx, cancel := context.WithTimeout(ctx, lumiAISearchBudget)
	defer cancel()

	results := a.WebSearch.Search(searchCtx, "스텔라이브 StelLive "+question, 3)
	if len(results) == 0 {
		return question
	}
	var sb strings.Builder
	sb.WriteString("[참고 - 방금 검색한 결과]\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s: %s\n", i+1, r.Title, r.Snippet)
	}
	sb.WriteString("\n검색 결과가 질문과 관련 있으면 참고해서 답하고, 관련 없으면 무시해.\n\n질문: ")
	sb.WriteString(question)
	return sb.String()
}

// lumiLanguageRequestHints: "일본어로 ~ 말해봐" 같은 요청은 시스템 프롬프트
// (14)번 규칙만으로는 자꾸 "일본어로는 이렇게 말해요"처럼 한국어로 설명만
// 하고 끝나버리는 문제가 있었다 - 작은 로컬 모델은 질문에서 멀리 떨어진(맨
// 위쪽) 규칙보다 질문 바로 옆에 붙은 지시를 훨씬 잘 따르는 편이라, 질문에
// "일본어로"/"영어로" 같은 표현이 실제로 나오면 질문 바로 뒤에 그 언어로
// 진짜 답하라는 문구를 직접 붙여준다(사이트 무관 주제 요청이면 그래도 (2)번
// 거절 규칙이 우선하도록 문구 안에 같이 명시함).
var lumiLanguageRequestHints = []struct {
	keyword string
	desc    string
}{
	{"일본어로", "실제 일본어 문장(히라가나·가타카나·한자)으로"},
	{"영어로", "실제 영어 문장으로"},
	{"중국어로", "실제 중국어(한자) 문장으로"},
	{"프랑스어로", "실제 프랑스어 문장으로"},
	{"독일어로", "실제 독일어 문장으로"},
	{"스페인어로", "실제 스페인어 문장으로"},
}

func lumiLanguageReminder(prompt string) string {
	for _, h := range lumiLanguageRequestHints {
		if strings.Contains(prompt, h.keyword) {
			return "\n\n[중요: 이 질문엔 " + h.desc + " 직접 답해야 해 - 그 언어에 대해 " +
				"한국어로 설명만 하고 끝내지 마, 진짜 그 언어 문장을 써. 단, 질문 내용 자체가 " +
				"스텔라이브와 무관하면 이 지시보다 위 (2)번 규칙이 우선이니 거절 문구로 답해.]"
		}
	}
	return ""
}

// isLumiGalleryShowRequest는 "팬갤러리 보여줘", "랜덤으로 갤러리 보여줘" 같은
// 요청을 감지한다. LLM한테 판단시키지 않고 간단한 키워드 매칭만 쓰는 이유:
// 이건 "그런 척 대답하기"가 아니라 실제로 DB에서 사진을 가져와야 하는
// 명확한 동작이라, 애매하게 추론시키는 것보다 확실한 키워드로 잡는 게 낫다.
func isLumiGalleryShowRequest(prompt string) bool {
	return strings.Contains(prompt, "갤러리") && strings.Contains(prompt, "보여")
}

// lumiGalleryShowReply는 승인된 팬아트 중 하나를 무작위로 뽑아서, 루미의
// 대화창에 실제 사진과 함께 보여줄 응답을 만든다. LLM을 거치지 않으므로
// "[이미지 갤러리에서 클릭해서 확인해보세요]" 처럼 사진 없이 안내문만
// 지어내는 문제가 아예 생길 수 없다. 대사 후보(lines)는 lumi_dialogue.json에서
// 불러온 값을 넘겨받는다.
func lumiGalleryShowReply(db *pdb.DB, lines []string) map[string]any {
	if db == nil {
		return map[string]any{"success": true, "reply": "지금은 갤러리에 접근할 수가 없네... 잠시 후 다시 물어봐줘!"}
	}
	imageURL, title, nickname, found, err := models.GetRandomActiveFanart(db)
	if err != nil || !found {
		return map[string]any{"success": true, "reply": "어라... 팬갤러리에 아직 보여줄 사진이 없나봐! 먼저 팬아트를 올려주면 다음에 보여줄 수 있어."}
	}
	reply := lines[rand.Intn(len(lines))]
	if nickname.Valid && nickname.String != "" {
		reply += " (" + nickname.String + "님이 올려주신 팬아트야"
		if title.Valid && title.String != "" {
			reply += " - " + title.String
		}
		reply += ")"
	}
	return map[string]any{"success": true, "reply": reply, "image_url": imageURL}
}

// isLumiSiteOriginQuestion은 "사이트 제작연도는?", "루미너스 언제 생겼어?" 같은
// 사이트 개설 시기 질문을 감지한다. 이런 질문에 로컬 LLM(작은 3B 모델)이
// 자꾸 "죄송합니다, 제공된 정보에는 포함되어 있지 않습니다..." 같은 딱딱한
// 챗봇 말투로 새어버리는 문제가 있어서(시스템 프롬프트 규칙만으로는 완전히
// 못 막음), 아예 LLM한테 안 보내고 정해진 장난스러운 반말 대사로 바로
// 넘겨버린다 - 팬갤러리 요청과 같은 이유.
func isLumiSiteOriginQuestion(prompt string) bool {
	if strings.Contains(prompt, "제작연도") || strings.Contains(prompt, "제작 연도") {
		return true
	}
	hasSiteWord := strings.Contains(prompt, "루미너스") || strings.Contains(prompt, "사이트") || strings.Contains(prompt, "여기")
	if !hasSiteWord {
		return false
	}
	hasOriginWord := strings.Contains(prompt, "생겼") || strings.Contains(prompt, "만들어") ||
		strings.Contains(prompt, "만든") || strings.Contains(prompt, "제작") ||
		strings.Contains(prompt, "생성") || strings.Contains(prompt, "오픈") ||
		strings.Contains(prompt, "개설") || strings.Contains(prompt, "창설") ||
		strings.Contains(prompt, "역사") || strings.Contains(prompt, "연혁")
	return hasOriginWord && (strings.Contains(prompt, "언제") || strings.Contains(prompt, "연도") || strings.Contains(prompt, "날짜"))
}

// lumiSiteOriginDeflectReply: 여고생 느낌의 장난스러운 반말 대사로만 답한다 -
// 존댓말/격식체는 절대 섞이면 안 됨. 대사 후보(lines)는 lumi_dialogue.json에서
// 불러온 값을 넘겨받는다.
func lumiSiteOriginDeflectReply(lines []string) map[string]any {
	return map[string]any{"success": true, "reply": lines[rand.Intn(len(lines))]}
}

// isLumiCreatorQuestion은 "사이트 제작자는 누구야?", "이거 누가 만들었어?" 같은
// 질문을 감지한다. data.LumiAdminFact가 "운영진이 누구야?" 류 표현은 이미
// 커버하지만, "제작자"/"만든 사람" 같은 동의어로 물어보면 작은 로컬 모델이
// 같은 질문으로 못 묶고 또 로봇 말투로 새는 경우가 있어서, 이 표현들도 아예
// LLM 없이 정해진 대사로 바로 답해버린다.
func isLumiCreatorQuestion(prompt string) bool {
	hasCreatorWord := strings.Contains(prompt, "제작자") || strings.Contains(prompt, "만든 사람") ||
		strings.Contains(prompt, "만든사람") || strings.Contains(prompt, "만든이") ||
		strings.Contains(prompt, "개발자") || strings.Contains(prompt, "운영자") ||
		strings.Contains(prompt, "운영진") || strings.Contains(prompt, "누가 만들") ||
		strings.Contains(prompt, "누가만들")
	if !hasCreatorWord {
		return false
	}
	return strings.Contains(prompt, "누구") || strings.Contains(prompt, "누가") || strings.Contains(prompt, "?") ||
		strings.Contains(prompt, "만들었")
}

// lumiCreatorDeflectReply: 사이트 운영진 개인 신상(학교, 학년, 재학 여부 등
// 특정 가능한 정보)은 절대 공개하지 않는다 - 방문자 아무나 물어볼 수 있는
// 공개 챗봇이라, 실제 신상이 특정될 수 있는 정보를 넣으면 안전 문제가 된다.
// 오타쿠 한 명이 애정으로 운영한다는 톤은 유지하되, 신상 정보 없이 캐릭터
// 있게만 답한다. 대사 후보(lines)는 lumi_dialogue.json에서 불러온 값을
// 넘겨받는다.
func lumiCreatorDeflectReply(lines []string) map[string]any {
	return map[string]any{"success": true, "reply": lines[rand.Intn(len(lines))]}
}

// ApiLumiAskHandler는 방문자가 루미에게 보낸 질문을 로컬 LLM에 넘기고 답을 받아온다.
func (a *App) ApiLumiAskHandler(w http.ResponseWriter, r *http.Request) {
	if a.LocalAI == nil {
		httputil.JSONError(w, http.StatusServiceUnavailable, "루미의 AI 기능이 아직 설정되지 않았어.")
		return
	}

	var prompt string
	var imageBytes []byte

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		// 사진이 첨부된 질문 - 프론트엔드(lumiAskAI)가 사진을 고르면 JSON
		// 대신 이 방식으로 보낸다. 사진 자체는 이 프로세스 메모리에서만
		// 잠깐 들고 있다가 memberid 서비스로 전달하고 버림(디스크에 저장
		// 안 함).
		if err := r.ParseMultipartForm(8 << 20); err != nil { // 폼 전체 8MB 상한
			httputil.JSONError(w, http.StatusBadRequest, "사진이 너무 크거나 요청 형식이 이상해(8MB 이하로 올려줘).")
			return
		}
		prompt = sanitizeUserHTML(strings.TrimSpace(r.FormValue("prompt")))
		if file, _, err := r.FormFile("image"); err == nil {
			defer file.Close()
			imageBytes, _ = io.ReadAll(io.LimitReader(file, 8<<20))
		}
		if prompt == "" && len(imageBytes) > 0 {
			prompt = "이 사진 속에 누가 있는지 알려주고, 그 멤버에 대해 짧게 소개해줘."
		}
	} else {
		var body struct {
			Prompt   string `json:"prompt"`
			ImageURL string `json:"image_url"`
		}
		_ = decodeJSONBody(r, &body)
		prompt = sanitizeUserHTML(strings.TrimSpace(body.Prompt))

		// 사진 파일 대신 "이미지 링크"를 붙여넣은 경우 - 프론트엔드가 텍스트
		// 안에서 이미지 URL을 미리 감지해서 이 필드로 따로 보내준다. 서버가
		// 대신 다운로드해와야 하는 이유: 브라우저에서 <img>로 미리보기는 되지만
		// 캔버스로 픽셀을 뽑아내는 건 CORS 헤더 없는 이미지 CDN(구글 gstatic
		// 등)에서는 막혀있어서 프론트에서 바이트를 얻을 방법이 없음.
		if imageURL := strings.TrimSpace(body.ImageURL); imageURL != "" {
			fetched, _, fetchErr := httputil.FetchRemoteImage(r.Context(), imageURL, 8<<20)
			if fetchErr != nil {
				httputil.JSONError(w, http.StatusBadRequest, "그 이미지 링크를 불러올 수가 없어... 다른 링크로 다시 시도하거나 사진을 직접 올려줘!")
				return
			}
			imageBytes = fetched
			if prompt == "" {
				prompt = "이 사진 속에 누가 있는지 알려주고, 그 멤버에 대해 짧게 소개해줘."
			}
		}
	}

	if prompt == "" {
		httputil.JSONError(w, http.StatusBadRequest, "질문을 입력해줘!")
		return
	}
	if runes := []rune(prompt); len(runes) > lumiAIMaxPromptRunes {
		prompt = string(runes[:lumiAIMaxPromptRunes])
	}

	// 루미의 성격/말투/시스템 프롬프트 규칙/고정 대사는 go-server/lumi_data/
	// lumi_dialogue.json 파일에서 매 요청마다 새로 읽어온다 - 캐싱 없이 매번
	// 다시 읽으므로, 리도님이 파일만 고쳐도 서버 재시작 없이 바로 반영됨
	// (member_profiles.json을 읽는 lumiprofiles 패키지와 같은 방식).
	dialogue := lumidialogue.Load(lumidialogue.DefaultPath())

	// "팬갤러리 보여줘/랜덤으로 보여줘" 같은 요청은 LLM한테 시키지 않는다 -
	// 예전엔 이런 질문에 LLM이 실제 사진 없이 "[이미지 갤러리에서 클릭해서
	// 확인해보세요]" 같은 가짜 안내문만 지어내고 끝났음. 대신 DB에서 진짜
	// 승인된 팬아트를 하나 무작위로 뽑아서 사진 자체를 대화에 바로 보여준다.
	// 사진 첨부 요청과는 무관하니 imageBytes 유무와 상관없이 먼저 확인하고,
	// Ollama를 아예 안 쓰니 쿨다운/캐시 로직보다 앞에서 처리해도 무방하다.
	if len(imageBytes) == 0 && isLumiGalleryShowRequest(prompt) {
		writeJSON(w, lumiGalleryShowReply(a.DB, dialogue.GalleryShowLines))
		return
	}

	// 사이트 제작 시기 질문도 같은 이유로 LLM을 거치지 않고 바로 답한다 -
	// 아래 isLumiSiteOriginQuestion 주석 참고.
	if len(imageBytes) == 0 && isLumiSiteOriginQuestion(prompt) {
		writeJSON(w, lumiSiteOriginDeflectReply(dialogue.SiteOriginDeflectLines))
		return
	}

	// "사이트 제작자/만든 사람이 누구야?" 류 질문도 LLM 없이 바로 답한다 -
	// 아래 isLumiCreatorQuestion 주석 참고. 신상 정보(학교/학년 등)는 절대
	// 넣지 않고 기존 [사이트 운영진에 대해] 톤 그대로만 답함.
	if len(imageBytes) == 0 && isLumiCreatorQuestion(prompt) {
		writeJSON(w, lumiCreatorDeflectReply(dialogue.CreatorDeflectLines))
		return
	}

	ip := httputil.GetClientIP(r)
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()

	lumiAICooldownMu.Lock()
	if until, ok := lumiAICooldownAt[ip]; ok && now.Before(until) {
		lumiAICooldownMu.Unlock()
		httputil.JSONError(w, http.StatusTooManyRequests, "루미가 방금 막 대답했어! 잠깐만 기다렸다가 다시 물어봐줘.")
		return
	}
	lumiAICooldownMu.Unlock()
	// 쿨다운은 "요청이 들어온 시각"이 아니라 "실제로 답을 끝낸 시각" 기준으로
	// 걸어야 한다 - 예전에는 요청이 들어오자마자 바로 20초를 걸어버려서, 답이
	// (이미지 캐시 히트처럼) 금방 끝나도 처음 질문 시각부터 20초가 지나기
	// 전까진 다음 질문이 막혔음 - 첫 인사 답변 밑에 있는 빠른 답장 버튼을
	// 바로 눌러도 막히는 게 그래서였음. 이 함수가 끝나는 시점(성공/실패/
	// 캐시 히트 전부 포함)에 defer로 걸어야 실제 완료 시각 기준이 됨.
	defer func() {
		until := time.Now().Add(lumiAICooldown)
		lumiAICooldownMu.Lock()
		lumiAICooldownAt[ip] = until
		if len(lumiAICooldownAt) > 5000 { // 메모리에 무한정 쌓이지 않도록 가끔 정리
			cleanupNow := time.Now()
			for k, u := range lumiAICooldownAt {
				if cleanupNow.After(u) {
					delete(lumiAICooldownAt, k)
				}
			}
		}
		lumiAICooldownMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(r.Context(), lumiAIRequestTimeout)
	defer cancel()

	// 사진이 첨부된 질문은 identify를 딱 한 번만 호출해서(네트워크 왕복 +
	// CLIP 추론이라 두 번 부르면 낭비) fact 텍스트와 phash를 같이 받아둔다.
	// 완전히 같은/거의 같은 사진(지각적 해시 기준)에 정확히 같은 질문이 다시
	// 오면 - 팬아트 하나에 여러 방문자가 기본 질문("이 사진 속에 누가
	// 있는지...")으로 물어보는 경우가 흔함 - Ollama를 또 돌리지 않고 예전
	// 답을 그대로 돌려준다. CPU 추론이 제일 무거운 부분이라 이게 실효성이 큼.
	var imageFact, imageCacheKey string
	if len(imageBytes) > 0 {
		var imagePhash string
		imageFact, imagePhash = a.buildImageIdentifyFact(ctx, imageBytes)
		if imagePhash != "" {
			imageCacheKey = imagePhash + "|" + strings.ToLower(strings.TrimSpace(prompt))
			if cached, ok := lumiImageReplyCacheGet(imageCacheKey); ok {
				writeJSON(w, map[string]any{"success": true, "reply": cached})
				return
			}
		}
	}

	// 사진 없는 순수 텍스트 질문이면, 다른 방문자가 방금 물어본 것과 사실상
	// 같은 질문인지 확인해서 있으면 바로 그 답을 돌려준다(위 이미지 캐시와
	// 같은 목적, 텍스트 질문용).
	var textCacheKey string
	if len(imageBytes) == 0 {
		if normalized := normalizeLumiQuestion(prompt); normalized != "" {
			textCacheKey = normalized
			if cached, ok := lumiTextReplyCacheGet(textCacheKey); ok {
				writeJSON(w, map[string]any{"success": true, "reply": cached})
				return
			}
		}
	}

	grounded := a.buildGroundedPrompt(ctx, prompt) + lumiLanguageReminder(prompt)

	// 질문에 등장한 멤버 중 아직 로컬에 상세 정보가 없는 사람이 있으면 나무위키에서
	// 자동으로 가져와 저장해둔다 - 성공하면 바로 아래 buildMemberProfileFact()가
	// 새로 저장된 값을 그대로 읽어서 이번 답변에도 반영됨(사진 첨부 요청은
	// buildImageIdentifyFact가 따로 처리하니 텍스트 질문일 때만).
	if len(imageBytes) == 0 {
		a.autoFetchMissingMemberProfile(ctx, prompt)
	}

	systemPrompt := lumidialogue.BuildSystemPrompt(dialogue, data.LumiRosterFacts) + "\n\n" + buildCurrentDateTimeFact() + "\n\n" + a.buildLiveStatusFact() + "\n\n" + a.buildScheduleFact()
	if profileFact := a.buildMemberProfileFact(); profileFact != "" {
		systemPrompt += "\n\n" + profileFact
	}
	if imageFact != "" {
		systemPrompt += "\n\n" + imageFact
	}

	reply, err := a.LocalAI.Ask(ctx, systemPrompt, grounded)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			// 순서를 기다리는 동안 방문자가 페이지를 닫거나 요청을 취소한 경우 -
			// 답할 대상이 없으니 조용히 끝낸다(에러 응답 자체가 의미 없음).
			return
		case localai.IsTimeout(err):
			httputil.JSONError(w, http.StatusServiceUnavailable, "생각하는 데 시간이 너무 오래 걸려서 멈췄어... 로컬 AI가 방금 켜졌다면 처음 한 번은 원래 느려. 잠시 후 다시 물어봐줘!")
		case localai.IsUnreachable(err):
			httputil.JSONError(w, http.StatusServiceUnavailable, "로컬 AI(Ollama)가 꺼져 있는 것 같아. 서버 관리자에게 확인해달라고 해줘.")
		default:
			httputil.JSONError(w, http.StatusServiceUnavailable, "지금은 대답하기 어려워... 잠시 후 다시 시도해줘.")
		}
		return
	}

	// 로컬 LLM이 직접 생성한 답변에만 존댓말→반말 안전망을 적용한다(캐시에도
	// 이미 변환된 버전을 저장해서, 캐시로 재사용될 때도 항상 반말로 나감).
	reply = sanitizeLumiBanmal(reply)

	if imageCacheKey != "" {
		lumiImageReplyCacheSet(imageCacheKey, reply)
	}
	if textCacheKey != "" {
		lumiTextReplyCacheSet(textCacheKey, reply)
	}

	writeJSON(w, map[string]any{"success": true, "reply": reply})
}
