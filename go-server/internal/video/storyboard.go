package video

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type StoryboardResult struct {
	Success     bool   `json:"success"`
	SheetURL    string `json:"sheet_url,omitempty"`
	Cols        int    `json:"cols,omitempty"`
	Rows        int    `json:"rows,omitempty"`
	FrameWidth  int    `json:"frame_width,omitempty"`
	FrameHeight int    `json:"frame_height,omitempty"`
	FrameCount  int    `json:"frame_count,omitempty"`
	IntervalMs  int    `json:"interval_ms,omitempty"`
}

var ytInitialPlayerResponsePattern = regexp.MustCompile(`ytInitialPlayerResponse\s*=\s*(\{.*?\})\s*;`)

type ytPlayerResponseStoryboards struct {
	Storyboards struct {
		PlayerStoryboardSpecRenderer struct {
			Spec string `json:"spec"`
		} `json:"playerStoryboardSpecRenderer"`
	} `json:"storyboards"`
}

func FetchVideoStoryboard(ctx context.Context, videoID string) (StoryboardResult, int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.youtube.com/watch?v="+videoID, nil)
	if err != nil {
		return StoryboardResult{Success: false}, http.StatusBadGateway
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en;q=0.8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return StoryboardResult{Success: false}, http.StatusBadGateway
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil && len(buf) == 0 {
		return StoryboardResult{Success: false}, http.StatusBadGateway
	}
	html := string(buf)

	m := ytInitialPlayerResponsePattern.FindStringSubmatch(html)
	if m == nil {
		return StoryboardResult{Success: false}, http.StatusNotFound
	}
	var parsed ytPlayerResponseStoryboards
	if err := json.Unmarshal([]byte(m[1]), &parsed); err != nil {
		return StoryboardResult{Success: false}, http.StatusNotFound
	}
	spec := parsed.Storyboards.PlayerStoryboardSpecRenderer.Spec
	if spec == "" {
		return StoryboardResult{Success: false}, http.StatusNotFound
	}

	parts := strings.Split(spec, "|")
	if len(parts) < 2 {
		return StoryboardResult{Success: false}, http.StatusNotFound
	}
	baseURL := parts[0]

	type level struct {
		width, height, totalFrames, cols, rows, interval int
	}
	var levels []level
	for _, seg := range parts[1:] {
		f := strings.Split(seg, "#")
		if len(f) < 8 {
			continue
		}
		width, e1 := strconv.Atoi(f[0])
		height, e2 := strconv.Atoi(f[1])
		totalFrames, e3 := strconv.Atoi(f[2])
		cols, e4 := strconv.Atoi(f[3])
		rows, e5 := strconv.Atoi(f[4])
		interval, e6 := strconv.Atoi(f[5])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
			continue
		}
		levels = append(levels, level{width, height, totalFrames, cols, rows, interval})
	}
	if len(levels) == 0 {
		return StoryboardResult{Success: false}, http.StatusNotFound
	}

	lvl := levels[0]
	perSheet := lvl.cols * lvl.rows
	frameCount := lvl.totalFrames
	if perSheet > 0 && frameCount > perSheet {
		frameCount = perSheet
	}
	if perSheet <= 0 {
		frameCount = 0
	}
	if frameCount <= 0 {
		return StoryboardResult{Success: false}, http.StatusNotFound
	}

	sheetURL := strings.NewReplacer("$L", "0", "$N", "0").Replace(baseURL)
	intervalMs := lvl.interval
	if intervalMs == 0 {
		intervalMs = 2000
	}
	return StoryboardResult{
		Success: true, SheetURL: sheetURL, Cols: lvl.cols, Rows: lvl.rows,
		FrameWidth: lvl.width, FrameHeight: lvl.height, FrameCount: frameCount, IntervalMs: intervalMs,
	}, http.StatusOK
}
