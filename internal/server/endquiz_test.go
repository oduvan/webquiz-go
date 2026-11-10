package server

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

// wholeQuizServer is a server whose quiz has n questions and a whole-quiz
// limit of 10 seconds, with one approved student "u1", like the setup of the
// Python unit tests of _end_whole_quiz.
func wholeQuizServer(t *testing.T, n int) *Server {
	t.Helper()
	text := "title: Timed\ntime_limit: {whole_quiz: 10}\nquestions:\n"
	for i := 1; i <= n; i++ {
		text += fmt.Sprintf("- {question: 'Q%d?', options: [A, B], correct_answer: 0}\n", i)
	}
	s := newTestServer(t, map[string]string{"timed.yaml": text}, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addUserLocked(&User{ID: "u1", Username: "Student", Approved: true})
	return s
}

// endWholeQuiz is _end_whole_quiz as the Python tests called it: with the lock.
func (s *Server) endWholeQuiz(userID string, now time.Time, questionID int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endWholeQuizLocked(userID, now, questionID)
}

// Two whole-quiz ends at the same moment (the page's empty answer and the
// background check) store every question once. Python needed this because a
// live-stats send could pause one end in the middle; Go runs them in
// parallel, so this test runs 20 of them at once.
//
// py: tests/test_time_limits_unit.py::test_whole_quiz_end_twice_at_once_stores_each_question_once
func TestWholeQuizEndTwiceAtOnceStoresEachQuestionOnce(t *testing.T) {
	s := wholeQuizServer(t, 3)
	now := time.Now()
	s.mu.Lock()
	s.quizStartTimes["u1"] = now.Add(-30 * time.Second)
	s.mu.Unlock()

	var ends sync.WaitGroup
	for range 20 {
		ends.Go(func() { s.endWholeQuiz("u1", now, 0) })
	}
	ends.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	if got := len(s.userAnswers["u1"]); got != 3 {
		t.Errorf("user_answers has %d answers, want 3", got)
	}
	if got := len(s.userResponses); got != 3 {
		t.Errorf("user_responses has %d rows, want 3", got)
	}
	if stats := s.userStats["u1"]; stats == nil || stats.TotalCount != 3 {
		t.Errorf("user_stats: %+v, want total_count 3", stats)
	}
}

// The whole-quiz end answers the question the page sent its empty answer for
// with the reply stored for that question: a retry then gets the same reply.
//
// py: tests/test_time_limits_unit.py::test_whole_quiz_end_returns_response_for_the_submitted_question
func TestWholeQuizEndReturnsResponseForTheSubmittedQuestion(t *testing.T) {
	s := wholeQuizServer(t, 2)
	response := s.endWholeQuiz("u1", time.Now(), 2)

	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.answerResults["u1"][2]
	if stored == nil || response == nil {
		t.Fatalf("response %v, stored %v", response, stored)
	}
	if reflect.ValueOf(response).UnsafePointer() != reflect.ValueOf(stored.Response).UnsafePointer() {
		t.Error("the reply must be the response stored in answer_results")
	}
	if response["timed_out"] != true {
		t.Errorf("timed_out = %v, want true", response["timed_out"])
	}
	if response["test_completed"] != true {
		t.Errorf("test_completed = %v, want true", response["test_completed"])
	}
}
