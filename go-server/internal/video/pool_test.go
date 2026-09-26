package video

import (
	"path/filepath"
	"testing"
)

// TestPoolDiskCache_SurvivesRestart: 마지막으로 성공한 풀을 디스크에 저장해두면,
// (서버 재시작을 흉내낸) 새 Pool이 그 파일에서 즉시 채워져서 첫 방문자부터
// 빈 목록을 보지 않아야 한다 - 유튜브 API 할당량이 소진된 채로 재시작해도
// "시간이 지나도 아무것도 안 보이는" 상태가 되지 않게 하는 게 목적.
func TestPoolDiskCache_SurvivesRestart(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "video_pool_cache.json")

	p1 := NewPool(cachePath)
	if !p1.IsEmpty() {
		t.Fatal("새로 만든 풀은 캐시 파일이 없으면 비어있어야 함")
	}
	want := []Video{
		{Title: "영상 1", VideoID: "abc123"},
		{Title: "영상 2", VideoID: "def456"},
	}
	p1.set(want)

	// "서버 재시작" 흉내: 같은 cachePath로 완전히 새 Pool을 만든다.
	p2 := NewPool(cachePath)
	got := p2.Get()
	if len(got) != len(want) {
		t.Fatalf("재시작 후 캐시에서 복원된 영상 개수 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].VideoID != want[i].VideoID {
			t.Errorf("영상[%d].VideoID = %q, want %q", i, got[i].VideoID, want[i].VideoID)
		}
	}
}

func TestPoolDiskCache_EmptyPathDisablesCache(t *testing.T) {
	p := NewPool("")
	p.set([]Video{{Title: "영상", VideoID: "x"}})
	// cachePath가 빈 문자열이면 persist가 아무 파일도 만들지 않아야 하고,
	// 이게 패닉/에러 없이 조용히 넘어가야 한다 (여기서 확인하는 건 그것뿐).
	if p.IsEmpty() {
		t.Fatal("set() 직후에는 비어있으면 안 됨")
	}
}

func TestIsShortTitle(t *testing.T) {
	cases := []struct {
		name string
		v    Video
		want bool
	}{
		{"explicit IsShort flag", Video{Title: "아무 제목", IsShort: true}, true},
		{"#shorts hashtag", Video{Title: "오늘의 하이라이트 #shorts"}, true},
		{"shorts word, mixed case", Video{Title: "Weekly Shorts Compilation"}, true},
		{"korean 쇼츠", Video{Title: "이번주 쇼츠 모음"}, true},
		{"plain long-form title", Video{Title: "이번주 방송 다시보기"}, false},
		{"empty title, no flag", Video{Title: ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsShortTitle(c.v); got != c.want {
				t.Errorf("IsShortTitle(%+v) = %v, want %v", c.v, got, c.want)
			}
		})
	}
}

func TestCountLongVideos(t *testing.T) {
	videos := []Video{
		{Title: "롱폼 1"},
		{Title: "쇼츠 #shorts"},
		{Title: "롱폼 2"},
		{Title: "쇼츠3", IsShort: true},
	}
	if got := countLongVideos(videos); got != 2 {
		t.Errorf("countLongVideos = %d, want 2", got)
	}
	if got := countLongVideos(nil); got != 0 {
		t.Errorf("countLongVideos(nil) = %d, want 0", got)
	}
}

func TestShouldSkipPoolRefresh(t *testing.T) {
	longVideo := Video{Title: "롱폼 영상"}
	shortVideo := Video{Title: "쇼츠 #shorts"}

	cases := []struct {
		name          string
		oldPool       []Video
		newPool       []Video
		quotaExceeded bool
		wantSkip      bool
	}{
		{
			name:          "quota exceeded and an existing pool - keep old pool (today's live bug)",
			oldPool:       []Video{longVideo, shortVideo},
			newPool:       []Video{shortVideo, shortVideo},
			quotaExceeded: true,
			wantSkip:      true,
		},
		{
			name:          "quota exceeded but no existing pool yet - accept whatever we got (cold start)",
			oldPool:       nil,
			newPool:       []Video{shortVideo},
			quotaExceeded: true,
			wantSkip:      false,
		},
		{
			name:          "quota fine, new pool has zero long videos but old pool had some - keep old",
			oldPool:       []Video{longVideo},
			newPool:       []Video{shortVideo, shortVideo},
			quotaExceeded: false,
			wantSkip:      true,
		},
		{
			name:          "quota fine, new pool has zero long videos and old pool never had any either - accept",
			oldPool:       []Video{shortVideo},
			newPool:       []Video{shortVideo},
			quotaExceeded: false,
			wantSkip:      false,
		},
		{
			name:          "healthy refresh - accept",
			oldPool:       []Video{longVideo},
			newPool:       []Video{longVideo, shortVideo},
			quotaExceeded: false,
			wantSkip:      false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skip, reason := shouldSkipPoolRefresh(c.oldPool, c.newPool, c.quotaExceeded)
			if skip != c.wantSkip {
				t.Errorf("shouldSkipPoolRefresh() skip = %v (reason=%q), want skip = %v", skip, reason, c.wantSkip)
			}
			if skip && reason == "" {
				t.Errorf("shouldSkipPoolRefresh() returned skip=true with no reason")
			}
		})
	}
}
