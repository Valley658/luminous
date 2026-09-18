package handlers

import "testing"

func TestIsJunkSearchTerm(t *testing.T) {
	cases := []struct {
		term string
		want bool
	}{
		{"히나", false},
		{"김블루 채널", false},
		{"1기생 라이브", false},
		{"' OR '1'='1", true},
		{"1 OR 1=1", true},
		{"<script>alert(1)</script>", true},
		{"UNION SELECT password FROM users", true},
		{"robert'; DROP TABLE members;--", true},
		{"정상적인 검색어입니다", false},
	}
	for _, c := range cases {
		t.Run(c.term, func(t *testing.T) {
			if got := isJunkSearchTerm(c.term); got != c.want {
				t.Errorf("isJunkSearchTerm(%q) = %v, want %v", c.term, got, c.want)
			}
		})
	}
}

func TestMaxSuggestConstants(t *testing.T) {
	// maxSuggestVideos가 maxSuggestTotal을 넘으면 ApiSearchSuggestHandler의 비디오
	// 우선순위 로직이 무의미해지므로, 둘 사이의 관계가 바뀌면 여기서 바로 드러나야 한다.
	if maxSuggestVideos > maxSuggestTotal {
		t.Errorf("maxSuggestVideos (%d) > maxSuggestTotal (%d)", maxSuggestVideos, maxSuggestTotal)
	}
}
