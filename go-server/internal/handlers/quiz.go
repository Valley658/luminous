package handlers

import (
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"pastellive/internal/data"
	"pastellive/internal/httputil"
)

var quizWhitespacePattern = regexp.MustCompile(`\s+`)

func normalizeQuizAnswer(text string) string {
	return strings.ToLower(quizWhitespacePattern.ReplaceAllString(text, ""))
}

var quizDifficultyAliases = map[string]string{
	"easy": "easy", "초": "easy", "초급": "easy",
	"medium": "medium", "중": "medium", "중급": "medium",
	"hard": "hard", "고": "hard", "고급": "hard",
}

func resolveQuizDifficulty(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		key = "hard"
	}
	if v, ok := quizDifficultyAliases[key]; ok {
		return v
	}
	return "hard"
}

func (a *App) ApiQuizQuestionsHandler(w http.ResponseWriter, r *http.Request) {
	difficulty := resolveQuizDifficulty(r.URL.Query().Get("difficulty"))
	questions := data.QuizQuestionsByDifficulty[difficulty]

	result := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		var choices []string
		answerCount := 1
		switch q.CheckType {
		case "ox":
			choices = []string{"O", "X"}
		case "all", "ordered":
			choices = append([]string(nil), q.Accepted...)
			rand.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
			answerCount = len(q.Accepted)
		default:
			// 오답 보기(decoy)는 반드시 같은 종류(Category)끼리만 섞는다 - 예전엔 같은
			// 난이도의 아무 문제 정답이나 갖다 써서 "멤버는 총 몇명?" 같은 숫자 문제에
			// O/X나 멤버 이름이 보기로 튀어나오는 문제가 있었음.
			var pool []string
			if len(q.Decoys) > 0 {
				pool = append([]string(nil), q.Decoys...)
			} else if q.Category != "" {
				for _, other := range questions {
					if other.DisplayAnswer == q.DisplayAnswer || other.Category != q.Category {
						continue
					}
					pool = append(pool, other.DisplayAnswer)
				}
			}
			rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			decoyCount := 3
			if len(pool) < decoyCount {
				decoyCount = len(pool)
			}
			choices = append([]string{q.DisplayAnswer}, pool[:decoyCount]...)
			rand.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
		}
		result = append(result, map[string]any{
			"question":     q.Question,
			"type":         q.CheckType,
			"answer_count": answerCount,
			"choices":      choices,
		})
	}
	writeJSON(w, map[string]any{"difficulty": difficulty, "questions": result})
}

func (a *App) ApiQuizCheckHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Difficulty string `json:"difficulty"`
		Index      any    `json:"index"`
		Answer     string `json:"answer"`
		Answers    []any  `json:"answers"`
	}
	_ = decodeJSONBody(r, &body)

	difficulty := resolveQuizDifficulty(body.Difficulty)
	questions := data.QuizQuestionsByDifficulty[difficulty]

	index, ok := quizAnyToInt(body.Index)
	if !ok {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}
	if index < 0 || index >= len(questions) {
		httputil.JSONError(w, http.StatusBadRequest, "잘못된 문제 번호입니다.")
		return
	}
	q := questions[index]

	var isCorrect bool
	switch q.CheckType {
	case "all":
		normalizedAccepted := make([]string, len(q.Accepted))
		for i, a := range q.Accepted {
			normalizedAccepted[i] = normalizeQuizAnswer(a)
		}
		if len(body.Answers) > 0 {
			submitted := make(map[string]bool, len(body.Answers))
			for _, raw := range body.Answers {
				s := anyToStr(raw)
				if strings.TrimSpace(s) != "" {
					submitted[normalizeQuizAnswer(s)] = true
				}
			}
			isCorrect = true
			for _, acc := range normalizedAccepted {
				if !submitted[acc] {
					isCorrect = false
					break
				}
			}
		} else {
			userAnswer := normalizeQuizAnswer(body.Answer)
			isCorrect = true
			for _, part := range normalizedAccepted {
				if !strings.Contains(userAnswer, part) {
					isCorrect = false
					break
				}
			}
		}
	case "ordered":
		normalizedAccepted := make([]string, len(q.Accepted))
		for i, a := range q.Accepted {
			normalizedAccepted[i] = normalizeQuizAnswer(a)
		}
		if len(body.Answers) > 0 {
			normalizedSubmitted := make([]string, len(body.Answers))
			for i, raw := range body.Answers {
				normalizedSubmitted[i] = normalizeQuizAnswer(anyToStr(raw))
			}
			isCorrect = len(normalizedSubmitted) == len(normalizedAccepted)
			if isCorrect {
				for i := range normalizedAccepted {
					if normalizedSubmitted[i] != normalizedAccepted[i] {
						isCorrect = false
						break
					}
				}
			}
		} else {
			userAnswer := normalizeQuizAnswer(body.Answer)
			cursor := 0
			isCorrect = true
			for _, part := range normalizedAccepted {
				idx := strings.Index(userAnswer[cursor:], part)
				if idx == -1 {
					isCorrect = false
					break
				}
				cursor += idx + len(part)
			}
		}
	default:
		userAnswer := normalizeQuizAnswer(body.Answer)
		isCorrect = false
		for _, a := range q.Accepted {
			if userAnswer == normalizeQuizAnswer(a) {
				isCorrect = true
				break
			}
		}
	}

	writeJSON(w, map[string]any{"success": true, "correct": isCorrect, "correct_answer": q.DisplayAnswer})
}

func quizAnyToInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}
