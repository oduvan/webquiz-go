package server

import (
	"fmt"
	"time"
)

// checkTimeLimits is check_time_limits: the time limit watcher calls it every
// Options.CheckInterval (time_limit_watcher). The page sends an empty answer
// when a countdown reaches zero; this check covers a closed tab or a locked
// phone and stores the timeouts the page did not send. It never starts a
// clock: a question the page never showed cannot time out.
func (s *Server) checkTimeLimits(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkTimeLimitsLocked(now)
	return nil
}

// checkTimeLimitsLocked does checkTimeLimits; the caller holds mu. A student
// whose answer is being checked right now is skipped: that answer arrived
// first (Python's watcher could not run while a checker ran), and the next
// round looks at the student again.
func (s *Server) checkTimeLimitsLocked(now time.Time) {
	// Per-question limits: the question whose clock is running.
	for userID, questionID := range s.currentQuestionIDs {
		limit := s.quiz.QuestionTimeLimits[questionID]
		if limit <= 0 || s.answersBeingChecked[userID] > 0 {
			continue
		}
		if _, ok := s.users[userID]; !ok {
			continue
		}
		if _, answered := s.answerResults[userID][questionID]; answered {
			continue
		}
		elapsed := s.questionElapsedLocked(userID, questionID, now)
		if !elapsed.later(limit, s.grace) {
			continue
		}
		question, ok := s.quiz.QuestionByID(questionID)
		if !ok {
			continue
		}
		s.log.Info(fmt.Sprintf("Time ran out for user %s on question %d (no answer from the page)", userID, questionID))
		s.recordAnswerLocked(userID, question, nil, elapsed, recordTimeout, nil)
	}

	// Whole-quiz limit.
	if wholeQuizSeconds(s.quiz) <= 0 {
		return
	}
	for userID, start := range s.quizStartTimes {
		if s.answersBeingChecked[userID] > 0 {
			continue
		}
		if _, ok := s.users[userID]; !ok {
			continue // gone after a quiz switch
		}
		if _, done := s.userStats[userID]; done {
			continue // already finished
		}
		if s.wholeQuizOverLocked(start, now) {
			s.endWholeQuizLocked(userID, now, 0)
		}
	}
}
