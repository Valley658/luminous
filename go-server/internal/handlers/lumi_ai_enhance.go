package handlers

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"pastellive/internal/data"
	"pastellive/internal/httputil"
	"pastellive/internal/models"
	"pastellive/internal/video"
)

// =====================================================================
// 기억력: 최근 대화 맥락 + 관심 멤버
//
// 지금까지는 매 질문을 systemPrompt + 이번 질문만으로 독립적으로 물어봤다
// (LocalAI.Ask 호출부 참고) - 즉 "그 멤버는 몇 살이야?" 같은 이어지는
// 질문을 해도 루미는 방금 전 대화를 전혀 몰랐다. 로그인 유저는 이미
// lumi_chat_history에 대화가 저장되고 있으니, 그중 최근 몇 개를 매 요청마다
// 시스템 프롬프트에 다시 넣어줘서 "기억하는 것처럼" 이어지는 대화가
// 가능하게 한다.
// =====================================================================

const lumiMemoryFieldMaxRunes = 120

// buildRecentConversationFact: 로그인 유저의 최근 대화 몇 턴을 시간순으로
// 붙여서, "그럼 그 멤버는?"류 이어지는 질문에서 뭘 가리키는지 참고할 수
// 있게 한다. 대화가 없거나 DB 조회에 실패하면 빈 문자열(없어도 기존 동작과
// 동일하게 안전).
func (a *App) buildRecentConversationFact(userID int64) string {
	if userID == 0 || a.LumiDB == nil {
		return ""
	}
	msgs, err := models.ListLumiChatHistory(a.LumiDB, userID, 3)
	if err != nil || len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[최근 이 유저와 나눈 대화 - 방금 물어본 질문이 \"그건?\", \"걔는?\"처럼 " +
		"이전 대화를 이어받는 질문이면 아래를 참고해서 뭘 가리키는지 파악해. 이미 다 끝난 " +
		"화제면 억지로 다시 안 꺼내도 됨]\n")
	// ListLumiChatHistory는 최신순(DESC)으로 오므로 시간순으로 뒤집어서 보여준다.
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		fmt.Fprintf(&sb, "Q: %s\nA: %s\n", truncateRunes(m.Question, lumiMemoryFieldMaxRunes), truncateRunes(m.Reply, lumiMemoryFieldMaxRunes))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// buildUserInterestFact: 최근 대화(최대 20개)에서 멤버 이름이 3번 이상
// 언급됐으면 "요즘 이 유저가 관심 있어 하는 멤버"로 참고 정보를 준다.
// 확실한 사실이 아니라 약한 신호라, 프롬프트 자체에도 "지어내지 마라"는
// 경고를 같이 넣는다.
func (a *App) buildUserInterestFact(userID int64) string {
	if userID == 0 || a.LumiDB == nil {
		return ""
	}
	msgs, err := models.ListLumiChatHistory(a.LumiDB, userID, 20)
	if err != nil || len(msgs) < 3 {
		return ""
	}
	counts := map[string]int{}
	for _, m := range msgs {
		combined := m.Question + " " + m.Reply
		for _, mem := range data.SIDEBAR_MEMBERS {
			if mem.Name == "스텔라이브" {
				continue
			}
			if strings.Contains(combined, mem.Name) {
				counts[mem.Name]++
			}
		}
	}
	var topName string
	var topCount int
	for name, c := range counts {
		if c > topCount {
			topName, topCount = name, c
		}
	}
	if topCount < 3 {
		return ""
	}
	return "[참고 - 이 유저의 최근 관심사, 약한 신호일 뿐이니 확실하지 않으면 굳이 " +
		"언급하지 마]\n최근 대화에서 '" + topName + "' 얘기가 유독 자주 나왔음."
}

// =====================================================================
// 참여형 기능: 루미가 직접 퀴즈를 내주기
//
// data.QuizQuestionsByDifficulty(퀴즈 페이지가 이미 쓰는 것과 같은 문제
// 은행)에서 무작위로 하나 골라 질문만 먼저 보여주고, "정답 알려줘" 같은
// 후속 요청이 오면 정답을 공개한다. LLM에게 문제를 만들라고 시키지 않고
// 미리 검증된 문제 은행에서만 고르므로, 틀린 트리비아를 지어낼 위험이 없다.
// =====================================================================

type lumiPendingQuizEntry struct {
	question  data.QuizQuestion
	expiresAt time.Time
}

const lumiPendingQuizTTL = 5 * time.Minute

var (
	lumiPendingQuizMu sync.Mutex
	lumiPendingQuiz   = map[string]lumiPendingQuizEntry{}
)

// lumiQuizIdentity: 퀴즈 진행 상태를 기억할 키. 로그인 유저는 계정 기준,
// 비로그인 방문자는 IP 기준(다른 안전장치들과 같은 방식).
func lumiQuizIdentity(userID int64, r *http.Request) string {
	if userID != 0 {
		return fmt.Sprintf("u:%d", userID)
	}
	return "ip:" + httputil.GetClientIP(r)
}

func isLumiQuizRequest(prompt string) bool {
	if !strings.Contains(prompt, "퀴즈") {
		return false
	}
	return strings.Contains(prompt, "내") || strings.Contains(prompt, "줘") ||
		strings.Contains(prompt, "문제") || strings.Contains(prompt, "풀")
}

func isLumiQuizAnswerRequest(identity, prompt string) bool {
	lumiPendingQuizMu.Lock()
	entry, ok := lumiPendingQuiz[identity]
	lumiPendingQuizMu.Unlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return false
	}
	hasAnswerWord := strings.Contains(prompt, "정답") || strings.Contains(prompt, "답") ||
		strings.Contains(prompt, "모르겠") || strings.Contains(prompt, "몰라") ||
		strings.Contains(prompt, "포기")
	return hasAnswerWord
}

