package video

import (
	"log"
	"sync"
	"time"
)

var quotaState = struct {
	mu         sync.Mutex
	exceeded   bool
	detectedAt time.Time
}{}

func MarkQuotaExceeded() {
	quotaState.mu.Lock()
	already := quotaState.exceeded
	quotaState.exceeded = true
	quotaState.detectedAt = time.Now()
	quotaState.mu.Unlock()
	if !already {
		log.Println("[유튜브 할당량] quotaExceeded 감지 - 안내 문구 표시 시작")
	}
}

func ClearQuotaExceeded() {
	quotaState.mu.Lock()
	was := quotaState.exceeded
	quotaState.exceeded = false
	quotaState.mu.Unlock()
	if was {
		log.Println("[유튜브 할당량] API 호출 성공 - 소진 상태 해제")
	}
}

func QuotaResetETA() (resetAtKSTLabel string, remainingSeconds int64) {
	pacific, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		pacific = time.FixedZone("PDT", -7*3600)
	}
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		kst = time.FixedZone("KST", 9*3600)
	}
	nowPacific := time.Now().In(pacific)
	y, m, d := nowPacific.Date()
	nextResetPacific := time.Date(y, m, d, 0, 0, 0, 0, pacific).AddDate(0, 0, 1)
	if nowPacific.Hour() == 0 && nowPacific.Minute() == 0 && nowPacific.Second() == 0 {
		nextResetPacific = nowPacific
	}
	nextResetKST := nextResetPacific.In(kst)
	remaining := int64(nextResetPacific.Sub(nowPacific).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	return nextResetKST.Format("15시 04분"), remaining
}

func QuotaIsExceeded() bool {
	quotaState.mu.Lock()
	exceeded := quotaState.exceeded
	quotaState.mu.Unlock()
	if !exceeded {
		return false
	}
	_, remaining := QuotaResetETA()
	if remaining <= 0 {
		ClearQuotaExceeded()
		return false
	}
	return true
}
