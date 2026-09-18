package meilisearch

import "testing"

func TestExtractChoseong(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single word", "히나", "ㅎㄴ"},
		{"two words - per-word plus combined", "히나 채널", "ㅎㄴ ㅊㄴ ㅎㄴㅊㄴ"},
		{"no korean text", "hello world", ""},
		{"mixed korean/latin in one word", "abc김", "ㄱ"},
		{"empty input", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExtractChoseong(c.in); got != c.want {
				t.Errorf("ExtractChoseong(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
