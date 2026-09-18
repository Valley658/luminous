package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"io"
	"log"
	"net/http"

	"pastellive/internal/video"
)

func (a *App) PubsubVerifyHandler(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	challenge := r.URL.Query().Get("hub.challenge")
	if (mode == "subscribe" || mode == "unsubscribe") && challenge != "" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(challenge))
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

type atomFeedEntry struct {
	Title     string `xml:"title"`
	ChannelID string `xml:"channelId"`
	VideoID   string `xml:"videoId"`
}

type atomFeed struct {
	Entries []atomFeedEntry `xml:"entry"`
}

func (a *App) PubsubNotifyHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	signature := r.Header.Get("X-Hub-Signature")
	mac := hmac.New(sha1.New, []byte(a.Cfg.WebSubSecret))
	mac.Write(body)
	expected := "sha1=" + hex.EncodeToString(mac.Sum(nil))
	if signature == "" || !hmac.Equal([]byte(expected), []byte(signature)) {
		reason := "없음"
		if signature != "" {
			reason = "불일치"
		}
		log.Printf("WebSub 알림 거부: 서명 %s (위조되었거나 비밀값이 재구독 이후로 바뀐 상태)", reason)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err == nil {
		for _, entry := range feed.Entries {
			if entry.ChannelID == "" {
				continue
			}
			log.Printf("WebSub: 새 영상 알림 수신 - channel=%s title=%s", entry.ChannelID, entry.Title)
			if entry.VideoID != "" {
				go video.NotifyNewVideoToN8N(a.DB, a.Cfg.N8NNewVideoWebhook, entry.ChannelID, entry.VideoID, entry.Title)
			}
		}
	} else {
		log.Printf("WebSub 알림 처리 중 오류: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
