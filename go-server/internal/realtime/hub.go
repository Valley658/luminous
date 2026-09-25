// Package realtime은 서버 실시간성(SSE) 기능의 기반이 되는 아주 단순한
// in-memory pub/sub 허브다. 외부 브로커(Redis 등) 없이, 지금 이 프로세스에
// 붙어있는 사용자에게만 알림을 즉시 밀어주는 용도라서 이 정도로 충분하다.
// (여러 서버 인스턴스로 수평 확장하게 되면 별도 브로커가 필요하지만,
// 지금은 단일 프로세스로 자체 호스팅 중이라 해당 없음.)
package realtime

import "sync"

// Event는 SSE로 클라이언트에 보내는 한 건의 이벤트.
type Event struct {
	Name string // SSE의 "event:" 필드 (예: "notification")
	Data string // SSE의 "data:" 필드 (JSON 문자열)
}

// Hub는 user_id별로 구독 중인 채널 목록을 들고 있다가, Publish가 호출되면
// 해당 사용자의 모든 구독자(여러 탭/기기에서 접속 가능)에게 이벤트를 보낸다.
type Hub struct {
	mu   sync.Mutex
	subs map[int64]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[int64]map[chan Event]struct{})}
}

// Subscribe는 userID용 새 구독 채널을 만들어 돌려준다. 반환된 unsubscribe
// 함수는 연결이 끊길 때(핸들러의 defer) 반드시 호출해야 채널이 누수되지 않는다.
func (h *Hub) Subscribe(userID int64) (ch chan Event, unsubscribe func()) {
	ch = make(chan Event, 8)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = make(map[chan Event]struct{})
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if set, ok := h.subs[userID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, userID)
			}
		}
		close(ch)
	}
	return ch, unsubscribe
}

// Publish는 userID를 구독 중인 모든 채널에 이벤트를 보낸다. 채널이 가득 차서
// (클라이언트가 느리거나 응답이 없어) 막혀있으면 블로킹하지 않고 그냥 건너뛴다
// - 실시간 "알림 왔어요" 신호일 뿐이고, 실제 목록은 REST API로 다시 불러오는
// 구조라서 신호 하나 유실돼도 치명적이지 않다.
func (h *Hub) Publish(userID int64, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[userID] {
		select {
		case ch <- ev:
		default:
		}
	}
}
