package data

import (
	"os"
	"path/filepath"
)

type Member struct {
	Name    string `json:"name"`
	Img     string `json:"img"`
	ChzzkID string `json:"chzzkId"`
}

type Generation struct {
	Key       string   `json:"key"`
	UnitLabel string   `json:"unit_label"`
	Members   []string `json:"-"`
}

type GroupedGeneration struct {
	Key       string   `json:"key"`
	UnitLabel string   `json:"unit_label"`
	Img       string   `json:"img"`
	Members   []Member `json:"members"`
}

var SIDEBAR_MEMBERS = []Member{
	{Name: "스텔라이브", Img: "/static/logo/logo.webp", ChzzkID: ""},
	{Name: "칸나", Img: "/static/images/members/kanna.webp", ChzzkID: ""},
	{Name: "유니", Img: "/static/images/members/yuni.webp", ChzzkID: "45e71a76e949e16a34764deb962f9d9f"},
	{Name: "후야", Img: "/static/images/members/huya.webp", ChzzkID: "36ddb9bb4f17593b60f1b63cec86611d"},
	{Name: "히나", Img: "/static/images/members/hina.webp", ChzzkID: "b044e3a3b9259246bc92e863e7d3f3b8"},
	{Name: "리제", Img: "/static/images/members/lize.webp", ChzzkID: "4325b1d5bbc321fad3042306646e2e50"},
	{Name: "마시로", Img: "/static/images/members/mashiro.webp", ChzzkID: "4515b179f86b67b4981e16190817c580"},
	{Name: "타비", Img: "/static/images/members/tabi.webp", ChzzkID: "a6c4ddb09cdb160478996007bff35296"},
	{Name: "린", Img: "/static/images/members/rin.webp", ChzzkID: "516937b5f85cbf2249ce31b0ad046b0f"},
	{Name: "시부키", Img: "/static/images/members/shibuki.webp", ChzzkID: "64d76089fba26b180d9c9e48a32600d9"},
	{Name: "나나", Img: "/static/images/members/nana.webp", ChzzkID: "4d812b586ff63f8a2946e64fa860bbf5"},
	{Name: "리코", Img: "/static/images/members/riko.webp", ChzzkID: "8fd39bb8de623317de90654718638b10"},
	{Name: "강지", Img: "https://pbs.twimg.com/media/ESBmIZDVAAAXQs_.jpg", ChzzkID: "b5ed5db484d04faf4d150aedd362f34b"},
	{Name: "김블루", Img: "https://pbs.twimg.com/media/ESBmIZDVAAAXQs_.jpg", ChzzkID: "0de8f1807076169b7eb9218137e99c51"},
}

var MEMBER_GENERATIONS = []Generation{
	{Key: "1기생", UnitLabel: "미스틱 · 에버리스", Members: []string{"칸나", "유니", "후야"}},
	{Key: "2기생", UnitLabel: "유니버스", Members: []string{"히나", "마시로", "리제", "타비"}},
	{Key: "3기생", UnitLabel: "클리셰", Members: []string{"시부키", "린", "나나", "리코"}},
}

var MEMBER_FULL_NAMES = map[string]string{
	// 스텔라이브 공식 11명 멤버가 아닌 사이드바 항목(로고/기타)도 여기 넣어야
	// gotemplates.go의 locale 폴백 로직(member.<이름> 카탈로그 조회)을 타고
	// 언어 전환 시 번역됨 - 값은 한국어 폴백용.
	"스텔라이브": "스텔라이브",
	"강지":    "강지",
	"김블루":   "김블루",
	"칸나":    "아이리 칸나",
	"유니":    "아야츠노 유니",
	"후야":    "사키하네 후야",
	"히나":    "시라유키 히나",
	"마시로":   "네네코 마시로",
	"리제":    "아카네 리제",
	"타비":    "아라하시 타비",
	"시부키":   "텐코 시부키",
	"린":     "아오쿠모 린",
	"나나":    "하나코 나나",
	"리코":    "유즈하 리코",
}

