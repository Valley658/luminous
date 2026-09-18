package data

type QuizQuestion struct {
	Question  string
	CheckType string
	// Category와 Decoys는 CheckType "any" 문제의 객관식 오답 보기를 고를 때만 쓰인다.
	// (과거엔 같은 난이도의 다른 모든 문제의 정답을 그냥 오답 보기로 섞어 썼는데,
	// 그러다 보니 "멤버는 총 몇명?" 같은 숫자 문제에 O/X나 멤버 이름이 보기로
	// 섞여 나오는 문제가 있었음. 이제는 Decoys에 미리 골라둔, 같은 종류의
	// 오답만 보여준다.)
	Category      string
	Decoys        []string
	Accepted      []string
	DisplayAnswer string
}

var quizQuestionsEasy = []QuizQuestion{
	{Question: "스텔라이브 멤버는 총 몇명? (졸업생 포함)", CheckType: "any", Category: "count",
		Decoys: []string{"9명", "10명", "12명"}, Accepted: []string{"11", "11명"}, DisplayAnswer: "11명"},
	{Question: "스텔라이브 멤버들은 공식적으로 고유색이 정확하게 지정되어있다.", CheckType: "ox", Accepted: []string{"X"}, DisplayAnswer: "X"},
	{Question: "스텔라이브는 현재 소속사같은 느낌이 아닌 진짜 기업 형태의 회사이다.", CheckType: "ox", Accepted: []string{"O"}, DisplayAnswer: "O"},
	{Question: "\"느엥\"이라는 유행어를 가지고 있는 멤버는?", CheckType: "any", Category: "member",
		Decoys: []string{"시라유키 히나", "네네코 마시로", "아라하시 타비"}, Accepted: []string{"아카네 리제", "아카네리제", "리제"}, DisplayAnswer: "아카네 리제"},
}

var quizQuestionsMedium = []QuizQuestion{
	{Question: "스텔라이브 멤버들은 전부다 콜라보곡이 하나씩 있다.", CheckType: "ox", Accepted: []string{"X"}, DisplayAnswer: "X"},
	{Question: "\"참지 않아도 돼 싸버려(참지마 싸버려)\"라는 유행어를 가지고 있는 멤버는?", CheckType: "any", Category: "member",
		Decoys: []string{"시라유키 히나", "텐코 시부키", "유즈하 리코"}, Accepted: []string{"아이리 칸나", "아이리칸나"}, DisplayAnswer: "아이리 칸나"},
	{Question: "개인곡을 3개 이상 가지고 있는 멤버들의 이름은? (콜라보곡 제외)", CheckType: "all", Accepted: []string{"아이리 칸나", "아야츠노 유니"}, DisplayAnswer: "아이리 칸나, 아야츠노 유니"},
	{Question: "1, B기생인 아이리 칸나는 자신의 생일에 2번 졸업 방송을 했다.", CheckType: "ox", Accepted: []string{"O"}, DisplayAnswer: "O"},
}

var quizQuestionsHard = []QuizQuestion{
	{Question: "\"그 온도를\"라는 가사가 나오는 노래를 부른 멤버는? (공식 풀네임으로)", CheckType: "any", Category: "member",
		Decoys: []string{"아이리 칸나", "아야츠노 유니", "하나코 나나"}, Accepted: []string{"강지"}, DisplayAnswer: "강지"},
	{Question: "\"사라질듯한\"라는 가사가 나오는 노래를 부른 멤버는? (공식 풀네임으로)", CheckType: "any", Category: "member",
		Decoys: []string{"강지", "아야츠노 유니", "아오쿠모 린"}, Accepted: []string{"아이리 칸나", "아이리칸나"}, DisplayAnswer: "아이리 칸나"},
	{Question: "\"민트초코\"라는 가사가 나오는 노래를 부른 멤버는? (공식 풀네임으로)", CheckType: "any", Category: "member",
		Decoys: []string{"강지", "아이리 칸나", "네네코 마시로"}, Accepted: []string{"아야츠노 유니", "아야츠노유니"}, DisplayAnswer: "아야츠노 유니"},
	{Question: "아이리 칸나의 \"최종화\"의 가사 중 \"사라질듯한 그댄 ???? 애달픈 꽃망울\"이라는 가사가 있는데 ????에 들어갈 말은?", CheckType: "any", Category: "word",
		Decoys: []string{"애처롭게", "서글프게", "쓸쓸하게"}, Accepted: []string{"허무하고"}, DisplayAnswer: "허무하고"},
	{Question: "아오쿠모 린의 \"maid my way\"의 가사 중 \"나도 원래 이랬던 건 아니야 누구보다 더 ????\"에서 ????에 들어갈 말은?", CheckType: "any", Category: "word",
		Decoys: []string{"우직하게", "고지식하게", "무모하게"}, Accepted: []string{"미련하게"}, DisplayAnswer: "미련하게"},
	{Question: "2, 3번 문제의 답인 2명은 몇 기생?", CheckType: "any", Category: "generation",
		Decoys: []string{"2기생", "3기생"}, Accepted: []string{"1기생", "1기"}, DisplayAnswer: "1기생"},
	{Question: "스텔라이브 멤버 중 봉누도1의 오프닝곡을 맡은 멤버의 이름은? (풀네임으로)", CheckType: "any", Category: "member",
		Decoys: []string{"유즈하 리코", "텐코 시부키", "아오쿠모 린"}, Accepted: []string{"하나코 나나", "하나코나나"}, DisplayAnswer: "하나코 나나"},
	{Question: "리제가 원래 하려고 했던 \"쿵쿵 ?? 쿵쿵 ?? 쿵쿵 ?? 쿵쿵 ??\"에서 ??에 들어갈 말을 순서대로 나열하면?", CheckType: "ordered", Accepted: []string{"천마", "강림", "엥나", "앙복"}, DisplayAnswer: "천마, 강림, 엥나, 앙복"},
	{Question: "아이리 칸나가 낸 노래들(개인곡 3개)을 오래된 순으로 나열하면? (정확한 표기로 할 것)", CheckType: "ordered", Accepted: []string{"ADDICT!ON", "최종화", "푸른 보석과 어린 용"}, DisplayAnswer: "ADDICT!ON, 최종화, 푸른 보석과 어린 용"},
	{Question: "\"luminous\"라는 단어가 들어가는 노래의 이름은? (정확한 표기로 할 것)", CheckType: "any", Category: "song",
		Decoys: []string{"최종화", "ADDICT!ON", "maid my way"}, Accepted: []string{"STAR TRAIL"}, DisplayAnswer: "STAR TRAIL"},
	{Question: "(멤버들 피셜) STAR TRAIL 뮤비에 강지가 나온 횟수는?", CheckType: "any", Category: "count",
		Decoys: []string{"1번", "3번", "4번"}, Accepted: []string{"2번", "2회", "2"}, DisplayAnswer: "2번"},
}

var QuizQuestionsByDifficulty = map[string][]QuizQuestion{
	"easy":   quizQuestionsEasy,
	"medium": quizQuestionsMedium,
	"hard":   quizQuestionsHard,
}