var lumiQuizDifficultyOrder = []string{"easy", "medium", "hard"}

func lumiPickQuizDifficulty(prompt string) string {
	switch {
	case strings.Contains(prompt, "어려운") || strings.Contains(prompt, "하드"):
		return "hard"
	case strings.Contains(prompt, "중간") || strings.Contains(prompt, "보통"):
		return "medium"
	case strings.Contains(prompt, "쉬운") || strings.Contains(prompt, "이지"):
		return "easy"
	default:
		return lumiQuizDifficultyOrder[rand.Intn(len(lumiQuizDifficultyOrder))]
	}
}

func lumiQuizReply(identity, prompt string) map[string]any {
	difficulty := lumiPickQuizDifficulty(prompt)
	pool := data.QuizQuestionsByDifficulty[difficulty]
	if len(pool) == 0 {
		return map[string]any{"success": true, "reply": "어라, 지금은 낼 수 있는 퀴즈가 없나봐! 잠시 후에 다시 물어봐줘."}
	}
	q := pool[rand.Intn(len(pool))]
	lumiPendingQuizMu.Lock()
	lumiPendingQuiz[identity] = lumiPendingQuizEntry{question: q, expiresAt: time.Now().Add(lumiPendingQuizTTL)}
	if len(lumiPendingQuiz) > 5000 {
		now := time.Now()
		for k, v := range lumiPendingQuiz {
			if now.After(v.expiresAt) {
				delete(lumiPendingQuiz, k)
			}
		}
	}
	lumiPendingQuizMu.Unlock()
	return map[string]any{"success": true, "reply": "퀴즈 나간다! " + q.Question + "\n(맞혀봐! 모르겠으면 \"정답 알려줘\"라고 물어봐)"}
}

func lumiQuizAnswerReply(identity string) map[string]any {
	lumiPendingQuizMu.Lock()
	entry, ok := lumiPendingQuiz[identity]
	delete(lumiPendingQuiz, identity)
	lumiPendingQuizMu.Unlock()
	if !ok {
		return map[string]any{"success": true, "reply": "어? 지금 풀고 있던 퀴즈가 없는데? \"퀴즈 내줘\"라고 말해봐!"}
	}
	return map[string]any{"success": true, "reply": "정답은 \"" + entry.question.DisplayAnswer + "\"였어! 또 낼까?"}
}

// =====================================================================
// 추천/큐레이션: 영상 추천
//
// "뭐 볼까"류 질문에 LLM이 존재하지도 않는 영상 제목을 지어내는 걸 막기
// 위해, 실제 영상 풀(VideoPool - 홈 화면 랜덤 스크롤이 쓰는 것과 같은
// 데이터)에서 진짜로 골라서 링크와 함께 보여준다. 질문에 멤버 이름이
// 있으면 그 멤버 영상 위주로 고른다.
// =====================================================================

