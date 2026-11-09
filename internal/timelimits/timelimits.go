package timelimits

import (
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// The time limit modes.
const (
	ModeNone        = "none"
	ModePerQuestion = "per_question"
	ModeWholeQuiz   = "whole_quiz"
)

// Modes are the keys a quiz-level time_limit may have.
var Modes = []string{ModePerQuestion, ModeWholeQuiz}

// Default values of the settings that tests may change through the environment.
const (
	DefaultGraceSeconds  = 5.0
	DefaultCheckInterval = time.Second
)

// Settings is the quiz-level time limit. Mode is ModeNone, ModePerQuestion or
// ModeWholeQuiz. Use NoLimit for "no limit", not the zero value.
type Settings struct {
	Mode    string
	Seconds int
}

// NoLimit is the setting of a quiz without a time limit.
var NoLimit = Settings{Mode: ModeNone}

// Limited tells whether a time limit mode is set.
func (s Settings) Limited() bool {
	return s.Mode == ModePerQuestion || s.Mode == ModeWholeQuiz
}

// IsWholeSeconds is true for a whole number that is 0 or more. True and False
// do not count as numbers here.
func IsWholeSeconds(v any) bool {
	if !pyval.IsIntNotBool(v) {
		return false
	}
	sign, ok := pyval.Compare(v, 0)
	return ok && sign >= 0
}

const exactlyOneMode = "'time_limit' must have exactly one of 'per_question' or 'whole_quiz'"

// quizLevelErrors returns the problems with a quiz-level time_limit value.
func quizLevelErrors(timeLimit any) []string {
	m, ok := pyval.Dict(timeLimit)
	if !ok || len(m) == 0 {
		return []string{exactlyOneMode}
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var errors []string
	var known []string
	for _, key := range keys {
		if slices.Contains(Modes, key) {
			known = append(known, key)
		} else {
			errors = append(errors, fmt.Sprintf("'time_limit' has an unknown key: '%s'", key))
		}
	}
	if len(known) > 1 {
		errors = append(errors, exactlyOneMode)
	}
	for _, key := range known {
		if !IsWholeSeconds(m[key]) {
			errors = append(errors, fmt.Sprintf("'time_limit.%s' must be a whole number of seconds (0 or more)", key))
		}
	}
	if len(known) == 0 && len(errors) == 0 {
		errors = append(errors, exactlyOneMode)
	}
	return errors
}

// Validate checks the quiz-level and the per-question time_limit values of
// quiz data before the quiz is saved. It returns error messages.
func Validate(data map[string]any) []string {
	var errors []string
	if timeLimit, ok := data["time_limit"]; ok {
		errors = append(errors, quizLevelErrors(timeLimit)...)
	}
	if questions, ok := pyval.List(data["questions"]); ok {
		for index, question := range questions {
			q, isDict := pyval.Dict(question)
			if !isDict {
				continue
			}
			if value, ok := q["time_limit"]; ok && !IsWholeSeconds(value) {
				errors = append(errors, fmt.Sprintf("Question %d 'time_limit' must be a whole number of seconds (0 or more)", index+1))
			}
		}
	}
	return errors
}

// Parse reads the quiz-level time_limit of loaded quiz data. Loading a quiz
// does not run the save checks: a broken value means "no time limits", and
// the second result is a warning for the log ("" when all is fine).
func Parse(data map[string]any) (Settings, string) {
	timeLimit, ok := data["time_limit"]
	if !ok {
		return NoLimit, ""
	}
	if errors := quizLevelErrors(timeLimit); len(errors) > 0 {
		return NoLimit, "Ignoring malformed time_limit, quiz runs without time limits: " + strings.Join(errors, "; ")
	}
	m, _ := pyval.Dict(timeLimit)
	for _, mode := range Modes {
		if value, ok := m[mode]; ok {
			seconds, _ := pyval.Int(value)
			return Settings{Mode: mode, Seconds: int(seconds)}, ""
		}
	}
	return NoLimit, ""
}

// QuestionLimits returns the effective per-question limit in seconds for every
// question id (0 = no limit). Only per_question mode gives questions their own
// limits. A broken question value counts as "not set" and uses the quiz default.
// Each question must have its "id".
func QuestionLimits[Q ~map[string]any](settings Settings, questions []Q) map[int]int {
	limits := make(map[int]int, len(questions))
	for _, question := range questions {
		limit := 0
		if settings.Mode == ModePerQuestion {
			limit = settings.Seconds
			if own := question["time_limit"]; IsWholeSeconds(own) {
				seconds, _ := pyval.Int(own)
				limit = int(seconds)
			}
		}
		id, _ := pyval.Int(question["id"])
		limits[int(id)] = limit
	}
	return limits
}

// IsEmptyAnswer is true when no answer was given: nil, empty or blank text,
// or an empty list.
func IsEmptyAnswer(answer any) bool {
	switch pyval.KindOf(answer) {
	case pyval.KindNone:
		return true
	case pyval.KindStr:
		return pyval.Strip(answer.(string)) == ""
	case pyval.KindList:
		items, _ := pyval.List(answer)
		return len(items) == 0
	}
	return false
}

// floatFromEnv reads a number of seconds like Python's float(os.environ[name]).
func floatFromEnv(name string, fallback float64) float64 {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	f, ok := pyval.ToFloat(value)
	if !ok {
		return fallback
	}
	return f
}

// GraceSeconds is how many seconds an answer may arrive after the limit and
// still count (WEBQUIZ_TIME_LIMIT_GRACE_SECONDS, default 5).
func GraceSeconds() float64 {
	return floatFromEnv("WEBQUIZ_TIME_LIMIT_GRACE_SECONDS", DefaultGraceSeconds)
}

// CheckInterval is how often the server checks for timed-out students
// (WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL in seconds, default 1). A value that is
// not a positive finite number gives the default: a Go ticker needs a
// positive interval.
func CheckInterval() time.Duration {
	seconds := floatFromEnv("WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL", DefaultCheckInterval.Seconds())
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return DefaultCheckInterval
	}
	interval := time.Duration(seconds * float64(time.Second))
	if interval <= 0 {
		return DefaultCheckInterval
	}
	return interval
}
