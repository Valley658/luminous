package video

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type ytThumbnail struct {
	URL string `json:"url"`
}

type ytSnippet struct {
	Title       string                 `json:"title"`
	Thumbnails  map[string]ytThumbnail `json:"thumbnails"`
	PublishedAt string                 `json:"publishedAt"`
}

type ytContentDetails struct {
	VideoID          string `json:"videoId"`
	VideoPublishedAt string `json:"videoPublishedAt"`
}

type ytPlaylistItem struct {
	Snippet        ytSnippet        `json:"snippet"`
	ContentDetails ytContentDetails `json:"contentDetails"`
}

type ytPlaylistItemsResponse struct {
	Items         []ytPlaylistItem `json:"items"`
	NextPageToken string           `json:"nextPageToken"`
	Error         *struct {
		Message string `json:"message"`
		Errors  []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

type quotaExceededError struct{ msg string }

func (e *quotaExceededError) Error() string { return e.msg }

var quotaReasons = map[string]bool{"quotaExceeded": true, "dailyLimitExceeded": true, "userRateLimitExceeded": true}

func pickThumbnail(thumbs map[string]ytThumbnail) string {
	for _, size := range []string{"high", "medium", "standard", "maxres", "default"} {
		if t, ok := thumbs[size]; ok && t.URL != "" {
			return t.URL
		}
	}
	return ""
}

func fetchPlaylistItemsPage(ctx context.Context, playlistID, pageToken, apiKey string) (*ytPlaylistItemsResponse, error) {
	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/playlistItems?part=snippet,contentDetails&playlistId=%s&maxResults=50&key=%s", playlistID, apiKey)
	if pageToken != "" {
		url += "&pageToken=" + pageToken
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out ytPlaylistItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		msg := ""
		if out.Error != nil {
			msg = out.Error.Message
			if resp.StatusCode == 403 {
				for _, e := range out.Error.Errors {
					if quotaReasons[e.Reason] {
						return nil, &quotaExceededError{msg: msg}
					}
				}
			}
		}
		return nil, fmt.Errorf("youtube API status %d: %s", resp.StatusCode, msg)
	}
	return &out, nil
}

func FetchPlaylistOrChannel(ctx context.Context, targetID string, isPlaylist, forceShort bool, memberName, apiKey string, extraPages int) []Video {
	rssVideos := FetchRSSVideosCtx(ctx, targetID, isPlaylist, forceShort)
	allVideos := make([]Video, len(rssVideos))
	copy(allVideos, rssVideos)
	seen := make(map[string]bool, len(rssVideos))
	for i := range allVideos {
		seen[allVideos[i].VideoID] = true
		if memberName != "" {
			allVideos[i].MemberName = memberName
		}
	}

	if apiKey == "" {
		return allVideos
	}

	var playlistID string
	if isPlaylist {
		playlistID = targetID
	} else if strings.HasPrefix(targetID, "UC") {
		playlistID = "UU" + targetID[2:]
	}
	if playlistID == "" {
		return allVideos
	}

	pageToken := ""
	for i := 0; i < extraPages; i++ {
		res, err := fetchPlaylistItemsPage(ctx, playlistID, pageToken, apiKey)
		if err != nil {
			if _, ok := err.(*quotaExceededError); ok {
				MarkQuotaExceeded()
			}
			if len(allVideos) == 0 {
				log.Printf("영상 풀 갱신: RSS와 Data API 모두 실패 (target=%s) - %v", targetID, err)
			}
			break
		}
		ClearQuotaExceeded()
		for _, item := range res.Items {
			title := item.Snippet.Title
			if title == "" {
				title = "제목 없음"
			}
			if bannedTitles[strings.ToLower(title)] {
				continue
			}
			vid := item.ContentDetails.VideoID
			if vid == "" || seen[vid] {
				continue
			}
			lower := strings.ToLower(title)
			isShort := forceShort || strings.Contains(lower, "#shorts") || strings.Contains(lower, "shorts") || strings.Contains(title, "쇼츠")
			seen[vid] = true
			allVideos = append(allVideos, Video{
				Title: title, Thumbnail: pickThumbnail(item.Snippet.Thumbnails),
				VideoID: vid, ID: vid, IsShort: isShort, MemberName: memberName,
			})
		}
		pageToken = res.NextPageToken
		if pageToken == "" {
			break
		}
	}

	if len(allVideos) > len(rssVideos) {
		log.Printf("영상 풀 갱신: target=%s RSS %d개 + Data API 추가 %d개 = 총 %d개", targetID, len(rssVideos), len(allVideos)-len(rssVideos), len(allVideos))
	}
	return allVideos
}

type ytVideoStatsResponse struct {
	Items []struct {
		ID         string `json:"id"`
		Statistics struct {
			ViewCount string `json:"viewCount"`
		} `json:"statistics"`
	} `json:"items"`
}

func fetchViewCounts(ctx context.Context, videoIDs []string, apiKey string) map[string]int64 {
	out := make(map[string]int64, len(videoIDs))
	if len(videoIDs) == 0 {
		return out
	}
	url := "https://www.googleapis.com/youtube/v3/videos?part=statistics&id=" + strings.Join(videoIDs, ",") + "&key=" + apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out
	}
	var body ytVideoStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return out
	}
	for _, item := range body.Items {
		if item.Statistics.ViewCount == "" {
			continue
		}
		if n, err := strconv.ParseInt(item.Statistics.ViewCount, 10, 64); err == nil {
			out[item.ID] = n
		}
	}
	return out
}