var MEMBER_ACCENT_COLORS = map[string]string{
	"칸나":  "#857fd8",
	"유니":  "#ad97f3",
	"후야":  "#8d7ca5",
	"히나":  "#d0a382",
	"리제":  "#8b2635",
	"마시로": "#d9a441",
	"타비":  "#8fc6f8",
	"린":   "#4d7dcd",
	"시부키": "#d7c6e8",
	"나나":  "#ff8ca1",
	"리코":  "#8fe1b0",
}

var MemberImages = func() map[string]string {
	m := make(map[string]string, len(SIDEBAR_MEMBERS))
	for _, mem := range SIDEBAR_MEMBERS {
		m[mem.Name] = mem.Img
	}
	return m
}()

var generationRepImageStems = map[string]string{
	"1기생": "gen1_group",
	"2기생": "gen2_group",
	"3기생": "gen3_group",
}
var generationRepImageExtensions = []string{"webp", "png", "jpg", "jpeg"}

func BuildGenerationRepImageOverrides(staticDir string) map[string]string {
	overrides := make(map[string]string)
	for genKey, stem := range generationRepImageStems {
		for _, ext := range generationRepImageExtensions {
			rel := filepath.Join("images", stem+"."+ext)
			if _, err := os.Stat(filepath.Join(staticDir, rel)); err == nil {
				overrides[genKey] = "/static/" + filepath.ToSlash(rel)
				break
			}
		}
	}
	return overrides
}

type SidebarGroups struct {
	Generations  []GroupedGeneration
	UngroupedTop []Member
	UngroupedBot []Member
}

func BuildSidebarGroups(repImageOverrides map[string]string) SidebarGroups {
	byName := make(map[string]Member, len(SIDEBAR_MEMBERS))
	for _, m := range SIDEBAR_MEMBERS {
		byName[m.Name] = m
	}
	groupedNames := make(map[string]bool)
	generations := make([]GroupedGeneration, 0, len(MEMBER_GENERATIONS))
	for _, gen := range MEMBER_GENERATIONS {
		genMembers := make([]Member, 0, len(gen.Members))
		for _, n := range gen.Members {
			if m, ok := byName[n]; ok {
				genMembers = append(genMembers, m)
			}
			groupedNames[n] = true
		}
		repImg := repImageOverrides[gen.Key]
		if repImg == "" && len(genMembers) > 0 {
			repImg = genMembers[0].Img
		}
		generations = append(generations, GroupedGeneration{
			Key:       gen.Key,
			UnitLabel: gen.UnitLabel,
			Img:       repImg,
			Members:   genMembers,
		})
	}
	ungroupedTop := make([]Member, 0, 1)
	ungroupedBot := make([]Member, 0, len(SIDEBAR_MEMBERS))
	for _, m := range SIDEBAR_MEMBERS {
		if groupedNames[m.Name] {
			continue
		}
		if m.Name == "스텔라이브" {
			ungroupedTop = append(ungroupedTop, m)
		} else {
			ungroupedBot = append(ungroupedBot, m)
		}
	}
	return SidebarGroups{Generations: generations, UngroupedTop: ungroupedTop, UngroupedBot: ungroupedBot}
}

func (m Member) ToTemplateMap() map[string]any {
	return map[string]any{"name": m.Name, "img": m.Img, "chzzkId": m.ChzzkID}
}

func MembersToTemplateMaps(members []Member) []map[string]any {
	out := make([]map[string]any, len(members))
	for i, m := range members {
		out[i] = m.ToTemplateMap()
	}
	return out
}

func (g GroupedGeneration) ToTemplateMap() map[string]any {
	return map[string]any{
		"key": g.Key, "unit_label": g.UnitLabel, "img": g.Img,
		"members": MembersToTemplateMaps(g.Members),
	}
}

func GroupedGenerationsToTemplateMaps(gens []GroupedGeneration) []map[string]any {
	out := make([]map[string]any, len(gens))
	for i, g := range gens {
		out[i] = g.ToTemplateMap()
	}
	return out
}

var GenerationKeys = func() map[string]bool {
	keys := make(map[string]bool, len(MEMBER_GENERATIONS))
	for _, g := range MEMBER_GENERATIONS {
		keys[g.Key] = true
	}
	return keys
}()
