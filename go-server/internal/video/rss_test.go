package video

import "testing"

const sampleFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
  <entry>
    <yt:videoId>abc123</yt:videoId>
    <title>일반 영상 제목</title>
    <published>2026-01-01T00:00:00+00:00</published>
    <media:group>
      <media:title>일반 영상 제목</media:title>
      <media:thumbnail url="https://img.example/abc123.jpg"/>
      <media:community>
        <media:statistics views="1234"/>
      </media:community>
    </media:group>
  </entry>
  <entry>
    <yt:videoId>def456</yt:videoId>
    <title>쇼츠 영상 #shorts</title>
    <published>2026-01-02T00:00:00+00:00</published>
    <media:group>
      <media:title>쇼츠 영상 #shorts</media:title>
    </media:group>
  </entry>
  <entry>
    <yt:videoId>ghi789</yt:videoId>
    <title>Private video</title>
  </entry>
  <entry>
    <yt:videoId></yt:videoId>
    <title>videoId가 없는 항목 (걸러져야 함)</title>
  </entry>
</feed>`

func TestParseRSSFeed(t *testing.T) {
	videos := parseRSSFeed([]byte(sampleFeedXML), false)

	// 4th entry has no videoId, 3rd is a banned title - both should be dropped.
	if len(videos) != 2 {
		t.Fatalf("parseRSSFeed() returned %d videos, want 2 (got %+v)", len(videos), videos)
	}

	first := videos[0]
	if first.VideoID != "abc123" {
		t.Errorf("videos[0].VideoID = %q, want abc123", first.VideoID)
	}
	if first.Title != "일반 영상 제목" {
		t.Errorf("videos[0].Title = %q, want 일반 영상 제목", first.Title)
	}
	if first.Thumbnail != "https://img.example/abc123.jpg" {
		t.Errorf("videos[0].Thumbnail = %q", first.Thumbnail)
	}
	if first.ViewCount != 1234 {
		t.Errorf("videos[0].ViewCount = %d, want 1234", first.ViewCount)
	}
	if first.IsShort {
		t.Errorf("videos[0].IsShort = true, want false")
	}

	second := videos[1]
	if second.VideoID != "def456" {
		t.Errorf("videos[1].VideoID = %q, want def456", second.VideoID)
	}
	if !second.IsShort {
		t.Errorf("videos[1].IsShort = false, want true (title has #shorts)")
	}
}

func TestParseRSSFeedForceShort(t *testing.T) {
	videos := parseRSSFeed([]byte(sampleFeedXML), true)
	if len(videos) != 2 {
		t.Fatalf("parseRSSFeed() returned %d videos, want 2", len(videos))
	}
	for _, v := range videos {
		if !v.IsShort {
			t.Errorf("video %q: IsShort = false, want true (forceShort=true)", v.VideoID)
		}
	}
}

func TestParseRSSFeedInvalidXML(t *testing.T) {
	if got := parseRSSFeed([]byte("not xml at all"), false); got != nil {
		t.Errorf("parseRSSFeed(invalid) = %+v, want nil", got)
	}
}
