package cache

import (
	"sync"
	"time"
)

type entry struct {
	value   any
	savedAt time.Time
	ttl     time.Duration
}

type Store struct {
	mu   sync.Mutex
	data map[string]entry
}

func New() *Store {
	return &Store{data: make(map[string]entry)}
}

const DefaultTTL = 5 * time.Minute

func (s *Store) Get(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		return nil, false
	}
	if e.ttl > 0 && time.Since(e.savedAt) > e.ttl {
		delete(s.data, key)
		return nil, false
	}
	return e.value, true
}

func (s *Store) Set(key string, value any, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = entry{value: value, savedAt: time.Now(), ttl: ttl}
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}