func FetchLatestVideosPage(ctx context.Context, targetID string, isPlaylist bool, pageToken, apiKey string) (videos []Video, nextPageToken string) {
	if !isPlaylist && !strings.HasPrefix(targetID, "UC") {
		return nil, ""
	}
	if apiKey == "" {
		if pageToken == "" {
			return FetchRSSVideosCtx(ctx, targetID, isPlaylist, false), ""
		}
		return nil, ""
	}

	playlistID := targetID
	if !isPlaylist {
		playlistID = "UU" + targetID[2:]
	}
	res, err := fetchPlaylistItemsPage(ctx, playlistID, pageToken, apiKey)
	if err != nil {
		if _, ok := err.(*quotaExceededError); ok {
			MarkQuotaExceeded()
		}
		if pageToken == "" {
			return FetchRSSVideosCtx(ctx, targetID, isPlaylist, false), ""
		}
		return nil, ""
	}
	ClearQuotaExceeded()

	if len(res.Items) == 0 {
		if pageToken == "" {
			return FetchRSSVideosCtx(ctx, targetID, isPlaylist, false), ""
		}
		return nil, res.NextPageToken
	}

	statIDs := make([]string, 0, len(res.Items))
	for _, item := range res.Items {
		if item.ContentDetails.VideoID != "" {
			statIDs = append(statIDs, item.ContentDetails.VideoID)
		}
	}
	viewCounts := fetchViewCounts(ctx, statIDs, apiKey)

	for _, item := range res.Items {
		title := item.Snippet.Title
		if title == "" {
			title = "제목 없음"
		}
		if bannedTitles[strings.ToLower(title)] {
			continue
		}
		vid := item.ContentDetails.VideoID
		if vid == "" {
			continue
		}
		publishedAt := item.Snippet.PublishedAt
		if publishedAt == "" {
			publishedAt = item.ContentDetails.VideoPublishedAt
		}
		videos = append(videos, Video{
			Title: title, Thumbnail: pickThumbnail(item.Snippet.Thumbnails),
			VideoID: vid, ID: vid, PublishedAt: publishedAt, ViewCount: viewCounts[vid],
		})
	}
	return videos, res.NextPageToken
}

func FetchArchivePage(ctx context.Context, channelID, pageToken, apiKey string) (videos []Video, nextPageToken string, err error) {
	if !strings.HasPrefix(channelID, "UC") {
		return nil, "", nil
	}
	if apiKey == "" {
		if pageToken != "" {
			return nil, "", nil
		}
		return FetchRSSVideosCtx(ctx, channelID, false, false), "", nil
	}
	playlistID := "UU" + channelID[2:]
	res, err := fetchPlaylistItemsPage(ctx, playlistID, pageToken, apiKey)
	if err != nil {
		return nil, "", err
	}
	for _, item := range res.Items {
		title := item.Snippet.Title
		if title == "" {
			title = "제목 없음"
		}
		if bannedTitles[strings.ToLower(title)] {
			continue
		}
		vid := item.ContentDetails.VideoID
		if vid == "" {
			continue
		}
		lower := strings.ToLower(title)
		isShort := strings.Contains(lower, "#shorts") || strings.Contains(lower, "shorts") || strings.Contains(title, "쇼츠")
		videos = append(videos, Video{
			Title: title, Thumbnail: pickThumbnail(item.Snippet.Thumbnails),
			VideoID: vid, ID: vid, IsShort: isShort,
		})
	}
	return videos, res.NextPageToken, nil
}
