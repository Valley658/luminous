package video

import "testing"

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
