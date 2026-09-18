package video

type Video struct {
	Title       string `json:"title"`
	Thumbnail   string `json:"thumbnail"`
	VideoID     string `json:"videoId"`
	ID          string `json:"id"`
	IsShort     bool   `json:"is_short"`
	IsDrive     bool   `json:"is_drive"`
	PublishedAt string `json:"publishedAt"`
	ViewCount   int64  `json:"viewCount"`
	MemberName  string `json:"member_name,omitempty"`

	CommentPreview *CommentPreview `json:"comment_preview,omitempty"`
}

type CommentPreview struct {
	Nickname string `json:"nickname"`
	Content  string `json:"content"`
	Source   string `json:"source"`
}

func (v Video) ToTemplateMap() map[string]any {
	m := map[string]any{
		"title": v.Title, "thumbnail": v.Thumbnail, "videoId": v.VideoID, "id": v.ID,
		"is_short": v.IsShort, "is_drive": v.IsDrive, "publishedAt": v.PublishedAt,
		"viewCount": v.ViewCount, "member_name": v.MemberName,
	}
	if v.CommentPreview != nil {
		m["comment_preview"] = map[string]any{
			"nickname": v.CommentPreview.Nickname, "content": v.CommentPreview.Content, "source": v.CommentPreview.Source,
		}
	} else {
		m["comment_preview"] = nil
	}
	return m
}

func ToTemplateMaps(videos []Video) []map[string]any {
	out := make([]map[string]any, len(videos))
	for i, v := range videos {
		out[i] = v.ToTemplateMap()
	}
	return out
}

var bannedTitles = map[string]bool{
	"private video": true,
	"deleted video": true,
	"비공개 동영상":       true,
	"삭제된 동영상":       true,
}