func isLumiVideoRecommendRequest(prompt string) bool {
	hasVideoWord := strings.Contains(prompt, "영상") || strings.Contains(prompt, "볼까") || strings.Contains(prompt, "볼영상") || strings.Contains(prompt, "뭐봐")
	hasAskWord := strings.Contains(prompt, "추천") || strings.Contains(prompt, "볼까") || strings.Contains(prompt, "골라") || strings.Contains(prompt, "뭐 봐") || strings.Contains(prompt, "뭐봐")
	return hasVideoWord && hasAskWord
}

func (a *App) lumiVideoRecommendReply(prompt string) map[string]any {
	if a.VideoPool == nil {
		return map[string]any{"success": true, "reply": "지금은 영상 목록을 못 가져오나봐... 홈 화면에서 한번 둘러봐줄래?"}
	}
	videos := a.VideoPool.Get()
	if len(videos) == 0 {
		return map[string]any{"success": true, "reply": "어라, 지금 보여줄 영상이 없네... 잠시 후에 다시 물어봐줘!"}
	}

	var mentionedMember string
	for _, m := range data.SIDEBAR_MEMBERS {
		if m.Name == "스텔라이브" {
			continue
		}
		if strings.Contains(prompt, m.Name) {
			mentionedMember = m.Name
			break
		}
	}

	pool := videos
	if mentionedMember != "" {
		var filtered []video.Video
		for _, v := range videos {
			if v.MemberName == mentionedMember {
				filtered = append(filtered, v)
			}
		}
		if len(filtered) > 0 {
			pool = filtered
		}
	}

	pick := pool[rand.Intn(len(pool))]
	siteHost := a.Cfg.SiteHost
	if siteHost == "" {
		siteHost = "pastellive.co.kr"
	}
	watchURL := "https://" + siteHost + "/watch/" + pick.VideoID

	var reply string
	if mentionedMember != "" {
		reply = mentionedMember + " 영상으로 골라봤어! [" + pick.Title + "](" + watchURL + ")"
	} else {
		reply = "이거 한번 봐봐! [" + pick.Title + "](" + watchURL + ")"
	}
	return map[string]any{"success": true, "reply": reply}
}

// =====================================================================
// 안정성: 답변 품질 안전망 확장
//
// 기존엔 "중국어로 새는" 경우만 감지해서 재시도했다. 작은 로컬 모델이
// 보이는 다른 흔한 실패 패턴 - 캐릭터를 깨고 "저는 AI 언어모델입니다" 식으로
// 자기소개해버리는 것, 같은 단어/구절을 계속 반복하는 것(디코딩 루프) -
// 도 감지해서 한 번 더 재시도하고, 그래도 안 되면 기존과 같은 안전한 고정
// 문구로 대체한다.
// =====================================================================

var lumiSelfDiscloseMarkers = []string{
	"AI 언어 모델", "AI 언어모델", "인공지능 언어 모델", "인공지능 언어모델",
	"저는 AI", "저는 인공지능", "언어 모델로서", "언어모델로서",
	"as an AI", "I'm an AI", "I am an AI", "large language model",
}

func looksLikeBrokenLumiReply(reply string) bool {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return true
	}
	for _, marker := range lumiSelfDiscloseMarkers {
		if strings.Contains(reply, marker) {
			return true
		}
	}
	return hasExcessiveWordRepetition(reply)
}

// hasExcessiveWordRepetition: 작은 모델이 가끔 같은 단어/짧은 구절을 답변이
// 끝날 때까지 계속 반복하는 "디코딩 루프"에 빠지는 경우가 있다 - 전체
// 단어 수 대비 같은 단어가 과도한 비중을 차지하면 이 상태로 간주한다.
// 정상적인 문장에서 조사/어미가 반복되는 정도로는 안 걸리게, 최소 길이와
// 꽤 높은 비중(35%)을 기준으로 잡았다.
func hasExcessiveWordRepetition(s string) bool {
	words := strings.Fields(s)
	if len(words) < 12 {
		return false
	}
	counts := map[string]int{}
	for _, w := range words {
		counts[w]++
	}
	for _, c := range counts {
		if c >= 6 && c*100/len(words) >= 35 {
			return true
		}
	}
	return false
}
