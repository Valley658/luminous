package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"pastellive/internal/httputil"
)

type slidingCounter struct {
	mu   sync.Mutex
	hits []time.Time
}

func (s *slidingCounter) hit(now time.Time, window time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-window)
	kept := s.hits[:0]
	for _, t := range s.hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	s.hits = kept
	return len(kept)
}

func (s *slidingCounter) empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hits) == 0
}

func (s *slidingCounter) countSince(now time.Time, window time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-window)
	n := 0
	for _, t := range s.hits {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

type RateLimiter struct {
	enabled     bool
	window      time.Duration
	maxRequests int
	banFor      time.Duration

	mu          sync.Mutex
	counters    map[string]*slidingCounter
	bannedUntil map[string]time.Time
	lastSweep   time.Time
}

func NewRateLimiter(enabled bool, windowSec, maxRequests, banMinutes int) *RateLimiter {
	return &RateLimiter{
		enabled:     enabled,
		window:      time.Duration(windowSec) * time.Second,
		maxRequests: maxRequests,
		banFor:      time.Duration(banMinutes) * time.Minute,
		counters:    make(map[string]*slidingCounter),
		bannedUntil: make(map[string]time.Time),
		lastSweep:   time.Now(),
	}
}

func (rl *RateLimiter) isBanned(ip string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	until, ok := rl.bannedUntil[ip]
	if !ok {
		return false
	}
	if now.After(until) {
		delete(rl.bannedUntil, ip)
		return false
	}
	return true
}

func (rl *RateLimiter) ban(ip string, now time.Time) {
	rl.mu.Lock()
	rl.bannedUntil[ip] = now.Add(rl.banFor)
	rl.mu.Unlock()
}

func (rl *RateLimiter) counterFor(ip string, now time.Time) *slidingCounter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if now.Sub(rl.lastSweep) > 5*time.Minute {
		for k, c := range rl.counters {
			if c.empty() {
				delete(rl.counters, k)
			}
		}
		rl.lastSweep = now
	}
	c, ok := rl.counters[ip]
	if !ok {
		c = &slidingCounter{}
		rl.counters[ip] = c
	}
	return c
}

type IPStat struct {
	IP               string    `json:"ip"`
	RequestsInWindow int       `json:"requests_in_window"`
	Banned           bool      `json:"banned"`
	BannedUntil      time.Time `json:"banned_until,omitempty"`
}

func (rl *RateLimiter) Snapshot() []IPStat {
	rl.mu.Lock()
	counters := make(map[string]*slidingCounter, len(rl.counters))
	for ip, c := range rl.counters {
		counters[ip] = c
	}
	banned := make(map[string]time.Time, len(rl.bannedUntil))
	for ip, until := range rl.bannedUntil {
		banned[ip] = until
	}
	rl.mu.Unlock()

	now := time.Now()
	out := make([]IPStat, 0, len(counters)+len(banned))
	seen := make(map[string]bool, len(counters))
	for ip, c := range counters {
		count := c.countSince(now, rl.window)
		until, isBanned := banned[ip]
		isBanned = isBanned && now.Before(until)
		if count == 0 && !isBanned {
			continue
		}
		stat := IPStat{IP: ip, RequestsInWindow: count}
		if isBanned {
			stat.Banned = true
			stat.BannedUntil = until
		}
		out = append(out, stat)
		seen[ip] = true
	}

	for ip, until := range banned {
		if seen[ip] || !now.Before(until) {
			continue
		}
		out = append(out, IPStat{IP: ip, Banned: true, BannedUntil: until})
	}
	return out
}

func (rl *RateLimiter) Config() (enabled bool, windowSec, maxRequests, banMinutes int) {
	return rl.enabled, int(rl.window.Seconds()), rl.maxRequests, int(rl.banFor.Minutes())
}

func writeRateLimited(w http.ResponseWriter, retryAfterSec int) {
	if retryAfterSec <= 0 {
		retryAfterSec = 30
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSec))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": "요청이 너무 많습니다. 잠시 후 다시 시도해주세요.",
	})
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.enabled || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := httputil.GetClientIP(r)
		if ip == "" || ip == "unknown" {
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now()
		if rl.isBanned(ip, now) {
			writeRateLimited(w, int(rl.banFor.Seconds()))
			return
		}
		count := rl.counterFor(ip, now).hit(now, rl.window)
		if count > rl.maxRequests {
			rl.ban(ip, now)
			log.Printf("[보안] 비정상 트래픽 감지 - IP 임시 차단(%d분): ip=%s count=%d window=%v path=%s",
				int(rl.banFor.Minutes()), ip, count, rl.window, r.URL.Path)
			writeRateLimited(w, int(rl.banFor.Seconds()))
			return
		}
		next.ServeHTTP(w, r)
	})
}
