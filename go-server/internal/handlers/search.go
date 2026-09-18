package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pastellive/internal/httputil"
	"pastellive/internal/meilisearch"
	"pastellive/internal/middleware"
	"pastellive/internal/models"
)

const maxSuggestVideos = 8
const maxSuggestTotal = 12

func (a *App) ApiSearchSuggestHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, map[string]any{"success": true, "suggestions": []any{}})
		return
	}
	qLower := strings.ToLower(query)

	var suggestions []map[string]any
	seenTitle := make(map[string]bool)
	seenVideoID := make(map[string]bool)

	addVideo := func(title, videoID, thumbnail string) {
		if title == "" || len(suggestions) >= maxSuggestVideos || seenTitle[title] {
			return
		}
		if videoID != "" && seenVideoID[videoID] {
			return
		}
		seenTitle[title] = true
		if videoID != "" {
			seenVideoID[videoID] = true
		}
		suggestions = append(suggestions, map[string]any{"type": "video", "text": title, "videoId": videoID, "thumbnail": thumbnail})
	}

	// 1순위: Meilisearch 색인 전체(라이브 풀 + 아카이브)에서 프리픽스/오타 허용 매칭.
	// 라이브 VideoPool 하나만 쓰면 할당량 소진 등으로 풀이 작을 때 자동완성이 빈약해 보이므로,
	// 색인된 전체 영상 데이터를 우선 사용한다.
	if a.Meili.Available() {
		if hits, err := a.Meili.Search(meiliIndexName, query, 30); err == nil {
			for _, h := range hits {
				if len(suggestions) >= maxSuggestVideos {
					break
				}
				if anyToStr(h["type"]) != "video" {
					continue
				}
				addVideo(anyToStr(h["title"]), anyToStr(h["videoId"]), anyToStr(h["thumbnail"]))
			}
		}
	}

	// 2순위: 라이브 VideoPool (Meili 미사용/결과 부족 시 보강)
	if len(suggestions) < maxSuggestVideos {
		a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
		for _, v := range a.VideoPool.Get() {
			if len(suggestions) >= maxSuggestVideos {
				break
			}
			if !strings.Contains(strings.ToLower(v.Title), qLower) {
				continue
			}
			addVideo(v.Title, v.VideoID, v.Thumbnail)
		}
	}

	// 3순위: DB 영상 아카이브 (역시 결과 부족 시 보강)
	if len(suggestions) < maxSuggestVideos {
		if archived, err := models.SearchArchiveVideos(a.DB, "%"+query+"%", maxSuggestVideos-len(suggestions)); err == nil {
			for _, av := range archived {
				if len(suggestions) >= maxSuggestVideos {
					break
				}
				addVideo(av.Title.String, av.VideoID, av.Thumbnail.String)
			}
		}
	}

	rows, err := a.DB.Query("SELECT member_name FROM members WHERE member_name LIKE ? LIMIT 5", "%"+query+"%")
	if err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil && name != "" && !seenTitle[name] {
				seenTitle[name] = true
				suggestions = append(suggestions, map[string]any{"type": "member", "text": name})
			}
		}
		rows.Close()
	}

	if len(suggestions) > maxSuggestTotal {
		suggestions = suggestions[:maxSuggestTotal]
	}
	if suggestions == nil {
		suggestions = []map[string]any{}
	}
	writeJSON(w, map[string]any{"success": true, "suggestions": suggestions})
}

