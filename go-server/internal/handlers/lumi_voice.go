package handlers

// lumi_voice.go - 루미 답변을 루미 목소리로 읽어 주기 (VoiceStudio 로컬 TTS 연동)
//
// 구조
//   - 브라우저: 루미 답변 옆 스피커 버튼 → POST /api/lumi/voice {text}
//   - 서버: 이미 만든 음성 파일이 있으면 그 주소를 바로 돌려주고, 없으면
//     같은 PC에서 도는 VoiceStudio(http://127.0.0.1:3900)에 만들어 달라고 해서
//     static/lumi_voice/<해시>.mp3 로 저장한 뒤 주소를 돌려준다(nginx가 /static/ 으로 바로 서빙).
//   - 고정 멘트(갤러리/제작자/사이트 출처 얼버무림, 대화 초기화 멘트, 거절 문구 등)는
//     서버가 켜질 때와 10분마다 빠진 것만 미리 만들어 둔다 → 누르면 기다림 없이 바로 재생.
//
// 안전장치
//   - "루미가 실제로 한 말"만 읽어 준다(아무 문장이나 루미 목소리로 만들게 하면 악용됨):
//     최근 답변(sendLumiReply에서 기록) / 고정 멘트 / 로그인 사용자의 저장된 대화 기록만 허용.
//   - 음성 생성은 한 번에 하나씩(GPU 공유), IP당 10분에 20개까지.
//   - VOICESTUDIO_VOICE 가 비어 있으면 기능 전체가 꺼진다(버튼도 안 보임).
//   - 목소리(VOICESTUDIO_VOICE)를 바꾸면 파일 이름(해시)이 달라져서 새 목소리로 다시 만들어진다.
//
// 음성 파일 보관/재사용 (2026-10-02)
//   - 만든 음성 파일은 지우지 않고 계속 쌓아 둔다(문장 하나당 수십 KB).
//   - 답변을 "문장 단위"로 잘라서 문장마다 따로 녹음/저장한다. 다른 방문자의 답변에
//     같은 문장("왼쪽 사이드바에서 바로 보러 갈 수 있어!" 같은)이 나오면 그 문장은
//     새로 만들지 않고 저장된 파일을 그대로 쓴다 → 쓸수록 바로 나오는 문장이 늘어난다.
//   - 브라우저는 문장 파일 목록(urls)을 받아 차례대로 이어서 재생하고, 앞 문장이
//     준비되면 나머지가 만들어지는 동안 먼저 재생을 시작한다.
//   - 예전 방식(답변 통째로 한 파일)으로 만든 파일이 있으면 그것도 그대로 쓴다.
//
// .env 설정
//   VOICESTUDIO_VOICE=<VoiceStudio에서 만든 루미 목소리 ID>   (필수, 비우면 기능 꺼짐)
//   VOICESTUDIO_URL=http://127.0.0.1:3900                    (기본값)
//   VOICESTUDIO_MODEL=tts-1   (기본값 - VoiceStudio 앱에서 지금 쓰는 엔진을 그대로 씀. 특정 엔진: omnivoice 등)
//   VOICESTUDIO_LANGUAGE=ko                                  (기본값)
//   VOICESTUDIO_API_KEY=                                     (VoiceStudio에 키를 걸었을 때만)

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pastellive/internal/httputil"
	"pastellive/internal/lumidialogue"
	"pastellive/internal/models"
)

const (
	lumiVoiceMaxRunes      = 800
	lumiVoiceGenTimeout    = 120 * time.Second
	lumiVoiceQueueWait     = 90 * time.Second
	lumiVoiceIPLimit       = 20
	lumiVoiceIPWindow      = 10 * time.Minute
	lumiVoiceRecentMax     = 5000
	lumiVoiceRecentTTL     = 48 * time.Hour
	lumiVoiceWarmupEvery   = 10 * time.Minute
	lumiVoiceRefusalLine   = "그러한 질문은 답변할 수 없어! 미안해~"
	lumiVoiceFixedFileName = "lumi_voice_fixed.json"
)

type lumiVoiceConf struct {
	URL, Voice, Model, Lang, Key, Rev string
	Enabled                           bool
}

func lumiVoiceEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func lumiVoiceConfig() lumiVoiceConf {
	c := lumiVoiceConf{
		URL:   strings.TrimRight(lumiVoiceEnv("VOICESTUDIO_URL", "http://127.0.0.1:3900"), "/"),
		Voice: lumiVoiceEnv("VOICESTUDIO_VOICE", ""),
		Model: lumiVoiceEnv("VOICESTUDIO_MODEL", "tts-1"),
		Lang:  lumiVoiceEnv("VOICESTUDIO_LANGUAGE", "ko"),
		Key:   strings.TrimSpace(os.Getenv("VOICESTUDIO_API_KEY")),
		Rev:   strings.TrimSpace(os.Getenv("VOICESTUDIO_VOICE_REV")),
	}
	c.Enabled = c.Voice != ""
	return c
}

func lumiTextHash(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

// 파일 이름 = 목소리+모델+문장 해시. 목소리를 바꾸면 자동으로 새로 만들어진다.
func lumiVoiceFileName(c lumiVoiceConf, text string) string {
	key := c.Voice + "\n" + c.Model + "\n" + strings.TrimSpace(text)
	if c.Rev != "" { // 같은 프로필 ID에 목소리를 덮어쓴 경우 새로 녹음되게 (루미목소리_바꾸기.bat 이 올려 줌)
		key = c.Rev + "\n" + key
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + ".mp3"
}

func (a *App) lumiVoiceDir() string { return filepath.Join(a.Cfg.StaticDir, "lumi_voice") }

func (a *App) lumiVoiceHas(name string) bool {
	_, err := os.Stat(filepath.Join(a.lumiVoiceDir(), name))
	return err == nil
}

/* ---------- 문장 단위로 자르기 ---------- */

var (
	reVoiceSentenceEnd = regexp.MustCompile(`[.!?。~…]+["')\]」』]*(\s+|$)|\n+`)
	reVoiceRepeatPunct = regexp.MustCompile(`([!?~.])[!?~.]+`)
)

const (
	lumiVoiceSegMinRunes = 8  // 이보다 짧은 조각("응!", "좋아!")은 다음 문장에 붙여서 자연스럽게 읽힌다
	lumiVoiceSegMax      = 24 // 문장이 너무 많으면 뒤쪽은 합쳐서 하나로
)

// lumiVoiceNormSeg 는 같은 문장을 같은 파일로 찾기 위한 정리(띄어쓰기/반복 문장부호만 통일).
func lumiVoiceNormSeg(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = reVoiceRepeatPunct.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// lumiVoiceSegments 는 답변을 읽을 문장 목록으로 자른다.
func lumiVoiceSegments(text string) []string {
	sp := lumiVoiceSpeakable(text)
	if sp == "" {
		return nil
	}
	var raw []string
	last := 0
	for _, m := range reVoiceSentenceEnd.FindAllStringIndex(sp, -1) {
		raw = append(raw, sp[last:m[1]])
		last = m[1]
	}
	if last < len(sp) {
		raw = append(raw, sp[last:])
	}
	var out []string
	carry := ""
	for _, r := range raw {
		seg := lumiVoiceNormSeg(carry + " " + r)
		if seg == "" {
			continue
		}
		if utf8.RuneCountInString(seg) < lumiVoiceSegMinRunes {
			carry = seg
			continue
		}
		carry = ""
		out = append(out, seg)
	}
	if carry != "" {
		if len(out) > 0 {
			out[len(out)-1] = lumiVoiceNormSeg(out[len(out)-1] + " " + carry)
		} else {
			out = append(out, carry)
		}
	}
	if len(out) > lumiVoiceSegMax {
		tail := strings.Join(out[lumiVoiceSegMax-1:], " ")
		out = append(out[:lumiVoiceSegMax-1], tail)
	}
	return out
}

// lumiVoicePlan 은 답변 하나를 재생할 파일 목록. 예전 방식의 통째 파일이 있으면 그것 하나.
func (a *App) lumiVoicePlan(c lumiVoiceConf, text string) []string {
	if whole := lumiVoiceFileName(c, text); a.lumiVoiceHas(whole) {
		return []string{whole}
	}
	segs := lumiVoiceSegments(text)
	names := make([]string, len(segs))
	for i, seg := range segs {
		names[i] = lumiVoiceFileName(c, seg)
	}
	return names
}

/* ---------- 루미가 실제로 한 말 기록 ---------- */

var lumiVoiceRecent = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// rememberLumiVoiceText 는 sendLumiReply 가 답변을 보낼 때마다 호출된다.
func rememberLumiVoiceText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	now := time.Now()
	lumiVoiceRecent.Lock()
	defer lumiVoiceRecent.Unlock()
	lumiVoiceRecent.m[lumiTextHash(text)] = now
	if len(lumiVoiceRecent.m) > lumiVoiceRecentMax {
		for k, t := range lumiVoiceRecent.m {
			if now.Sub(t) > lumiVoiceRecentTTL || len(lumiVoiceRecent.m) > lumiVoiceRecentMax {
				delete(lumiVoiceRecent.m, k)
			}
		}
	}
}

func lumiVoiceWasSaid(text string) bool {
	lumiVoiceRecent.Lock()
	defer lumiVoiceRecent.Unlock()
	t, ok := lumiVoiceRecent.m[lumiTextHash(text)]
	return ok && time.Since(t) < lumiVoiceRecentTTL
}

// lumiVoiceFixedLines 는 미리 녹음해 둘 고정 멘트 전체.
//   - lumi_dialogue.json 의 고정 대사(갤러리/제작자/사이트 출처)
//   - lumi_voice_fixed.json 의 "lines" (화면에서 바로 띄우는 멘트: 대화 초기화 등)
//   - 주제 밖 질문 거절 문구
func lumiVoiceFixedLines() []string {
	d := lumidialogue.Load(lumidialogue.DefaultPath())
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(lumiVoiceRefusalLine)
	for _, group := range [][]string{d.GalleryShowLines, d.CreatorDeflectLines, d.SiteOriginDeflectLines} {
		for _, s := range group {
			add(s)
		}
	}
	path := filepath.Join(filepath.Dir(lumidialogue.DefaultPath()), lumiVoiceFixedFileName)
	if raw, err := os.ReadFile(path); err == nil {
		var f struct {
			Lines []string `json:"lines"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			log.Printf("[루미 목소리] %s 읽기 실패(JSON 문법 확인): %v", lumiVoiceFixedFileName, err)
		}
		for _, s := range f.Lines {
			add(s)
		}
	}
	return out
}

func lumiVoiceIsFixed(text string) bool {
	text = strings.TrimSpace(text)
	for _, s := range lumiVoiceFixedLines() {
		if s == text {
			return true
		}
	}
	return false
}

func (a *App) lumiVoiceInHistory(userID int64, text string) bool {
	if userID == 0 || a.LumiDB == nil {
		return false
	}
	msgs, err := models.ListLumiChatHistory(a.LumiDB, userID, 100)
	if err != nil {
		return false
	}
	text = strings.TrimSpace(text)
	for _, m := range msgs {
		if strings.TrimSpace(m.Reply) == text {
			return true
		}
	}
	return false
}

/* ---------- IP 제한 ---------- */

var lumiVoiceIPHits = struct {
	sync.Mutex
	m map[string][]time.Time
}{m: map[string][]time.Time{}}

func lumiVoiceAllowIP(ip string) bool {
	now := time.Now()
	lumiVoiceIPHits.Lock()
	defer lumiVoiceIPHits.Unlock()
	kept := lumiVoiceIPHits.m[ip][:0]
	for _, t := range lumiVoiceIPHits.m[ip] {
		if now.Sub(t) < lumiVoiceIPWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= lumiVoiceIPLimit {
		lumiVoiceIPHits.m[ip] = kept
		return false
	}
	lumiVoiceIPHits.m[ip] = append(kept, now)
	if len(lumiVoiceIPHits.m) > 10000 {
		lumiVoiceIPHits.m = map[string][]time.Time{}
	}
	return true
}

/* ---------- VoiceStudio 호출 ---------- */

// 한 번에 하나씩만 만든다 (GPU를 로컬AI/이미지 서비스와 나눠 쓰므로).
var lumiVoiceSem = make(chan struct{}, 1)

var (
	reMdLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reURL      = regexp.MustCompile(`https?://\S+`)
	reMdMarks  = regexp.MustCompile("[*_`#>|]+|~~") // 한 개짜리 ~ 는 말끝("~")이라 남겨 둔다(문장 자르기에 씀)
	reEmoji    = regexp.MustCompile(`[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{FE0F}\x{200D}]`)
	reSpaces   = regexp.MustCompile(`[ \t]+`)
	reNewlines = regexp.MustCompile(`\n{2,}`)
)

// 읽어 줄 문장 정리: 마크다운 기호, 링크 주소, 이모지는 소리 내 읽으면 이상하니 뺀다.
func lumiVoiceSpeakable(text string) string {
	s := reMdLink.ReplaceAllString(text, "$1")
	s = reURL.ReplaceAllString(s, "")
	s = reMdMarks.ReplaceAllString(s, "")
	s = reEmoji.ReplaceAllString(s, "")
	s = reSpaces.ReplaceAllString(s, " ")
	s = reNewlines.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

var lumiVoiceHTTP = &http.Client{Timeout: lumiVoiceGenTimeout}

func (a *App) lumiVoiceSynthesize(ctx context.Context, c lumiVoiceConf, text string) ([]byte, error) {
	input := lumiVoiceSpeakable(text)
	if input == "" {
		return nil, errors.New("읽을 내용이 없음")
	}
	body, _ := json.Marshal(map[string]any{
		"model":           c.Model,
		"input":           input,
		"voice":           c.Voice,
		"response_format": "mp3",
		"language":        c.Lang,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	res, err := lumiVoiceHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("VoiceStudio 연결 실패(%s, 켜져 있는지 확인): %w", c.URL, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 30<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("VoiceStudio 오류 %d: %s", res.StatusCode, strings.TrimSpace(string(data[:min(len(data), 300)])))
	}
	// 보통은 mp3 바이트가 그대로 오지만, JSON(base64)으로 오는 버전도 대비한다.
	if ct := res.Header.Get("Content-Type"); strings.Contains(ct, "json") || (len(data) > 0 && data[0] == '{') {
		var j map[string]any
		if err := json.Unmarshal(data, &j); err != nil {
			return nil, fmt.Errorf("VoiceStudio 응답을 이해하지 못함: %w", err)
		}
		for _, k := range []string{"audio", "data", "b64_json", "audio_base64"} {
			if s, ok := j[k].(string); ok && s != "" {
				if i := strings.Index(s, ","); strings.HasPrefix(s, "data:") && i > 0 {
					s = s[i+1:]
				}
				return base64.StdEncoding.DecodeString(s)
			}
		}
		return nil, fmt.Errorf("VoiceStudio 응답에 음성이 없음: %s", strings.TrimSpace(string(data[:min(len(data), 300)])))
	}
	if len(data) < 64 {
		return nil, errors.New("VoiceStudio가 빈 음성을 보냄")
	}
	return data, nil
}

// lumiVoiceEnsure 는 음성 파일이 없으면 만들고, 파일 이름을 돌려준다.
func (a *App) lumiVoiceEnsure(ctx context.Context, c lumiVoiceConf, text string) (string, error) {
	name := lumiVoiceFileName(c, text)
	dir := a.lumiVoiceDir()
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return name, nil
	}
	wait, cancel := context.WithTimeout(ctx, lumiVoiceQueueWait)
	defer cancel()
	select {
	case lumiVoiceSem <- struct{}{}:
	case <-wait.Done():
		return "", errors.New("busy")
	}
	defer func() { <-lumiVoiceSem }()
	if _, err := os.Stat(path); err == nil { // 기다리는 동안 다른 요청이 만들었을 수 있음
		return name, nil
	}
	audio, err := a.lumiVoiceSynthesize(ctx, c, text)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, audio, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return name, nil
}

/* ---------- API ---------- */

// GET /api/lumi/voice/status - 듣기 버튼을 보여 줄지
func (a *App) ApiLumiVoiceStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"success": true, "enabled": lumiVoiceConfig().Enabled})
}

// POST /api/lumi/voice {text} → {url}
func (a *App) ApiLumiVoiceHandler(w http.ResponseWriter, r *http.Request) {
	c := lumiVoiceConfig()
	if !c.Enabled {
		httputil.JSONError(w, http.StatusServiceUnavailable, "루미 목소리는 아직 준비 중이야!")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "요청 형식이 이상해.")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || utf8.RuneCountInString(text) > lumiVoiceMaxRunes {
		httputil.JSONError(w, http.StatusBadRequest, "이 답변은 너무 길어서 읽어 줄 수가 없어!")
		return
	}
	plan := a.lumiVoicePlan(c, text)
	if len(plan) == 0 {
		httputil.JSONError(w, http.StatusBadRequest, "읽을 내용이 없어!")
		return
	}
	// 앞에서부터 이미 있는 문장 파일들(먼저 재생 시작용)
	var ready []string
	for _, n := range plan {
		if !a.lumiVoiceHas(n) {
			break
		}
		ready = append(ready, "/static/lumi_voice/"+n)
	}
	if len(ready) == len(plan) {
		writeJSON(w, map[string]any{"success": true, "url": ready[0], "urls": ready, "total": len(plan)})
		return
	}
	name := lumiVoiceFileName(c, text) // 이 답변의 생성 작업 이름
	if !(lumiVoiceWasSaid(text) || lumiVoiceIsFixed(text) || a.lumiVoiceInHistory(sessionUserID(r), text)) {
		httputil.JSONError(w, http.StatusForbidden, "루미가 한 말만 읽어 줄 수 있어!")
		return
	}
	// [2026-10-02] 예전엔 여기서 음성이 다 만들어질 때까지 요청을 붙잡고 있었는데,
	// 그래픽카드가 작아(GTX 1060 3GB) 루미 AI 직후엔 1분 넘게 걸리고 Cloudflare가
	// 100초에서 요청을 끊어서 버튼이 계속 돌기만 했다. 이제는 백그라운드로 만들기
	// 시작하고 바로 "준비 중"(202)을 돌려준다. 브라우저는 몇 초마다 다시 물어보고,
	// 파일이 생기면 위의 os.Stat 에서 바로 주소를 받는다.
	if err, failed := lumiVoiceTakeFailure(name); failed {
		log.Printf("[루미 목소리] 생성 실패: %v", err)
		httputil.JSONError(w, http.StatusServiceUnavailable, "지금은 목소리가 안 나와... 나중에 다시 눌러줘!")
		return
	}
	if !lumiVoiceInflight(name) {
		if !lumiVoiceAllowIP(httputil.GetClientIP(r)) {
			httputil.JSONError(w, http.StatusTooManyRequests, "목이 아파... 조금 있다가 다시 눌러줘!")
			return
		}
		a.lumiVoiceStartBackground(c, text)
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"success": true, "pending": true, "urls": ready, "total": len(plan)})
}

/* ---------- 백그라운드 생성 (중복 방지 + 실패 기록) ---------- */

var lumiVoiceJobs = struct {
	sync.Mutex
	inflight map[string]bool
	failed   map[string]error
}{inflight: map[string]bool{}, failed: map[string]error{}}

func lumiVoiceInflight(name string) bool {
	lumiVoiceJobs.Lock()
	defer lumiVoiceJobs.Unlock()
	return lumiVoiceJobs.inflight[name]
}

// lumiVoiceTakeFailure 는 직전 생성이 실패했으면 그 오류를 한 번 돌려주고 지운다
// (다음에 다시 누르면 새로 시도함).
func lumiVoiceTakeFailure(name string) (error, bool) {
	lumiVoiceJobs.Lock()
	defer lumiVoiceJobs.Unlock()
	err, ok := lumiVoiceJobs.failed[name]
	if ok {
		delete(lumiVoiceJobs.failed, name)
	}
	return err, ok
}

func (a *App) lumiVoiceStartBackground(c lumiVoiceConf, text string) {
	name := lumiVoiceFileName(c, text)
	lumiVoiceJobs.Lock()
	if lumiVoiceJobs.inflight[name] {
		lumiVoiceJobs.Unlock()
		return
	}
	lumiVoiceJobs.inflight[name] = true
	delete(lumiVoiceJobs.failed, name)
	lumiVoiceJobs.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[루미 목소리] 생성 중 오류: %v", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), lumiVoiceGenTimeout+lumiVoiceQueueWait*2)
		defer cancel()
		var err error
		for _, seg := range a.lumiVoiceSegmentsToMake(c, text) {
			if _, err = a.lumiVoiceEnsure(ctx, c, seg); err != nil {
				break
			}
		}
		lumiVoiceJobs.Lock()
		delete(lumiVoiceJobs.inflight, name)
		if err != nil {
			lumiVoiceJobs.failed[name] = err
			if len(lumiVoiceJobs.failed) > 500 {
				lumiVoiceJobs.failed = map[string]error{}
			}
		}
		lumiVoiceJobs.Unlock()
	}()
}

// lumiVoicePrefetch 는 루미가 대답하자마자 그 대답의 음성을 미리 만들기 시작한다.
// 방문자가 대답을 읽는 동안 만들어 두면 🔊를 눌렀을 때 바로 나온다.
func (a *App) lumiVoicePrefetch(text string) {
	c := lumiVoiceConfig()
	text = strings.TrimSpace(text)
	if !c.Enabled || text == "" || utf8.RuneCountInString(text) > lumiVoiceMaxRunes {
		return
	}
	if len(a.lumiVoiceSegmentsToMake(c, text)) == 0 {
		return
	}
	a.lumiVoiceStartBackground(c, text)
}

// lumiVoiceSegmentsToMake 는 이 답변에서 아직 녹음 안 된 문장들(순서대로).
func (a *App) lumiVoiceSegmentsToMake(c lumiVoiceConf, text string) []string {
	if a.lumiVoiceHas(lumiVoiceFileName(c, text)) {
		return nil
	}
	var out []string
	for _, seg := range lumiVoiceSegments(text) {
		if !a.lumiVoiceHas(lumiVoiceFileName(c, seg)) {
			out = append(out, seg)
		}
	}
	return out
}

/* ---------- 고정 멘트 미리 녹음 ---------- */

// StartLumiVoiceWarmup 은 서버 시작 20초 뒤, 그리고 10분마다 고정 멘트 중
// 아직 녹음 안 된 것만 만들어 둔다. lumi_dialogue.json / lumi_voice_fixed.json 에
// 멘트를 추가하거나 목소리를 바꾸면 다음 주기에 자동으로 새로 녹음된다.
func (a *App) StartLumiVoiceWarmup() {
	go func() {
		time.Sleep(20 * time.Second)
		for {
			a.lumiVoiceWarmupOnce()
			time.Sleep(lumiVoiceWarmupEvery)
		}
	}()
}

func (a *App) lumiVoiceWarmupOnce() {
	c := lumiVoiceConfig()
	if !c.Enabled {
		return
	}
	made, failed := 0, 0
	for _, line := range lumiVoiceFixedLines() {
		todo := a.lumiVoiceSegmentsToMake(c, line)
		if len(todo) == 0 {
			continue
		}
		var err error
		for _, seg := range todo {
			ctx, cancel := context.WithTimeout(context.Background(), lumiVoiceGenTimeout+lumiVoiceQueueWait)
			_, err = a.lumiVoiceEnsure(ctx, c, seg)
			cancel()
			if err != nil {
				break
			}
		}
		if err != nil {
			failed++
			if failed == 1 {
				log.Printf("[루미 목소리] 고정 멘트 미리 녹음 실패: %v", err)
			}
			if failed >= 3 { // VoiceStudio가 꺼져 있으면 다음 주기에 다시
				break
			}
			continue
		}
		made++
	}
	if made > 0 || failed > 0 {
		log.Printf("[루미 목소리] 고정 멘트 미리 녹음: 새로 %d개, 실패 %d개", made, failed)
	}
}
