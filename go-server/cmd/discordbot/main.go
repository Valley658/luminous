package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"pastellive/internal/config"
)

const (
	svcCheckInterval = 3 * time.Minute
	svcRestartCooldn = 5 * time.Minute
	confirmButtonTTL = 30 * time.Second
)

var (
	authorizedUserID string
	testGuildID      string
)

var managedServices = []string{"PastelliveApp", "NginxStartup"}

func main() {

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("실행 파일 경로를 알 수 없음: %v", err)
	}
	exeDir := parentDir(exePath)
	projectDir := os.Getenv("PROJECT_DIR")
	if projectDir == "" {

		projectDir = parentDir(parentDir(exeDir))
	}
	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = joinPath(projectDir, ".env")
	}
	config.LoadEnvFile(envFile)

	authorizedUserID = strings.TrimSpace(os.Getenv("DISCORD_AUTHORIZED_USER_ID"))
	testGuildID = strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID"))
	if authorizedUserID == "" {
		log.Println("[discordbot] 경고: DISCORD_AUTHORIZED_USER_ID가 설정되지 않음 - 관리자 전용 명령을 아무도 쓸 수 없습니다.")
	}

	token := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
	if token == "" {
		log.Fatal("[discordbot] DISCORD_BOT_TOKEN이 .env에 설정되어 있지 않습니다. 봇을 시작할 수 없습니다.")
	}
	alertChannelID := strings.TrimSpace(os.Getenv("DISCORD_ALERT_CHANNEL_ID"))

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("[discordbot] 세션 생성 실패: %v", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	b := &bot{
		session:        dg,
		alertChannelID: alertChannelID,
		lastRestart:    map[string]time.Time{},
		pending:        map[string]*pendingRestart{},
	}
	dg.AddHandler(b.onReady)
	dg.AddHandler(b.onInteractionCreate)

	if err := dg.Open(); err != nil {
		log.Fatalf("[discordbot] 디스코드 연결 실패: %v", err)
	}
	defer dg.Close()

	log.Println("[discordbot] 실행 중 - Ctrl+C 또는 서비스 종료 신호로 멈춥니다.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGTERM)
	<-sc
}

type pendingRestart struct {
	service     string
	requesterID string
	mu          sync.Mutex
	handled     bool
}

type bot struct {
	session        *discordgo.Session
	alertChannelID string

	mu          sync.Mutex
	lastRestart map[string]time.Time

	pendingMu sync.Mutex
	pending   map[string]*pendingRestart
}

func (b *bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("[discordbot] 로그인 완료: %s#%s (id=%s)\n", r.User.Username, r.User.Discriminator, r.User.ID)

	if err := b.registerCommands(); err != nil {
		log.Printf("[discordbot] 슬래시 명령어 등록 실패: %v\n", err)
	}

	go b.serviceWatchdogLoop()
	b.warnIfAlertChannelPublic()
}

func (b *bot) registerCommands() error {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(managedServices))
	for _, s := range managedServices {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: s, Value: s})
	}
	cmd := &discordgo.ApplicationCommand{
		Name:        "서비스재시작",
		Description: "⚠️ 지정한 Windows 예약 작업을 재시작하고 RDP 세션도 초기화합니다 (버튼으로 한 번 더 확인).",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "서비스",
				Description: "재시작할 서비스",
				Required:    true,
				Choices:     choices,
			},
		},
		DefaultMemberPermissions: permPtr(discordgo.PermissionManageServer),
	}
	_, err := b.session.ApplicationCommandCreate(b.session.State.User.ID, testGuildID, cmd)
	return err
}

func permPtr(p int64) *int64 { return &p }

func (b *bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		data := i.ApplicationCommandData()
		if data.Name == "서비스재시작" {
			b.handleRestartCommand(s, i)
		}
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		if strings.HasPrefix(customID, "svcrestart:") {
			b.handleRestartButton(s, i, customID)
		}
	}
}

