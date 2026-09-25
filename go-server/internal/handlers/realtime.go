package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"pastellive/internal/httputil"
	"pastellive/internal/models"
	"pastellive/internal/realtime"
)

// CreateNotify는 models.CreateNotification으로 DB에 알림을 남기고, 성공하면
// 그 수신자가 지금 SSE로 접속해 있을 경우 실시간으로도 알려준다(폴링 없이).
// 기존 models.CreateNotification 호출부를 전부 이걸로 바꿔서 쓴다.
func (a *App) CreateNotify(recipientUserID, actorUserID int64, actorNickname, notifType, targetType string, targetID int64, previewText string) error {
	if err := models.CreateNotification(a.DB, recipientUserID, actorUserID, actorNickname, notifType, targetType, targetID, previewText); err != nil {
		return err
	}
	if recipientUserID != 0 && recipientUserID != actorUserID {
		unread, _ := models.CountUnreadNotifications(a.DB, recipientUserID)
		payload, _ := json.Marshal(map[string]any{
			"type":           notifType,
			"actor_nickname": actorNickname,
			"unread_count":   unread,
		})
		a.Notify.Publish(recipientUserID, realtime.Event{Name: "notification", Data: string(payload)})
	}
	return nil
}

// ApiNotificationsStreamHandler는 Server-Sent Events(SSE)로 실시간 알림을
// 밀어준다. 로그인한 사용자만, 자신의 알림만 받는다. 클라이언트는 기존
// 폴링 대신(또는 폴링 주기를 크게 늘리고) 이 연결을 열어두면 새 알림이
// 생기는 즉시(브라우저 탭이 열려 있는 동안) 이벤트를 받을 수 있다.
func (a *App) ApiNotificationsStreamHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessionUserID(r)
	if userID == 0 {
		httputil.JSONError(w, http.StatusUnauthorized, "로그인이 필요합니다.")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httputil.JSONError(w, http.StatusInternalServerError, "실시간 스트림을 지원하지 않는 서버 환경입니다.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx가 SSE를 버퍼링하지 않도록
	w.WriteHeader(http.StatusOK)

	ch, unsubscribe := a.Notify.Subscribe(userID)
	defer unsubscribe()

	// 연결 직후 커넥션이 살아있음을 클라이언트에 바로 알림(재연결 로직이
	// "연결됨" 상태를 바로 판단할 수 있게).
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	// nginx/브라우저가 idle 커넥션을 끊지 않도록 주기적으로 keep-alive 핑을 보낸다.
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			_, _ = w.Write([]byte("event: " + ev.Name + "\ndata: " + ev.Data + "\n\n"))
			flusher.Flush()
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}
