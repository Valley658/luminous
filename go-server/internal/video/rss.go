package video

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type rssFeed struct {
	XMLName xml.Name   `xml:"http://www.w3.org/2005/Atom feed"`
	Entries []rssEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

type rssEntry struct {
	VideoID   string        `xml:"http://www.youtube.com/xml/schemas/2015 videoId"`
	Title     string        `xml:"http://www.w3.org/2005/Atom title"`
	Published string        `xml:"http://www.w3.org/2005/Atom published"`
	Group     rssMediaGroup `xml:"http://search.yahoo.com/mrss/ group"`
}

type rssMediaGroup struct {
	Title      string            `xml:"http://search.yahoo.com/mrss/ title"`
	Thumbnails []rssMediaThumb   `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	Community  rssMediaCommunity `xml:"http://search.yahoo.com/mrss/ community"`
}

type rssMediaThumb struct {
	URL string `xml:"url,attr"`
}

type rssMediaCommunity struct {
	Statistics rssMediaStatistics `xml:"http://search.yahoo.com/mrss/ statistics"`
}

type rssMediaStatistics struct {
	Views string `xml:"views,attr"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func rssURL(targetID string, isPlaylist bool) string {
	param := "channel_id"
	if isPlaylist {
		param = "playlist_id"
	}
	return fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?%s=%s", param, targetID)
}

func parseRSSFeed(body []byte, forceShort bool) []Video {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}
	videos := make([]Video, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		if e.VideoID == "" {
			continue
		}
		title := strings.TrimSpace(e.Group.Title)
		if title == "" {
			title = strings.TrimSpace(e.Title)
		}
		if title == "" {
			title = "제목 없음"
		}
		if bannedTitles[strings.ToLower(title)] {
			continue
		}
		thumbURL := ""
		if len(e.Group.Thumbnails) > 0 {
			thumbURL = e.Group.Thumbnails[0].URL
		}
		lower := strings.ToLower(title)
		isShort := forceShort || strings.Contains(lower, "#shorts") || strings.Contains(lower, "shorts") || strings.Contains(title, "쇼츠")
		viewCount := int64(0)
		if e.Group.Community.Statistics.Views != "" {
			if n, err := strconv.ParseInt(e.Group.Community.Statistics.Views, 10, 64); err == nil {
				viewCount = n
			}
		}
		videos = append(videos, Video{
			Title: title, Thumbnail: thumbURL, VideoID: e.VideoID, ID: e.VideoID,
			IsShort: isShort, IsDrive: false, PublishedAt: e.Published, ViewCount: viewCount,
		})
	}
	return videos
}

func FetchRSSVideosSync(targetID string, isPlaylist, forceShort bool) []Video {
	resp, err := httpClient.Get(rssURL(targetID, isPlaylist))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	return parseRSSFeed(body, forceShort)
}

// [2026-09-18: 예전엔 RSS 실패가 전부 조용히 nil을 반환해서, 유튜브 할당량
// 초과 상황이 터졌을 때 "RSS와 Data API 모두 실패"라는 상위 로그만 보이고
// RSS 쪽이 정확히 왜(네트워크 오류인지, 응답 코드가 뭔지, 타임아웃인지)
// 실패했는지 전혀 구분이 안 됐음. 풀이 빈 동안의 재시도엔 이제 쿨다운이
// 있어서 예전만큼 자주 호출되지 않으니, 실패 원인을 간단히 로그로 남겨서
// 다음에 이런 장애가 생기면 RSS 문제인지 API 할당량 문제인지 바로 구분할 수
// 있게 한다.]
func FetchRSSVideosCtx(ctx context.Context, targetID string, isPlaylist, forceShort bool) []Video {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rssURL(targetID, isPlaylist), nil)
	if err != nil {
		log.Printf("RSS 요청 생성 실패(target=%s): %v", targetID, err)
		return nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("RSS 요청 실패(target=%s): %v", targetID, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("RSS 응답 오류(target=%s): status=%d", targetID, resp.StatusCode)
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("RSS 응답 본문 읽기 실패(target=%s): %v", targetID, err)
		return nil
	}
	videos := parseRSSFeed(body, forceShort)
	if len(videos) == 0 {
		log.Printf("RSS 파싱 결과 0개(target=%s, 응답 길이=%d bytes)", targetID, len(body))
	}
	return videos
}