func (b *bot) handleRestartCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := interactionUserID(i)
	log.Printf("[discordbot] 명령어 실행: /서비스재시작 by %s\n", userID)
	if userID != authorizedUserID {
		respondEphemeral(s, i, "이 명령어를 사용할 권한이 없습니다.")
		return
	}
	data := i.ApplicationCommandData()
	var service string
	for _, opt := range data.Options {
		if opt.Name == "서비스" {
			service = opt.StringValue()
		}
	}
	if service == "" {
		respondEphemeral(s, i, "서비스 값이 필요합니다.")
		return
	}

	token := fmt.Sprintf("%d", time.Now().UnixNano())
	b.pendingMu.Lock()
	b.pending[token] = &pendingRestart{service: service, requesterID: userID}
	b.pendingMu.Unlock()

	warning := ""
	if service == "NginxStartup" {
		warning = " nginx 재시작 중에는 몇 초간 사이트 접속이 끊길 수 있습니다."
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("⚠️ `%s` 서비스를 정말 재시작할까요?%s", service, warning),
			Flags:   discordgo.MessageFlagsEphemeral,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "재시작 확정", Style: discordgo.DangerButton, CustomID: "svcrestart:confirm:" + token},
					discordgo.Button{Label: "취소", Style: discordgo.SecondaryButton, CustomID: "svcrestart:cancel:" + token},
				}},
			},
		},
	})
	if err != nil {
		log.Printf("[discordbot] 재시작 확인 메시지 전송 실패: %v\n", err)
		return
	}

	time.AfterFunc(confirmButtonTTL, func() {
		b.pendingMu.Lock()
		pr, ok := b.pending[token]
		b.pendingMu.Unlock()
		if !ok {
			return
		}
		pr.mu.Lock()
		already := pr.handled
		pr.handled = true
		pr.mu.Unlock()
		if already {
			return
		}
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Components: &[]discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "재시작 확정", Style: discordgo.DangerButton, CustomID: "svcrestart:confirm:" + token, Disabled: true},
					discordgo.Button{Label: "취소", Style: discordgo.SecondaryButton, CustomID: "svcrestart:cancel:" + token, Disabled: true},
				}},
			},
		})
	})
}

func (b *bot) handleRestartButton(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	parts := strings.SplitN(customID, ":", 3)
	if len(parts) != 3 {
		return
	}
	action, token := parts[1], parts[2]

	b.pendingMu.Lock()
	pr, ok := b.pending[token]
	b.pendingMu.Unlock()
	if !ok {
		respondEphemeral(s, i, "이미 만료된 요청입니다.")
		return
	}

	userID := interactionUserID(i)
	if userID != pr.requesterID {
		respondEphemeral(s, i, "이 버튼을 사용할 권한이 없습니다.")
		return
	}

	pr.mu.Lock()
	if pr.handled {
		pr.mu.Unlock()
		return
	}
	pr.handled = true
	pr.mu.Unlock()

	if action == "cancel" {
		log.Printf("[discordbot] 서비스 재시작 취소: %s by %s\n", pr.service, userID)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{Content: "취소되었습니다.", Components: []discordgo.MessageComponent{}},
		})
		return
	}

	log.Printf("[discordbot] 서비스 재시작 확정: %s by %s\n", pr.service, userID)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Content: fmt.Sprintf("⏳ `%s` 재시작 중...", pr.service), Components: []discordgo.MessageComponent{}},
	})

	ok2, output := serviceRestart(pr.service)
	rdpOK, rdpOutput := resetRDPSession()
	rdpEmoji := "🟡"
	if rdpOK {
		rdpEmoji = "🟢"
		log.Printf("[discordbot] RDP 세션 초기화 성공: %s\n", rdpOutput)
	} else {
		log.Printf("[discordbot] RDP 세션 초기화 실패/대상 없음: %s\n", rdpOutput)
	}
	rdpLine := fmt.Sprintf("\n%s RDP 세션 초기화: %s", rdpEmoji, truncate(rdpOutput, 200))

	var content string
	if ok2 {
		log.Printf("[discordbot] 서비스 재시작 성공: %s\n", pr.service)
		content = fmt.Sprintf("✅ `%s` 재시작 완료.%s", pr.service, rdpLine)
	} else {
		log.Printf("[discordbot] 서비스 재시작 실패: %s - %s\n", pr.service, truncate(output, 400))
		content = fmt.Sprintf("❌ `%s` 재시작 실패: %s%s", pr.service, truncate(output, 400), rdpLine)
	}
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: content})
}