func (a *App) ApiSearchLocalIndexHandler(w http.ResponseWriter, r *http.Request) {
	a.VideoPool.RefreshIfEmpty(r.Context(), a.DB, a.Cfg.YoutubeAPIKey)
	var items []map[string]any
	seen := make(map[string]bool)
	pool := a.VideoPool.Get()
	if len(pool) > 300 {
		pool = pool[:300]
	}
	for _, v := range pool {
		if v.Title != "" && !seen[v.Title] {
			seen[v.Title] = true
			items = append(items, map[string]any{"type": "video", "text": v.Title, "videoId": v.VideoID, "thumbnail": v.Thumbnail})
		}
	}
	rows, err := a.DB.Query("SELECT member_name FROM members")
	if err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil && name != "" && !seen[name] {
				seen[name] = true
				items = append(items, map[string]any{"type": "member", "text": name})
			}
		}
		rows.Close()
	}
	if items == nil {
		items = []map[string]any{}
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, map[string]any{"success": true, "items": items})
}

var junkSearchPattern = regexp.MustCompile(`(?i)['"<>;` + "`" + `]|--|/\*|\bunion\b|\bselect\b|\binsert\b|\bdelete\b|\bdrop\b|\bscript\b|\balert\(|\bOR\b\s*['"]?\s*\d`)

func isJunkSearchTerm(term string) bool {
	return junkSearchPattern.MatchString(term)
}

