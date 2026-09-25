package handlers

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
)

// runWeeklyDigestJob은 매일 정해진 시각에 실행되는 스케줄러 작업이지만,
// 월요일이 아니면 아무것도 하지 않고 조용히 리턴한다(scheduler.go에는
// "매주" 트리거가 따로 없어서, 매일 실행되는 runDailyAt 위에서 요일만
// 걸러내는 방식을 씀 - DB 백업 스케줄과 달리 이건 Go 프로세스 안에서 직접
// 도는 작업이라 별도 Windows 작업 스케줄러 등록이 필요 없음).
func (a *App) runWeeklyDigestJob() {
	if time.Now().In(models.KST).Weekday() != time.Monday {
		return
	}
	a.SendWeeklyDigest()
}

// SendWeeklyDigest는 최근 7일간 인기 팬아트/최신 커뮤니티 글을 모아 이메일을
// 등록하고 수신거부하지 않은 모든 사용자에게 보낸다. SMTP가 설정 안 돼있으면
// 조용히 스킵(로그만 남김) - 관리자가 이메일 기능을 아직 설정하지 않았다고
// 매번 실패 로그로 화면을 채우지 않기 위함.
func (a *App) SendWeeklyDigest() {
	if !a.Email.Enabled() {
		log.Printf("[주간 다이제스트] SMTP가 설정되지 않아 건너뜀")
		return
	}
	fanarts, err := models.GetTopFanartThisWeek(a.DB, 5)
	if err != nil {
		log.Printf("[주간 다이제스트] 인기 팬아트 조회 실패: %v", err)
	}
	posts, err := models.GetRecentCommunityPostsThisWeek(a.DB, 5)
	if err != nil {
		log.Printf("[주간 다이제스트] 최신 커뮤니티 글 조회 실패: %v", err)
	}
	if len(fanarts) == 0 && len(posts) == 0 {
		log.Printf("[주간 다이제스트] 지난 7일간 새 콘텐츠가 없어 건너뜀")
		return
	}
	recipients, err := models.GetEmailDigestRecipients(a.DB)
	if err != nil {
		log.Printf("[주간 다이제스트] 수신자 목록 조회 실패: %v", err)
		return
	}
	if len(recipients) == 0 {
		log.Printf("[주간 다이제스트] 수신 대상이 없어 건너뜀")
		return
	}

	var fanartHTML strings.Builder
	for _, f := range fanarts {
		fanartHTML.WriteString(fmt.Sprintf(`
			<div style="margin-bottom:14px;">
				<img src="https://%s%s" style="width:100%%;max-width:400px;border-radius:8px;display:block;margin-bottom:6px;">
				<div style="font-size:14px;color:#222;"><b>%s</b> · %s님 · 좋아요 %d개</div>
			</div>`,
			html.EscapeString(a.Cfg.SiteHost), html.EscapeString(f.ImageURL),
			html.EscapeString(firstNonEmpty(f.Title, "무제")), html.EscapeString(f.Nickname), f.Likes))
	}
	var postHTML strings.Builder
	for _, p := range posts {
		preview := []rune(p.Content)
		if len(preview) > 80 {
			preview = append(preview[:80], []rune("...")...)
		}
		postHTML.WriteString(fmt.Sprintf(`
			<div style="margin-bottom:12px;padding-bottom:12px;border-bottom:1px solid #eee;">
				<div style="font-size:14px;color:#222;"><b>%s</b> · %s 커뮤니티 · %s님</div>
				<div style="font-size:13px;color:#666;margin-top:4px;">%s</div>
			</div>`,
			html.EscapeString(firstNonEmpty(p.Title, "제목 없음")), html.EscapeString(p.MemberName),
			html.EscapeString(p.Nickname), html.EscapeString(string(preview))))
	}

	subject := "[루미너스] 이번 주 인기 팬아트 & 커뮤니티 소식"
	sent := 0
	for _, rcpt := range recipients {
		unsubURL := fmt.Sprintf("https://%s/email-digest/unsubscribe?uid=%d&token=%s",
			a.Cfg.SiteHost, rcpt.UserID, models.DigestUnsubscribeToken(a.Cfg.SecretKey, rcpt.UserID))
		bodyHTML := fmt.Sprintf(`
			<div style="font-family:'Malgun Gothic',sans-serif;max-width:480px;margin:0 auto;padding:24px;color:#222;">
				<h2 style="color:#38bdf8;">이번 주 루미너스 소식</h2>
				<p>%s님, 안녕하세요! 지난 한 주 동안 루미너스에서 있었던 일들을 모아왔어요.</p>
				%s
				%s
				<p style="margin-top:24px;"><a href="https://%s/" style="background:#38bdf8;color:#fff;padding:10px 20px;border-radius:8px;text-decoration:none;font-weight:bold;">루미너스 방문하기</a></p>
				<p style="font-size:11px;color:#aaa;margin-top:28px;">이 메일이 반갑지 않다면 <a href="%s" style="color:#aaa;">여기서 수신거부</a>할 수 있어요.</p>
			</div>`,
			html.EscapeString(firstNonEmpty(rcpt.Nickname, "회원")),
			fanartHTML.String(), postHTML.String(), html.EscapeString(a.Cfg.SiteHost), unsubURL)

		if err := a.Email.Send(rcpt.Email, subject, bodyHTML); err != nil {
			log.Printf("[주간 다이제스트] 발송 실패(user_id=%d): %v", rcpt.UserID, err)
			continue
		}
		sent++
	}
	log.Printf("[주간 다이제스트] %d/%d명에게 발송 완료 (팬아트 %d개, 게시글 %d개)", sent, len(recipients), len(fanarts), len(posts))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ApiEmailDigestUnsubscribeHandler는 이메일 속 "수신거부" 링크가 가리키는
// 주소. 로그인 없이(메일 클라이언트/브라우저에서 클릭만으로) 동작해야 해서
// 세션 대신 서명된 토큰으로 본인 확인을 한다.
func (a *App) ApiEmailDigestUnsubscribeHandler(w http.ResponseWriter, r *http.Request) {
	uidStr := r.URL.Query().Get("uid")
	token := r.URL.Query().Get("token")
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil || token == "" || !models.VerifyDigestUnsubscribeToken(a.Cfg.SecretKey, uid, token) {
		httputil.JSONError(w, http.StatusBadRequest, "유효하지 않은 링크입니다.")
		return
	}
	if err := models.SetEmailDigestUnsubscribed(a.DB, uid); err != nil {
		httputil.JSONError(w, http.StatusInternalServerError, "처리 중 오류가 발생했습니다.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>수신거부 완료</title></head>
		<body style="font-family:sans-serif;text-align:center;padding:60px 20px;color:#222;">
			<h2>수신거부가 완료됐어요</h2>
			<p>앞으로 루미너스 주간 소식 메일을 보내지 않을게요.</p>
		</body></html>`))
}
