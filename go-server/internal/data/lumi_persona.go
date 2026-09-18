package data

import (
	"fmt"
	"strings"
)

// 루미(사이트 마스코트) 관련 데이터를 한 곳에 모아둔 파일.
//
// 성격/말투/시스템 프롬프트 규칙/고정 대사는 더 이상 여기 Go 코드에 없다 -
// go-server/lumi_data/lumi_dialogue.json 파일로 옮겨서(go-server/internal/
// lumidialogue 패키지가 읽음), 리도님이 Go 코드를 빌드하지 않고도 텍스트
// 에디터로 바로 고칠 수 있게 했다. 여기 남은 건 members.go 데이터에서
// 자동으로 만들어지는 값들뿐이다 - 이건 사람이 손으로 관리하면 안 되는
// 값이라(새 멤버가 추가돼도 자동으로 반영돼야 함) 계속 코드에 남겨둔다.

// LumiName은 마스코트의 이름.
const LumiName = "루미"

// LumiRosterFacts는 스텔라이브 멤버 로스터를 짧게 요약해 시스템 프롬프트에 넣어주는
// 근거 자료. (작은 로컬 모델은 최신/상세 정보를 모를 수 있으니, 최소한 "누가
// 멤버인지"는 헷갈리지 않도록 근거를 직접 제공함.)
// members.go의 MEMBER_GENERATIONS/MEMBER_FULL_NAMES/SIDEBAR_MEMBERS에서 그대로
// 만들어낸다(예전엔 이 파일에 손으로 따로 적어뒀었는데, 새 멤버가 들어와도
// 여기 안 고치면 루미만 계속 옛날 로스터로 아는 문제가 있어서 자동 생성으로
// 바꿈 - 진짜 데이터 소스는 항상 members.go 하나뿐이게).
var LumiRosterFacts = buildLumiRosterFacts()

func buildLumiRosterFacts() string {
	inGeneration := make(map[string]bool, len(SIDEBAR_MEMBERS))
	genLines := make([]string, 0, len(MEMBER_GENERATIONS))
	total := 0
	for _, gen := range MEMBER_GENERATIONS {
		parts := make([]string, 0, len(gen.Members))
		for _, name := range gen.Members {
			inGeneration[name] = true
			full := MEMBER_FULL_NAMES[name]
			if full == "" || full == name {
				parts = append(parts, name)
			} else {
				parts = append(parts, fmt.Sprintf("%s(%s)", name, full))
			}
		}
		total += len(gen.Members)
		genLines = append(genLines, fmt.Sprintf("%s(%s, %d명): %s", gen.Key, gen.UnitLabel, len(gen.Members), strings.Join(parts, ", ")))
	}
	others := make([]string, 0)
	for _, m := range SIDEBAR_MEMBERS {
		if m.Name == "스텔라이브" || inGeneration[m.Name] {
			continue
		}
		others = append(others, m.Name)
	}
	total += len(others)

	var sb strings.Builder
	// 총원을 맨 앞에 명확한 숫자로 박아준다 - "스텔라이브 멤버 총 몇 명이야?" 같은
	// 질문에 모델이 직접 세다가 틀리는 일이 없도록(로스터를 죽 나열만 해주면
	// 작은 모델이 합산을 틀리는 경우가 있었음), members.go 기준으로 항상 정확히
	// 계산해서 답을 미리 만들어준다.
	fmt.Fprintf(&sb, "[스텔라이브 멤버 목록 - 총 %d명]\n", total)
	for _, line := range genLines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if len(others) > 0 {
		sb.WriteString("기타: " + strings.Join(others, ", "))
	}
	return strings.TrimRight(sb.String(), "\n")
}