func (a *App) executeSearchCore(ctx context.Context, rawQuery string) map[string]any {
	query := strings.ToLower(rawQuery)
	cacheKey := "search_result_" + query
	if cached, ok := a.Cache.Get(cacheKey); ok {
		if m, ok := cached.(map[string]any); ok {
			return m
		}
	}

	a.VideoPool.RefreshIfEmpty(ctx, a.DB, a.Cfg.YoutubeAPIKey)
	pool := a.VideoPool.Get()

	var videoResults []map[string]any
	var communityResults []map[string]any
	var fanartResults []map[string]any
	meiliOK := false

	var memberAugment []map[string]any
	matchedMemberName, matchedChannelID, memberFound, _ := models.FindMemberByLowerName(a.DB, query)
	if memberFound {
		if archived, err := models.GetArchiveVideosByMember(a.DB, matchedMemberName); err == nil {
			for _, av := range archived {
				memberAugment = append(memberAugment, av.ToTemplateMap())
			}
		}
		if matchedChannelID != "" && matchedChannelID != "UC_KANNA_PLACEHOLDER" {

			go models.SyncMemberChannelArchive(context.Background(), a.DB, matchedMemberName, matchedChannelID, a.Cfg.YoutubeAPIKey, false, 400)
		}
	}

	if a.Meili.Available() {
		if hits, err := a.Meili.Search(meiliIndexName, rawQuery, 20000); err == nil {
			for _, h := range hits {
				switch anyToStr(h["type"]) {
				case "video":
					videoResults = append(videoResults, map[string]any{
						"title": h["title"], "thumbnail": h["thumbnail"], "videoId": h["videoId"], "id": h["videoId"],
						"is_short": h["is_short"],
					})
				case "fanart":
					fanartResults = append(fanartResults, map[string]any{
						"id": h["fanartId"], "title": h["title"], "description": h["description"],
						"image_url": h["image_url"], "thumbnail_url": h["thumbnail_url"], "nickname": h["nickname"],
					})
				case "community":
					communityResults = append(communityResults, map[string]any{
						"id": h["postId"], "member_name": h["member_name"], "author": h["author"],
						"content": h["content"], "date": h["date"],
					})
				}
			}
			meiliOK = true
		}
	}

	seenVideoIDs := make(map[string]bool)
	for _, v := range videoResults {
		if vid := anyToStr(v["videoId"]); vid != "" {
			seenVideoIDs[vid] = true
		}
	}

	if meiliOK {

		for _, v := range pool {
			if v.VideoID == "" || seenVideoIDs[v.VideoID] {
				continue
			}
			if strings.Contains(strings.ToLower(v.Title), query) || strings.Contains(strings.ToLower(v.MemberName), query) {
				seenVideoIDs[v.VideoID] = true
				videoResults = append(videoResults, v.ToTemplateMap())
			}
		}
		if archived, err := models.SearchArchiveVideos(a.DB, "%"+rawQuery+"%", 200); err == nil {
			for _, av := range archived {
				if seenVideoIDs[av.VideoID] {
					continue
				}
				seenVideoIDs[av.VideoID] = true
				videoResults = append(videoResults, av.ToTemplateMap())
			}
		}
	} else {
		videoResults = nil
		seenVideoIDs = make(map[string]bool)
		if archived, err := models.SearchArchiveVideos(a.DB, "%"+rawQuery+"%", 20000); err == nil {
			for _, av := range archived {
				if seenVideoIDs[av.VideoID] {
					continue
				}
				seenVideoIDs[av.VideoID] = true
				videoResults = append(videoResults, av.ToTemplateMap())
			}
		}
		for _, v := range pool {
			if v.VideoID == "" || seenVideoIDs[v.VideoID] {
				continue
			}
			if strings.Contains(strings.ToLower(v.Title), query) || strings.Contains(strings.ToLower(v.MemberName), query) {
				seenVideoIDs[v.VideoID] = true
				videoResults = append(videoResults, v.ToTemplateMap())
			}
		}

		if posts, err := models.SearchCommunityPosts(a.DB, rawQuery, 20); err == nil {
			for _, p := range posts {
				communityResults = append(communityResults, map[string]any{
					"id": p.ID, "member_name": p.MemberName.String, "author": p.Author.String,
					"content": p.Content.String, "date": p.Date,
				})
			}
		}
		if fanarts, err := models.SearchFanartGallery(a.DB, rawQuery, 20); err == nil {
			for _, f := range fanarts {
				fanartResults = append(fanartResults, map[string]any{
					"id": f.ID, "title": f.Title.String, "description": f.Description.String,
					"image_url": f.ImageURL.String, "thumbnail_url": f.ThumbnailURL.String, "nickname": f.Nickname.String,
				})
			}
		}
	}

	if len(memberAugment) > 0 {
		seen := make(map[string]bool)
		for _, v := range videoResults {
			if vid := anyToStr(v["videoId"]); vid != "" {
				seen[vid] = true
			}
		}
		for _, v := range memberAugment {
			vid := anyToStr(v["videoId"])
			if vid != "" && !seen[vid] {
				seen[vid] = true
				videoResults = append(videoResults, v)
			}
		}
	}

	likePatternUC := "%" + rawQuery + "%"
	seenUC := make(map[string]bool)
	for _, v := range videoResults {
		if vid := anyToStr(v["videoId"]); vid != "" {
			seenUC[vid] = true
		}
	}
	if userChanVideos, err := models.SearchUserChannelVideos(a.DB, likePatternUC, 200); err == nil {
		for _, v := range userChanVideos {
			if seenUC[v.VideoID] {
				continue
			}
			seenUC[v.VideoID] = true
			videoResults = append(videoResults, v.ToMap())
		}
	}

	var channelResults []map[string]any
	if channels, err := models.SearchChannels(a.DB, likePatternUC, 10); err == nil {
		for _, c := range channels {
			channelResults = append(channelResults, map[string]any{
				"channel_id": c.ChannelID, "nickname": c.Nickname, "picture": c.Picture,
				"description": c.Description, "video_count": c.VideoCount,
			})
		}
	}

	if videoResults == nil {
		videoResults = []map[string]any{}
	}
	if communityResults == nil {
		communityResults = []map[string]any{}
	}
	if fanartResults == nil {
		fanartResults = []map[string]any{}
	}
	if channelResults == nil {
		channelResults = []map[string]any{}
	}

	result := map[string]any{
		"success": true, "videos": videoResults, "community": communityResults,
		"fanarts": fanartResults, "channels": channelResults,
	}
	a.Cache.Set(cacheKey, result, 180*time.Second)
	return result
}

func anyToStr(v any) string {
	s, _ := v.(string)
	return s
}