func (b *bot) serviceWatchdogLoop() {
	ticker := time.NewTicker(svcCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		for _, svc := range managedServices {
			status := serviceStatus(svc)
			if status == "실행 중" || strings.EqualFold(status, "Running") {
				continue
			}
			b.mu.Lock()
			last := b.lastRestart[svc]
			now := time.Now()
			if now.Sub(last) < svcRestartCooldn {
				b.mu.Unlock()
				continue
			}
			b.lastRestart[svc] = now
			b.mu.Unlock()

			log.Printf("[discordbot] 워치독: %s 상태 이상(%s) 감지 - 자동 재시작 시도\n", svc, status)
			ok, output := serviceRestart(svc)
			if ok {
				log.Printf("[discordbot] 워치독: %s 자동 재시작 성공\n", svc)
			} else {
				log.Printf("[discordbot] 워치독: %s 자동 재시작 실패: %s\n", svc, truncate(output, 400))
			}
		}
	}
}

func (b *bot) warnIfAlertChannelPublic() {
	if b.alertChannelID == "" {
		return
	}
	ch, err := b.session.Channel(b.alertChannelID)
	if err != nil || ch.GuildID == "" {
		return
	}
	guild, err := b.session.Guild(ch.GuildID)
	if err != nil {
		return
	}
	everyoneID := ch.GuildID
	overwriteAllowsView := true
	for _, ow := range ch.PermissionOverwrites {
		if ow.ID == everyoneID && ow.Type == discordgo.PermissionOverwriteTypeRole {
			if ow.Deny&discordgo.PermissionViewChannel != 0 {
				overwriteAllowsView = false
			}
		}
	}
	baseAllows := false
	for _, role := range guild.Roles {
		if role.ID == everyoneID {
			baseAllows = role.Permissions&discordgo.PermissionViewChannel != 0 || role.Permissions&discordgo.PermissionAdministrator != 0
		}
	}
	if baseAllows && overwriteAllowsView {
		log.Printf("[discordbot] [보안 경고] 알림 채널(#%s, id=%s)을 @everyone이 볼 수 있습니다! "+
			"운영 정보가 서버 전체에 노출되고 있을 수 있습니다 - Discord 서버 설정에서 이 채널을 관리자 전용 비공개 채널로 바꿔주세요.\n",
			ch.Name, ch.ID)
	}
}

func serviceStatus(name string) string {
	if runtime.GOOS != "windows" {
		return "UNKNOWN"
	}
	out, _ := runCombined(10*time.Second, "schtasks", "/query", "/tn", name, "/fo", "list")
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "상태:") || strings.HasPrefix(lower, "status:") {
			idx := strings.Index(line, ":")
			if idx >= 0 {
				return strings.TrimSpace(line[idx+1:])
			}
		}
	}
	return "UNKNOWN"
}

func serviceRestart(name string) (bool, string) {
	if runtime.GOOS != "windows" {
		return false, "윈도우 전용 기능입니다."
	}
	_, _ = runCombined(15*time.Second, "schtasks", "/end", "/tn", name)
	out, err := runCombined(15*time.Second, "schtasks", "/run", "/tn", name)
	out = strings.TrimSpace(out)
	if err != nil {
		if out == "" {
			out = err.Error()
		}
		return false, out
	}
	if out == "" {
		out = name + " 재시작 완료"
	}
	return true, out
}

func resetRDPSession() (bool, string) {
	if runtime.GOOS != "windows" {
		return false, "리눅스에서는 RDP 세션 재연결이 필요 없습니다 (건너뜀)."
	}
	out, err := runCombined(10*time.Second, "query", "user")
	if err != nil && out == "" {
		return false, "명령을 찾을 수 없습니다: " + err.Error()
	}
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty <= 1 {
		return false, "조회된 세션이 없습니다."
	}
	var targetID string
	for _, line := range lines[1:] {
		if !strings.Contains(line, "Disc") {
			continue
		}
		for _, tok := range strings.Fields(line) {
			if isAllDigits(tok) {
				targetID = tok
				break
			}
		}
		if targetID != "" {
			break
		}
	}
	if targetID == "" {
		return false, "재연결할 연결 끊김(Disc) 세션이 없습니다 (이미 정상 상태일 수 있음)."
	}
	out2, err2 := runCombined(15*time.Second, "tscon", targetID, "/dest:console")
	out2 = strings.TrimSpace(out2)
	if err2 != nil {
		if out2 == "" {
			out2 = err2.Error()
		}
		return false, out2
	}
	if out2 == "" {
		out2 = fmt.Sprintf("세션 %s -> 콘솔로 재연결 완료", targetID)
	}
	return true, out2
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: discordgo.MessageFlagsEphemeral},
	})
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func parentDir(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return p
	}
	return p[:i]
}

func joinPath(parts ...string) string {
	return strings.Join(parts, string(os.PathSeparator))
}
