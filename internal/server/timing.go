package server

import (
	"fmt"
	"strconv"
	"time"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
)

// The clocks of the time limits and of time_taken (server.py: question-start,
// _question_elapsed, _answer_elapsed, _remaining_times).
//
// A question's clock starts at question-start, when the page shows the
// question; registration and approval do not start it. questionStartTimes
// and currentQuestionIDs hold the running question clock of each student,
// quizStartTimes the whole-quiz clock (the first question-start). An answer
// stops only its own question's clock. questionReadyTimes (registration or
// approval, then each stored answer) is where the time limit check counts
// from when an answer arrives without question-start, as a request sent by
// hand does.

// seconds is a number of seconds as the Python server had it: the int 0 when
// no clock ran for the question, otherwise the float that
// timedelta.total_seconds() gave (whole microseconds). JSON and the CSV files
// write it like Python: 0, 2.5, 3.0, 1e-06.
type seconds struct {
	value float64
	float bool // a float in Python; false is the int 0 (or a sum of them)
}

// zeroSeconds is the int 0 of Python's code.
var zeroSeconds = seconds{}

// secondsBetween is (now - start).total_seconds().
func secondsBetween(start, now time.Time) seconds {
	return seconds{value: float64(now.Sub(start).Microseconds()) / 1e6, float: true}
}

// plus is Python's a + b for two times.
func (t seconds) plus(u seconds) seconds {
	return seconds{value: t.value + u.value, float: t.float || u.float}
}

// String is Python's str() of the number.
func (t seconds) String() string {
	if !t.float {
		return strconv.FormatInt(int64(t.value), 10)
	}
	return pyval.FloatRepr(t.value)
}

// MarshalJSON writes the number like Python's json.dumps.
func (t seconds) MarshalJSON() ([]byte, error) {
	return []byte(t.String()), nil
}

// later tells whether the time is more than limit seconds plus the grace,
// like "elapsed > limit + grace".
func (t seconds) later(limit int, grace time.Duration) bool {
	return t.value > float64(limit)+grace.Seconds()
}

// remainingSeconds is max(0.0, limit - elapsed): the int limit when no clock
// ran, otherwise a float that is never below 0.0.
func remainingSeconds(limit int, elapsed seconds) seconds {
	remaining := seconds{value: float64(limit) - elapsed.value, float: elapsed.float}
	if !(remaining.value > 0) {
		return seconds{value: 0, float: true}
	}
	return remaining
}

// points is a points value in a reply: whole values as integers (3), others
// with their decimals (0.5), like normalize_points gave them to json.dumps.
type points float64

// String is Python's str(normalize_points(value)).
func (p points) String() string { return quiz.FormatPoints(float64(p)) }

// MarshalJSON writes the value like Python's json.dumps.
func (p points) MarshalJSON() ([]byte, error) { return []byte(p.String()), nil }

// isoformat is Python's datetime.isoformat() of a local time without a time
// zone: microseconds only when they are not zero.
func isoformat(t time.Time) string {
	t = t.Local()
	text := t.Format("2006-01-02T15:04:05")
	if microseconds := t.Nanosecond() / 1000; microseconds != 0 {
		text += fmt.Sprintf(".%06d", microseconds)
	}
	return text
}

// questionIsLimitedLocked is _question_is_limited: the question has its own
// limit, or the whole quiz has one. The caller holds mu.
func (s *Server) questionIsLimitedLocked(questionID int) bool {
	return s.quiz.QuestionTimeLimits[questionID] > 0 || wholeQuizSeconds(s.quiz) > 0
}

// questionElapsedLocked is _question_elapsed: the seconds since this
// question's clock started, or the int 0 when its clock is not running.
func (s *Server) questionElapsedLocked(userID string, questionID int, now time.Time) seconds {
	current, ok := s.currentQuestionIDs[userID]
	start, started := s.questionStartTimes[userID]
	if !ok || current != questionID || !started {
		return zeroSeconds
	}
	return secondsBetween(start, now)
}

// answerElapsedLocked is _answer_elapsed: the seconds to check against the
// question's limit when an answer arrives. Normally the question's own clock;
// when the page never sent question-start for it, the time since the question
// became available.
func (s *Server) answerElapsedLocked(userID string, questionID int, now time.Time) seconds {
	if current, ok := s.currentQuestionIDs[userID]; ok && current == questionID {
		if start, started := s.questionStartTimes[userID]; started {
			return secondsBetween(start, now)
		}
	}
	ready, ok := s.questionReadyTimes[userID]
	if !ok {
		return zeroSeconds
	}
	return secondsBetween(ready, now)
}

// remainingTimesLocked is _remaining_times: the seconds left for the question
// and for the whole quiz, without the grace. Only the limits that apply are
// in the result, so a quiz without limits gives an empty map.
func (s *Server) remainingTimesLocked(userID string, questionID int, now time.Time) map[string]any {
	remaining := map[string]any{}
	if limit := s.quiz.QuestionTimeLimits[questionID]; limit > 0 {
		elapsed := zeroSeconds
		if current, ok := s.currentQuestionIDs[userID]; ok && current == questionID {
			elapsed = secondsBetween(s.questionStartTimes[userID], now)
		}
		remaining["remaining_seconds"] = remainingSeconds(limit, elapsed)
	}
	if limit := wholeQuizSeconds(s.quiz); limit > 0 {
		if start, ok := s.quizStartTimes[userID]; ok {
			remaining["quiz_remaining_seconds"] = remainingSeconds(limit, secondsBetween(start, now))
		}
	}
	return remaining
}

// wholeQuizOverLocked tells whether the whole-quiz time and the grace have
// passed for a whole-quiz clock that started at start. The caller holds mu.
func (s *Server) wholeQuizOverLocked(start, now time.Time) bool {
	return secondsBetween(start, now).later(wholeQuizSeconds(s.quiz), s.grace)
}