func (a *App) ApiSearchHandler(w http.ResponseWriter, r *http.Request) {
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	if rawQuery == "" {
		writeJSON(w, map[string]any{"success": false, "message": "검색어를 입력해주세요."})
		return
	}
	if !isJunkSearchTerm(rawQuery) {
		go func() {
			_ = models.LogSearchTrend(a.DB, rawQuery)
		}()
	}
	result := a.executeSearchCore(r.Context(), rawQuery)
	writeJSON(w, result)
}

func (a *App) ApiTrendingSearchesHandler(w http.ResponseWriter, r *http.Request) {
	keywords, err := models.GetTrendingKeywords(a.DB, 5)
	if err != nil {
		writeJSON(w, map[string]any{"success": false, "trends": []any{}})
		return
	}
	writeJSON(w, map[string]any{"success": true, "trends": keywords})
}

func (a *App) ApiDeleteTrendingHandler(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	if sess == nil || sess.GetInt64("user_id") == 0 || !a.isAdmin(r) {
		httputil.JSONError(w, http.StatusForbidden, "권한이 없습니다.")
		return
	}
	var body struct {
		Keyword string `json:"keyword"`
	}
	_ = decodeJSONBody(r, &body)
	if body.Keyword == "" {
		httputil.JSONError(w, http.StatusBadRequest, "검색어가 없습니다.")
		return
	}
	if err := models.DeleteSearchTrend(a.DB, body.Keyword); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "서버 오류가 발생했습니다."})
		return
	}
	a.logAdminAction(r, "delete_trending", "search_trend", body.Keyword, "")
	writeJSON(w, map[string]any{"success": true})
}

func (a *App) SyncMeilisearchIndex() {
	if !a.Meili.Available() {
		return
	}
	var documents []map[string]any
	seen := make(map[string]bool)

	for _, v := range a.VideoPool.Get() {
		if v.VideoID == "" || seen[v.VideoID] {
			continue
		}
		seen[v.VideoID] = true
		documents = append(documents, map[string]any{
			"id": "video_" + v.VideoID, "type": "video", "title": v.Title, "thumbnail": v.Thumbnail,
			"videoId": v.VideoID, "is_short": v.IsShort, "member_name": v.MemberName,
			"choseong": meilisearch.ExtractChoseong(v.Title + " " + v.MemberName),
		})
	}

	if archived, err := models.GetAllArchiveVideos(a.DB); err == nil {
		for _, av := range archived {
			if av.VideoID == "" || seen[av.VideoID] {
				continue
			}
			seen[av.VideoID] = true
			documents = append(documents, map[string]any{
				"id": "video_" + av.VideoID, "type": "video", "title": av.Title.String, "thumbnail": av.Thumbnail.String,
				"videoId": av.VideoID, "is_short": av.IsShort, "member_name": av.MemberName.String,
				"choseong": meilisearch.ExtractChoseong(av.Title.String + " " + av.MemberName.String),
			})
		}
	}

	if fanarts, err := models.SearchFanartGallery(a.DB, "", 500); err == nil {
		for _, f := range fanarts {
			documents = append(documents, map[string]any{
				"id": "fanart_" + strconv.FormatInt(f.ID, 10), "type": "fanart", "title": f.Title.String,
				"description": f.Description.String, "nickname": f.Nickname.String,
				"image_url": f.ImageURL.String, "thumbnail_url": f.ThumbnailURL.String, "fanartId": f.ID,
			})
		}
	}

	if posts, err := models.SearchCommunityPosts(a.DB, "", 500); err == nil {
		for _, p := range posts {
			documents = append(documents, map[string]any{
				"id": "community_" + strconv.FormatInt(p.ID, 10), "type": "community", "content": p.Content.String,
				"author": p.Author.String, "member_name": p.MemberName.String, "postId": p.ID, "date": p.Date,
			})
		}
	}

	if len(documents) == 0 {
		return
	}
	_ = a.Meili.AddDocuments(meiliIndexName, documents)
}
