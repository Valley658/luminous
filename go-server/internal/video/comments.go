package video

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

type YouTubeComment struct {
	Nickname  string `json:"nickname"`
	Content   string `json:"content"`
	Date      string `json:"date"`
	LikeCount int64  `json:"like_count"`
}

type YouTubeCommentsResult struct {
	Success       bool             `json:"success"`
	Comments      []YouTubeComment `json:"comments"`
	NextPageToken string           `json:"nextPageToken,omitempty"`
	Message       string           `json:"message,omitempty"`
}

type ytCommentThreadsResponse struct {
	Items []struct {
		Snippet struct {
			TopLevelComment struct {
				Snippet struct {
					AuthorDisplayName string `json:"authorDisplayName"`
					TextDisplay       string `json:"textDisplay"`
					PublishedAt       string `json:"publishedAt"`
					LikeCount         int64  `json:"likeCount"`
				} `json:"snippet"`
			} `json:"topLevelComment"`
		} `json:"snippet"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
	Error         *struct {
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

var elevenCharIDPattern = regexp.MustCompile(`[a-zA-Z0-9_-]{11}`)

func FetchYouTubeComments(ctx context.Context, videoID, pageToken, apiKey string, maxResults int) YouTubeCommentsResult {
	if apiKey == "" {
		return YouTubeCommentsResult{Success: false, Comments: []YouTubeComment{}, Message: "API_KEY 누락"}
	}
	targetVideoID := strings.TrimSpace(videoID)
	if len(targetVideoID) > 11 {
		if m := elevenCharIDPattern.FindString(targetVideoID); m != "" {
			targetVideoID = m
		}
	}
	if maxResults <= 0 {
		maxResults = 30
	}

	url := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/commentThreads?part=snippet&videoId=%s&maxResults=%d&order=relevance&textFormat=plainText&key=%s",
		targetVideoID, maxResults, apiKey,
	)
	if pageToken != "" {
		url += "&pageToken=" + pageToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return YouTubeCommentsResult{Success: false, Comments: []YouTubeComment{}, Message: "댓글을 불러오지 못했습니다."}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return YouTubeCommentsResult{Success: false, Comments: []YouTubeComment{}, Message: "댓글을 불러오지 못했습니다."}
	}
	defer resp.Body.Close()

	var body ytCommentThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return YouTubeCommentsResult{Success: false, Comments: []YouTubeComment{}, Message: "댓글을 불러오지 못했습니다."}
	}
	if resp.StatusCode != http.StatusOK || body.Error != nil {
		reason := ""
		if body.Error != nil && len(body.Error.Errors) > 0 {
			reason = body.Error.Errors[0].Reason
		}
		if reason == "commentsDisabled" || reason == "videoNotFound" {
			return YouTubeCommentsResult{Success: false, Comments: []YouTubeComment{}, Message: "댓글을 사용할 수 없는 영상입니다."}
		}
		return YouTubeCommentsResult{Success: false, Comments: []YouTubeComment{}, Message: "댓글을 불러오지 못했습니다."}
	}

	comments := make([]YouTubeComment, 0, len(body.Items))
	for _, item := range body.Items {
		s := item.Snippet.TopLevelComment.Snippet
		nickname := s.AuthorDisplayName
		if nickname == "" {
			nickname = "유튜브 이용자"
		}
		date := "방금 전"
		if strings.Contains(s.PublishedAt, "T") {
			d := strings.Replace(s.PublishedAt, "T", " ", 1)
			if len(d) > 16 {
				d = d[:16]
			}
			date = d
		}
		comments = append(comments, YouTubeComment{Nickname: nickname, Content: s.TextDisplay, Date: date, LikeCount: s.LikeCount})
	}

	sort.SliceStable(comments, func(i, j int) bool { return comments[i].LikeCount > comments[j].LikeCount })
	return YouTubeCommentsResult{Success: true, Comments: comments, NextPageToken: body.NextPageToken}
}
