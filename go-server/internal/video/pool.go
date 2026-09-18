package video

import (
	"context"
	"database/sql"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	pdb "pastellive/internal/db"
)

type Pool struct {
	mu     sync.RWMutex
	videos []Video

	// [2026-09-18: 유튜브 할당량 초과 + RSS까지 동시에 실패하는 장애 상황에서,
	// 풀이 비어있는 동안 들어오는 방문자 요청마다 RefreshIfEmpty가 매번 25~30개
	// 채널/재생목록 전체를 다시 긁어가려고 시도하면서 유튜브에 요청을 폭격하고
	// 있었음(로그에서 1분에 여러 번, 심지어 동시에 중복으로 재시도하는 것도
	// 확인됨) - 이게 오히려 RSS 쪽 요청까지 더 실패하게 만들거나 할당량 회복을
	// 늦출 수 있다고 판단해서, 풀이 빈 상태에서의 재시도에 쿨다운과 중복 실행
	// 방지 락을 추가함.]
	refreshMu        sync.Mutex
	refreshing       bool
	lastEmptyAttempt time.Time
}

// emptyPoolRefreshCooldown: 풀이 비어있을 때 RefreshIfEmpty가 다시 전체 갱신을
// 시도하기까지 최소로 기다리는 시간. 유튜브 API 할당량 초과나 일시적 네트워크
// 장애로 갱신이 계속 실패하는 동안, 방문자 요청이 들어올 때마다 매번 재시도해서
// 유튜브 쪽에 부담을 더 주지 않도록 하는 안전장치.
const emptyPoolRefreshCooldown = 20 * time.Second

func NewPool() *Pool {
	return &Pool{}
}

func (p *Pool) Get() []Video {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Video, len(p.videos))
	copy(out, p.videos)
	return out
}

func (p *Pool) IsEmpty() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.videos) == 0
}

func (p *Pool) set(videos []Video) {
	p.mu.Lock()
	p.videos = videos
	p.mu.Unlock()
}

type memberChannelRow struct {
	MemberName      string
	ChannelID       sql.NullString
	ShortsPlaylists sql.NullString
}

