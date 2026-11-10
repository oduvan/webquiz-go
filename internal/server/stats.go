package server

import (
	"fmt"
	"math"
	"slices"

	"github.com/oduvan/webquiz-go/internal/quiz"
)

// answerRecord is an entry of Python's user_answers: one stored answer (or
// timeout) of a student, for the statistics and the results screen. Records
// are never changed after they are stored.
type answerRecord struct {
	Question       any     // question.get("question", ""): the text as written
	Image          any     // question.get("image"): nil when the question has none
	File           any     // the attachment address "/attach/<name>", or the value as written when empty
	SelectedAnswer string  // "" for a timeout
	CorrectAnswer  any     // the option text(s) of a choice question, correct_value of a text question
	IsCorrect      bool    //
	TimeTaken      seconds //
	Points         float64 // the question's points (normalized)
	EarnedPoints   float64 // Points when correct, else 0
	CheckerError   string  // the checker's message; "" when there is none
	TimedOut       bool    // time ran out (shown on the results screen, not written to the CSV)
}

// result is the record as the results screen gets it. Without showAnswers
// the correct answer and the correctness are left out (get_user_final_results).
func (a *answerRecord) result(showAnswers bool) map[string]any {
	result := map[string]any{
		"question":        a.Question,
		"image":           a.Image,
		"file":            a.File,
		"selected_answer": a.SelectedAnswer,
		"time_taken":      a.TimeTaken,
		"points":          points(a.Points),
		"earned_points":   points(a.EarnedPoints),
	}
	if showAnswers {
		result["correct_answer"] = a.CorrectAnswer
		result["is_correct"] = a.IsCorrect
	}
	if a.CheckerError != "" {
		result["checker_error"] = a.CheckerError
	}
	if a.TimedOut {
		result["timed_out"] = true
	}
	return result
}

// answerRow is an entry of Python's user_responses: an answer that waits for
// the next CSV flush.
type answerRow struct {
	UserID         string
	Username       string
	QuestionID     int
	Question       any // the question text as written ("" when the question has none)
	SelectedAnswer string
	CorrectAnswer  any
	IsCorrect      bool
	TimeTaken      seconds
	Timestamp      string
	CheckerError   string
}

// userStats is an entry of Python's user_stats: the final results of a
// student who answered every question.
type userStats struct {
	TestResults      []*answerRecord
	CorrectCount     int
	TotalCount       int
	Percentage       int
	EarnedPoints     float64
	TotalPoints      float64
	PointsPercentage int
	TotalTime        seconds
	CompletedAt      string // isoformat
}

// answerTotals are the sums over a student's answers that the statistics and
// the users CSV need.
type answerTotals struct {
	count        int
	correct      int
	earnedPoints float64 // normalized
	totalPoints  float64 // normalized
	time         seconds
}

func totalsOf(answers []*answerRecord) answerTotals {
	totals := answerTotals{count: len(answers)}
	earned, total := 0.0, 0.0
	for _, answer := range answers {
		if answer.IsCorrect {
			totals.correct++
		}
		earned += answer.EarnedPoints
		total += answer.Points
		totals.time = totals.time.plus(answer.TimeTaken)
	}
	totals.earnedPoints = quiz.NormalizePoints(earned)
	totals.totalPoints = quiz.NormalizePoints(total)
	return totals
}

// pyRound is Python's round(x) for a float: the nearest whole number, halves
// to the even one.
func pyRound(x float64) int {
	return int(math.RoundToEven(x))
}

// calculateAndStoreUserStatsLocked is calculate_and_store_user_stats: it
// stores the final results of a student from user_answers. The caller holds mu.
func (s *Server) calculateAndStoreUserStatsLocked(userID string) {
	answers := s.userAnswers[userID]
	if len(answers) == 0 {
		s.log.Warn(fmt.Sprintf("No answers found for user %s during stats calculation", userID))
		return
	}
	totals := totalsOf(answers)
	percentage := pyRound(float64(totals.correct) / float64(totals.count) * 100)
	pointsPercentage := 0
	if totals.totalPoints > 0 {
		pointsPercentage = pyRound(totals.earnedPoints / totals.totalPoints * 100)
	}
	s.userStats[userID] = &userStats{
		TestResults:      slices.Clone(answers),
		CorrectCount:     totals.correct,
		TotalCount:       totals.count,
		Percentage:       percentage,
		EarnedPoints:     totals.earnedPoints,
		TotalPoints:      totals.totalPoints,
		PointsPercentage: pointsPercentage,
		TotalTime:        totals.time,
		CompletedAt:      isoformat(s.now()),
	}
	s.log.Info(fmt.Sprintf("Stored final stats for user %s: %d/%d (%d%%), points: %s/%s (%d%%) using user_answers",
		userID, totals.correct, totals.count, percentage, points(totals.earnedPoints), points(totals.totalPoints), pointsPercentage))
}

// allStudentsCompletedLocked is all_students_completed: every student (with
// approval switched on: every approved student) has finished, or the admin
// pressed "Show answers". With no students it is false. The caller holds mu.
func (s *Server) allStudentsCompletedLocked() bool {
	if s.forceAllCompleted {
		return true
	}
	students := len(s.users)
	if s.cfg.Registration.Approve {
		students = 0
		for _, user := range s.users {
			if user.Approved {
				students++
			}
		}
	}
	if students == 0 {
		return false
	}
	return len(s.userStats) >= students
}

// userFinalResultsLocked is get_user_final_results: the results screen of a
// student who finished. The correct answers are left out unless
// show_right_answer is on, or show_answers_on_completion is on and every
// student has finished. The caller holds mu.
func (s *Server) userFinalResultsLocked(userID string) map[string]any {
	stats, ok := s.userStats[userID]
	if !ok {
		return map[string]any{
			"test_results":               []any{},
			"correct_count":              0,
			"total_count":                0,
			"percentage":                 0,
			"earned_points":              0,
			"total_points":               0,
			"points_percentage":          0,
			"total_time":                 0,
			"all_completed":              false,
			"show_answers_on_completion": false,
		}
	}
	allCompleted := s.allStudentsCompletedLocked()
	showAnswers := s.quiz.ShowRightAnswer || (s.quiz.ShowAnswersOnCompletion && allCompleted)
	results := make([]any, len(stats.TestResults))
	for i, answer := range stats.TestResults {
		results[i] = answer.result(showAnswers)
	}
	return map[string]any{
		"test_results":               results,
		"correct_count":              stats.CorrectCount,
		"total_count":                stats.TotalCount,
		"percentage":                 stats.Percentage,
		"earned_points":              points(stats.EarnedPoints),
		"total_points":               points(stats.TotalPoints),
		"points_percentage":          stats.PointsPercentage,
		"total_time":                 stats.TotalTime,
		"all_completed":              allCompleted,
		"show_answers_on_completion": s.quiz.ShowAnswersOnCompletion,
	}
}
