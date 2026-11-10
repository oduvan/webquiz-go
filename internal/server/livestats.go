package server

import (
	"net/http"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
)

// The live stats page (/live-stats/) gets everything over the WebSocket
// /ws/live-stats: an initial_state message when it connects, then a message
// for every change (user_registered, state_update, quiz_switched). It shows
// the students in two groups, in progress and completed.

// updateLiveStatsLocked is update_live_stats: the state of one question of
// one student ("think", "ok", "fail" or "timeout") and the seconds it took
// (nil while the student thinks). The caller holds mu.
func (s *Server) updateLiveStatsLocked(userID string, questionID int, state string, timeTaken *seconds) {
	questions, ok := s.liveStats[userID]
	if !ok {
		questions = map[int]liveState{}
		s.liveStats[userID] = questions
	}
	questions[questionID] = liveState{State: state, TimeTaken: timeTaken}
}

// totalPointsLocked is sum_points(q.get("points", 1) for q in questions): the
// points of the whole quiz. The caller holds mu.
func (s *Server) totalPointsLocked() points {
	values := make([]float64, 0, len(s.quiz.Questions))
	for _, question := range s.quiz.Questions {
		values = append(values, rawPoints(question))
	}
	return points(quiz.SumPoints(values...))
}

// rawPoints is float(question.get("points", 1)) before rounding (Python
// summed the values as written and rounded the sum).
func rawPoints(question quiz.Question) float64 {
	value, ok := question["points"]
	if !ok {
		return 1
	}
	number, ok := pyval.ToFloat(value)
	if !ok {
		return question.Points() // quiz.Load rejects such a value; this only guards hand-made questions
	}
	return number
}

// firstQuestionIDLocked is the question a student starts with: the first of
// the student's own order when the quiz is shuffled, else question 1. The
// caller holds mu and checks that the quiz has questions.
func (s *Server) firstQuestionIDLocked(user *User) int {
	if s.quiz.RandomizeQuestions && len(user.QuestionOrder) > 0 {
		return user.QuestionOrder[0]
	}
	return 1
}

// startLiveStatsLocked puts a student who may start now (registered without
// approval, or approved) on the live stats page: the first question shows
// "think" and the pages hear user_registered. Nothing happens for a quiz
// without questions. The caller holds mu.
func (s *Server) startLiveStatsLocked(user *User) {
	if len(s.quiz.Questions) == 0 {
		return
	}
	first := s.firstQuestionIDLocked(user)
	s.updateLiveStatsLocked(user.ID, first, "think", nil)
	s.broadcastLiveStats(map[string]any{
		"type":            "user_registered",
		"user_id":         user.ID,
		"username":        user.Username,
		"question_id":     first,
		"state":           "think",
		"time_taken":      nil,
		"total_questions": len(s.quiz.Questions),
		"total_points":    s.totalPointsLocked(),
	})
}

// liveStatsInitialStateLocked is the initial_state message of
// websocket_live_stats. Students who wait for approval are left out. The
// caller holds mu; the message is encoded while mu is still held.
func (s *Server) liveStatsInitialStateLocked() map[string]any {
	users := map[string]any{}
	liveStats := map[string]any{}
	completedUsers := map[string]any{}
	completionTimes := map[string]any{}
	for userID, user := range s.users {
		if !user.Approved {
			continue
		}
		users[userID] = user.Username
		if stats, ok := s.liveStats[userID]; ok {
			liveStats[userID] = stats
		}
		completedUsers[userID] = len(s.userAnswers[userID]) == len(s.quiz.Questions)
		if stats, ok := s.userStats[userID]; ok {
			completionTimes[userID] = stats.CompletedAt
		}
	}
	var currentQuiz any
	if name := s.currentQuizName(); name != "" {
		currentQuiz = name
	}
	return map[string]any{
		"type":             "initial_state",
		"live_stats":       liveStats,
		"users":            users,
		"completed_users":  completedUsers,
		"completion_times": completionTimes,
		"questions":        s.quiz.Questions,
		"total_questions":  len(s.quiz.Questions),
		"total_points":     s.totalPointsLocked(),
		"current_quiz":     currentQuiz,
	}
}

// websocketLiveStats is /ws/live-stats (local network only).
func (s *Server) websocketLiveStats(w http.ResponseWriter, r *http.Request) error {
	return s.serveWebSocket(w, r, s.liveStatsClients, func() any { return s.liveStatsInitialStateLocked() })
}
