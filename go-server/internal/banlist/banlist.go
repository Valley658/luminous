// Package banlist: 관리자가 수동으로 차단한 IP를 매 요청마다 빠르게(DB 쿼리
// 없이) 확인하기 위한 메모리 캐시. models.BanIP/UnbanIP가 실제 저장소(DB)고,
// 이 List는 서버가 켜져 있는 동안 그 내용을 메모리에 복제해둔 것 - 서버
// 시작 시 한 번 로드하고, 관리자가 차단/해제할 때마다 즉시 갱신한다.
package banlist

import (
	"sync"

	pdb "pastellive/internal/db"
	"pastellive/internal/models"
)

type List struct {
	mu  sync.RWMutex
	ips map[string]bool
}

func New() *List {
	return &List{ips: make(map[string]bool)}
}

// Load: 서버 시작 시 DB에 저장된 차단 목록을 메모리로 불러온다.
func (l *List) Load(d *pdb.DB) error {
	banned, err := models.ListBannedIPs(d)
	if err != nil {
		return err
	}
	m := make(map[string]bool, len(banned))
	for _, b := range banned {
		m[b.IP] = true
	}
	l.mu.Lock()
	l.ips = m
	l.mu.Unlock()
	return nil
}

func (l *List) IsBanned(ip string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.ips[ip]
}

// Ban: DB에 기록하고 메모리에도 즉시 반영한다.
func (l *List) Ban(d *pdb.DB, ip, reason string) error {
	if err := models.BanIP(d, ip, reason); err != nil {
		return err
	}
	l.mu.Lock()
	l.ips[ip] = true
	l.mu.Unlock()
	return nil
}

func (l *List) Unban(d *pdb.DB, ip string) error {
	if err := models.UnbanIP(d, ip); err != nil {
		return err
	}
	l.mu.Lock()
	delete(l.ips, ip)
	l.mu.Unlock()
	return nil
}
