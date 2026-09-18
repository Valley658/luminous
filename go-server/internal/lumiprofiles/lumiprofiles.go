// Package lumiprofiles는 루미(AI 마스코트)가 스텔라이브 멤버 개개인에 대해
// 아는 "상세 정보"(소개/취향/기타 사실)를 저장하고 읽어오는 아주 작은
// 로컬 전용 데이터 저장소다.
//
// 이 데이터는 두 프로그램이 공유한다:
//   - cmd/lumi-admin (127.0.0.1에서만 열리는 편집용 웹페이지) - 여기서 씀
//   - cmd/server(메인 사이트, lumi_ai.go) - 여기서 읽어서 루미 AI 시스템
//     프롬프트에 그대로 근거로 넣어줌
//
// 데이터베이스 대신 평범한 JSON 파일 하나로 관리한다 - 양이 아주 적고(멤버
// 10여 명), 편집 UI도 아주 단순해서 별도 스키마/마이그레이션을 둘 필요가
// 없다. 파일 위치는 두 프로그램 다 실행 파일이 go-server/bin/에 있다는
// 전제로 항상 go-server/lumi_data/member_profiles.json을 가리키게 계산한다.
package lumiprofiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Profile은 멤버 한 명에 대해 루미가 알아야 할 상세 정보.
type Profile struct {
	Bio      string `json:"bio"`      // 소개/성격/컨셉
	Likes    string `json:"likes"`    // 좋아하는 것, 취향
	Dislikes string `json:"dislikes"` // 싫어하는 것, 어려워하는 것
	Extra    string `json:"extra"`    // 그 외 자유롭게 적는 사실(유행어, 별명, 특징 등)
}

func (p Profile) IsEmpty() bool {
	return p.Bio == "" && p.Likes == "" && p.Dislikes == "" && p.Extra == ""
}

// DefaultPath는 실행 파일 경로(go-server/bin/*.exe) 기준으로 데이터 파일
// 위치를 계산한다. 두 프로그램(lumi-admin, server) 모두 같은 규칙을 쓰므로
// 항상 같은 파일을 가리킨다.
func DefaultPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return filepath.Join("lumi_data", "member_profiles.json")
	}
	// go-server/bin/xxx.exe -> go-server/lumi_data/member_profiles.json
	binDir := filepath.Dir(exePath)
	goServerDir := filepath.Dir(binDir)
	return filepath.Join(goServerDir, "lumi_data", "member_profiles.json")
}

// Load는 저장된 프로필 맵을 읽어온다. 파일이 아직 없으면(한 번도 안 적었으면)
// 에러 없이 빈 맵을 돌려준다 - 루미 쪽에서는 그냥 "아직 적힌 상세 정보 없음"
// 으로 취급하면 됨.
func Load(path string) (map[string]Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Profile{}, nil
		}
		return nil, err
	}
	var m map[string]Profile
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]Profile{}
	}
	return m, nil
}

// Save는 프로필 맵을 파일에 저장한다. 쓰는 도중 프로세스가 죽거나 다른
// 프로그램이 동시에 읽어도 반쪽짜리 파일을 보지 않도록, 임시 파일에 먼저
// 쓰고 원자적으로 rename한다.
func Save(path string, data map[string]Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SortedNames는 안내용으로 쓸 수 있게 프로필이 채워진 멤버 이름을 정렬해서
// 돌려준다.
func SortedNames(m map[string]Profile) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