func fetchMemberChannels(db *pdb.DB) ([]memberChannelRow, error) {
	rows, err := db.Query("SELECT member_name, channel_id, shorts_playlists FROM members WHERE channel_id IS NOT NULL AND channel_id != 'UC_KANNA_PLACEHOLDER'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memberChannelRow
	for rows.Next() {
		var r memberChannelRow
		if err := rows.Scan(&r.MemberName, &r.ChannelID, &r.ShortsPlaylists); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type fetchJob struct {
	targetID   string
	isPlaylist bool
	forceShort bool
	memberName string
}

// buildFetchJobs는 멤버별 DB 행(채널ID/shorts_playlists)을 실제 유튜브 조회
// 작업 목록으로 바꾼다. 네트워크/DB 접근이 전혀 없는 순수 함수라 따로 떼어내서
// 유닛 테스트하기 쉽게 만들었다.
func buildFetchJobs(rows []memberChannelRow) []fetchJob {
	var jobs []fetchJob
	for _, r := range rows {
		if r.ChannelID.Valid && r.ChannelID.String != "" {
			jobs = append(jobs, fetchJob{targetID: r.ChannelID.String, isPlaylist: false, forceShort: false, memberName: r.MemberName})
		}

		// [2026-09-18: shorts_playlists가 없을 때 "UUSH"+채널ID 재생목록으로
		// 자동 대체하던 폴백을 일단 되돌림 - 배포 후 메인 화면 영상 그리드
		// (videoGrid, init_videos)에 스텔라이브와 전혀 무관한 영상들이 섞여
		// 나오는 현상이 실제로 발생했다. 유력한 원인: members 테이블의
		// channel_id가 잘못 들어있는 행이 있고(정확히 어떤 행인지는 DB
		// 직접 접근 권한이 없어 아직 특정 못함), 이 폴백이 그 채널의
		// "UUSH" 재생목록까지 추가로 긁어오면서 원래도 소량 섞여 있던 그
		// 채널 영상 비중이 전체 풀에서 갑자기 커져버린 것으로 추정. 잘못된
		// channel_id를 실제로 찾아서 고치기 전까지는, 최소한 이 자동 폴백
		// 자체는 꺼둬서 더 이상 상황을 악화시키지 않도록 한다. 아래
		// shorts_playlists 기반 forceShort 작업(원래부터 있던 것)은 그대로
		// 유지 - 이건 리도님이 직접 등록해둔 값이라 안전함.]
		playlistIDs := parseShortsPlaylists(r.ShortsPlaylists)
		for _, pid := range playlistIDs {
			if pid = strings.TrimSpace(pid); pid != "" {
				jobs = append(jobs, fetchJob{targetID: pid, isPlaylist: true, forceShort: true, memberName: r.MemberName})
			}
		}
	}
	return jobs
}

// mergeJobResults는 여러 fetchJob의 결과를 영상 ID 기준으로 합친다. 같은
// 멤버라도 "전체 업로드" 작업(forceShort=false)과 "쇼츠 전용" 작업
// (forceShort=true)을 따로 따로 돌리는데, 한 멤버의 쇼츠는 전체 업로드
// 목록에도 당연히 같이 들어있다(유튜브의 "전체 업로드" 재생목록은 쇼츠를
// 포함한 전체 영상 목록이라서). 예전엔 먼저 처리되는 작업(전체 업로드,
// forceShort=false라 IsShort=false로 기록됨)이 그 영상 ID를 먼저 "차지"해
// 버리면, 나중에 처리되는 쇼츠 전용 작업에서 같은 영상이 IsShort=true로
// 다시 나와도 "이미 있는 ID"라며 그냥 버려졌다 - 그 결과 제목에 "#shorts"/
// "쇼츠" 글자가 우연히 없는 멤버는 실제로는 쇼츠인 영상도 전부 롱폼으로
// 분류돼버려서, 홈 화면 "추천 Shorts"가 제목에 그 글자가 있는 멤버 쪽으로만
// 쏠리는 문제가 있었다. 이제는 중복 ID를 만나도 무조건 버리지 않고, 둘 중
// 하나라도 IsShort=true면 최종 결과도 IsShort=true로 남긴다(그 외 필드는
// 먼저 나온 값을 그대로 유지).
func mergeJobResults(results [][]Video) []Video {
	newPool := make([]Video, 0)
	index := make(map[string]int)
	for _, vids := range results {
		for _, v := range vids {
			if v.VideoID == "" {
				continue
			}
			if idx, ok := index[v.VideoID]; ok {
				if v.IsShort && !newPool[idx].IsShort {
					newPool[idx].IsShort = true
				}
				continue
			}
			index[v.VideoID] = len(newPool)
			newPool = append(newPool, v)
		}
	}
	return newPool
}

// maxVideosPerMember: 한 멤버(또는 DB에 잘못 들어간 channel_id처럼 예상 밖의
// 소스)가 통 전체를 과도하게 차지하는 걸 막는 안전장치. 2026-09-18에 실제로
// 이 문제가 터졌음(메인 화면 영상 그리드에 스텔라이브와 무관한 영상이 대거
// 섞여 나옴) - 정확한 원인(잘못된 channel_id로 추정)을 못 찾은 상태에서도,
// 이 상한선만으로 어떤 소스든 최종 풀의 절대 다수를 차지하는 걸 막아준다.
const maxVideosPerMember = 60

// capVideosPerMember는 멤버별 영상 개수를 maxVideosPerMember로 제한한다(먼저
// 나온 것부터 유지 - Refresh에서 셔플 전에 호출되므로 아직 순서에 특별한
// 의미는 없음).
func capVideosPerMember(videos []Video, maxPerMember int) []Video {
	count := make(map[string]int, len(videos))
	out := make([]Video, 0, len(videos))
	for _, v := range videos {
		count[v.MemberName]++
		if count[v.MemberName] > maxPerMember {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (p *Pool) Refresh(ctx context.Context, db *pdb.DB, apiKey string) {
	rows, err := fetchMemberChannels(db)
	if err != nil {
		log.Printf("영상 풀 갱신 실패: DB 조회 중 오류 - %v", err)
		return
	}
	if len(rows) == 0 {
		log.Printf("영상 풀 갱신 실패: members 테이블에 channel_id가 채워진 행이 없습니다.")
		return
	}

	jobs := buildFetchJobs(rows)
	if len(jobs) == 0 {
		return
	}

	results := make([][]Video, len(jobs))
	var wg sync.WaitGroup
	var errCount int
	var errMu sync.Mutex
	for i, job := range jobs {
		wg.Add(1)
		go func(idx int, j fetchJob) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errMu.Lock()
					errCount++
					errMu.Unlock()
				}
			}()
			results[idx] = FetchPlaylistOrChannel(ctx, j.targetID, j.isPlaylist, j.forceShort, j.memberName, apiKey, 4)
		}(i, job)
	}
	wg.Wait()

	newPool := mergeJobResults(results)
	newPool = capVideosPerMember(newPool, maxVideosPerMember)

	if len(newPool) == 0 {
		log.Printf("영상 풀 갱신 실패: 요청 %d건 중 %d건 예외 발생, 반환된 영상 0개 (채널ID/재생목록ID/네트워크 확인 필요)", len(jobs), errCount)
		return
	}

	// 할당량이 소진된 동안에는 Data API 대신 채널/재생목록별 RSS(최신 15개)로만 채워지기 때문에,
	// 매 갱신마다 롱폼/쇼츠 비율이 들쭉날쭉해질 수 있다. 이미 정상적으로 채워둔 풀이 있다면
	// 할당량이 회복될 때까지 갱신을 보류하고 기존 풀을 그대로 유지한다.
	if skip, reason := shouldSkipPoolRefresh(p.Get(), newPool, QuotaIsExceeded()); skip {
		log.Printf("영상 풀 갱신 보류: %s (요청 %d건 중 %d건 예외 발생)", reason, len(jobs), errCount)
		return
	}

	newLongCount := countLongVideos(newPool)
	log.Printf("영상 풀 갱신 완료: 요청 %d건 중 %d개 영상 확보 (롱폼 %d개)", len(jobs), len(newPool), newLongCount)
	rand.Shuffle(len(newPool), func(i, j int) { newPool[i], newPool[j] = newPool[j], newPool[i] })
	p.set(newPool)
}

// shouldSkipPoolRefresh decides whether a freshly-fetched, non-empty pool should be
// discarded in favor of keeping the pool that's already live. newPool is assumed
// non-empty (callers check that separately, with its own distinct log message).
// Pulled out as a pure function so the decision itself can be unit tested without a
// DB or network - see pool_test.go.
func shouldSkipPoolRefresh(oldPool, newPool []Video, quotaExceeded bool) (skip bool, reason string) {
	if len(oldPool) > 0 && quotaExceeded {
		return true, "유튜브 API 할당량 소진 상태 - 결과가 불안정할 수 있어 기존 풀 유지"
	}

	if countLongVideos(newPool) == 0 && countLongVideos(oldPool) > 0 {
		return true, "이번 갱신 결과에 롱폼 영상이 0개입니다 (유튜브 API 할당량 소진 등 일시적 문제로 추정) - 기존 풀 유지"
	}

	return false, ""
}

func countLongVideos(videos []Video) int {
	n := 0
	for _, v := range videos {
		if !IsShortTitle(v) {
			n++
		}
	}
	return n
}

func (p *Pool) RefreshIfEmpty(ctx context.Context, db *pdb.DB, apiKey string) {
	if !p.IsEmpty() {
		return
	}

	p.refreshMu.Lock()
	if p.refreshing || time.Since(p.lastEmptyAttempt) < emptyPoolRefreshCooldown {
		p.refreshMu.Unlock()
		return
	}
	p.refreshing = true
	p.lastEmptyAttempt = time.Now()
	p.refreshMu.Unlock()

	defer func() {
		p.refreshMu.Lock()
		p.refreshing = false
		p.refreshMu.Unlock()
	}()

	p.Refresh(ctx, db, apiKey)
}

func IsShortTitle(v Video) bool {
	if v.IsShort {
		return true
	}
	lower := strings.ToLower(v.Title)
	return strings.Contains(lower, "#shorts") || strings.Contains(lower, "shorts") || strings.Contains(v.Title, "쇼츠")
}
