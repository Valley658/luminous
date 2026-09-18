package video

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	pdb "pastellive/internal/db"
)

const websubHubURL = "https://pubsubhubbub.appspot.com/subscribe"

var websubHTTPClient = &http.Client{Timeout: 10 * time.Second}

func SubscribeToChannelWebsub(channelID string, unsubscribe bool, callbackBase, secret string) bool {
	if len(channelID) < 2 || channelID[:2] != "UC" {
		return false
	}
	topic := "https://www.youtube.com/xml/feeds/videos.xml?channel_id=" + channelID
	callback := callbackBase + "/api/pubsub/callback"
	mode := "subscribe"
	if unsubscribe {
		mode = "unsubscribe"
	}
	form := url.Values{
		"hub.mode":          {mode},
		"hub.topic":         {topic},
		"hub.callback":      {callback},
		"hub.verify":        {"async"},
		"hub.secret":        {secret},
		"hub.lease_seconds": {strconv.Itoa(432000)},
	}
	req, err := http.NewRequest(http.MethodPost, websubHubURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		log.Printf("WebSub 구독 요청 실패(channel=%s): %v", channelID, err)
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := websubHTTPClient.Do(req)
	if err != nil {
		log.Printf("WebSub 구독 요청 실패(channel=%s): %v", channelID, err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 202 || resp.StatusCode == 204
}

func ResubscribeAllChannels(ctx context.Context, d *pdb.DB, callbackBase, secret string) {
	rows, err := d.Query("SELECT DISTINCT channel_id FROM members WHERE channel_id IS NOT NULL AND channel_id != 'UC_KANNA_PLACEHOLDER'")
	if err != nil {
		log.Printf("WebSub 재구독 실패: 멤버 목록 조회 오류 - %v", err)
		return
	}
	var channelIDs []string
	for rows.Next() {
		var ch sql.NullString
		if err := rows.Scan(&ch); err == nil && ch.Valid {
			channelIDs = append(channelIDs, ch.String)
		}
	}
	rows.Close()

	okCount := 0
	for _, ch := range channelIDs {
		if SubscribeToChannelWebsub(ch, false, callbackBase, secret) {
			okCount++
		}
	}
	log.Printf("WebSub 구독 갱신: %d/%d개 채널 구독 요청 완료", okCount, len(channelIDs))
}

func NotifyNewVideoToN8N(d *pdb.DB, webhookURL, channelID, videoID, title string) {
	if webhookURL == "" {
		return
	}
	var memberName string
	if err := d.QueryRow("SELECT member_name FROM members WHERE channel_id = ?", channelID).Scan(&memberName); err != nil {
		memberName = ""
	}
	payload, _ := json.Marshal(map[string]any{
		"channel_id":  channelID,
		"video_id":    videoID,
		"title":       title,
		"member_name": memberName,
	})
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("새 영상 n8n 알림 전송 중 오류(치명적이지 않음): %v", err)
		return
	}
	resp.Body.Close()
}
