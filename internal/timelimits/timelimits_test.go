package timelimits

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// makeQuestion builds a choice question like make_question() in the Python test.
func makeQuestion(extra map[string]any) map[string]any {
	q := map[string]any{"question": "Q?", "options": []any{"A", "B"}, "correct_answer": 0}
	for k, v := range extra {
		q[k] = v
	}
	return q
}

// py: tests/test_time_limits_unit.py::test_question_limits_follow_spec_table
func TestQuestionLimitsFollowSpecTable(t *testing.T) {
	questions := []map[string]any{
		makeQuestion(map[string]any{"id": 1}),
		makeQuestion(map[string]any{"time_limit": 20, "id": 2}),
		makeQuestion(map[string]any{"time_limit": 0, "id": 3}),
	}
	cases := []struct {
		settings Settings
		want     map[int]int
	}{
		{NoLimit, map[int]int{1: 0, 2: 0, 3: 0}},
		{Settings{ModePerQuestion, 60}, map[int]int{1: 60, 2: 20, 3: 0}},
		{Settings{ModePerQuestion, 0}, map[int]int{1: 0, 2: 20, 3: 0}},
		{Settings{ModeWholeQuiz, 900}, map[int]int{1: 0, 2: 0, 3: 0}},
	}
	for _, c := range cases {
		if got := QuestionLimits(c.settings, questions); !reflect.DeepEqual(got, c.want) {
			t.Errorf("QuestionLimits(%v) = %v, want %v", c.settings, got, c.want)
		}
	}
}

// py: tests/test_time_limits_unit.py::test_malformed_question_value_uses_default
func TestMalformedQuestionValueUsesDefault(t *testing.T) {
	questions := []map[string]any{makeQuestion(map[string]any{"time_limit": "x", "id": 1})}
	if got := QuestionLimits(Settings{ModePerQuestion, 60}, questions); !reflect.DeepEqual(got, map[int]int{1: 60}) {
		t.Errorf("QuestionLimits = %v, want map[1:60]", got)
	}
}

// py: tests/test_time_limits_unit.py::test_parse_time_limit
func TestParseTimeLimit(t *testing.T) {
	settings, warning := Parse(map[string]any{"time_limit": map[string]any{"whole_quiz": 900}})
	if settings != (Settings{ModeWholeQuiz, 900}) || warning != "" {
		t.Errorf("whole_quiz: got %v, %q", settings, warning)
	}
	settings, warning = Parse(map[string]any{"time_limit": map[string]any{"per_question": 30}})
	if settings != (Settings{ModePerQuestion, 30}) || warning != "" {
		t.Errorf("per_question: got %v, %q", settings, warning)
	}
	settings, warning = Parse(map[string]any{})
	if settings != NoLimit || warning != "" {
		t.Errorf("no time_limit: got %v, %q", settings, warning)
	}
	settings, warning = Parse(map[string]any{"time_limit": map[string]any{"per_question": 5, "whole_quiz": 9}})
	if settings != NoLimit {
		t.Errorf("two modes: got %v, want no limit", settings)
	}
	if warning == "" {
		t.Error("two modes should give a warning")
	}
}

// py: tests/test_time_limits_unit.py::test_validate_time_limits_accepts_good_values
func TestValidateTimeLimitsAcceptsGoodValues(t *testing.T) {
	good := []map[string]any{
		{"time_limit": map[string]any{"per_question": 30}, "questions": []any{makeQuestion(map[string]any{"time_limit": 0})}},
		{"time_limit": map[string]any{"whole_quiz": 0}, "questions": []any{makeQuestion(nil)}},
		// A question time_limit without a quiz time_limit is allowed (and ignored when the quiz runs)
		{"questions": []any{makeQuestion(map[string]any{"time_limit": 10})}},
	}
	for _, data := range good {
		if errors := Validate(data); len(errors) != 0 {
			t.Errorf("Validate(%v) = %v, want no errors", data, errors)
		}
	}
}

// py: tests/test_time_limits_unit.py::test_validate_time_limits_rejects_bad_quiz_values
func TestValidateTimeLimitsRejectsBadQuizValues(t *testing.T) {
	badValues := []any{
		30,
		map[string]any{},
		map[string]any{"per_question": 1, "whole_quiz": 1},
		map[string]any{"minutes": 1},
		map[string]any{"per_question": -1},
		map[string]any{"whole_quiz": 1.5},
		map[string]any{"per_question": true},
		map[string]any{"per_question": "60"},
	}
	for _, value := range badValues {
		errors := Validate(map[string]any{"time_limit": value, "questions": []any{makeQuestion(nil)}})
		if len(errors) == 0 {
			t.Errorf("time_limit %v: no errors", value)
		}
		for _, e := range errors {
			if !strings.Contains(e, "time_limit") {
				t.Errorf("time_limit %v: error does not name time_limit: %q", value, e)
			}
		}
	}
}

// py: tests/test_time_limits_unit.py::test_validate_time_limits_rejects_bad_question_values
func TestValidateTimeLimitsRejectsBadQuestionValues(t *testing.T) {
	for _, value := range []any{-5, 2.5, false, "10"} {
		errors := Validate(map[string]any{"questions": []any{makeQuestion(nil), makeQuestion(map[string]any{"time_limit": value})}})
		found := false
		for _, e := range errors {
			if strings.Contains(e, "Question 2") && strings.Contains(e, "time_limit") {
				found = true
			}
		}
		if !found {
			t.Errorf("time_limit %#v: errors %v do not name Question 2", value, errors)
		}
	}
}

// py: tests/test_time_limits_unit.py::test_is_empty_answer
func TestIsEmptyAnswer(t *testing.T) {
	for _, v := range []any{nil, "", "   ", []any{}} {
		if !IsEmptyAnswer(v) {
			t.Errorf("IsEmptyAnswer(%#v) = false", v)
		}
	}
	for _, v := range []any{0, []any{0}, "a", json.Number("0"), false} {
		if IsEmptyAnswer(v) {
			t.Errorf("IsEmptyAnswer(%#v) = true", v)
		}
	}
}

func TestValidateMessages(t *testing.T) {
	// The exact texts of time_limits.py.
	cases := []struct {
		value any
		want  []string
	}{
		{30, []string{"'time_limit' must have exactly one of 'per_question' or 'whole_quiz'"}},
		{map[string]any{}, []string{"'time_limit' must have exactly one of 'per_question' or 'whole_quiz'"}},
		{map[string]any{"minutes": 1}, []string{"'time_limit' has an unknown key: 'minutes'"}},
		{map[string]any{"b": 1, "a": 2}, []string{"'time_limit' has an unknown key: 'a'", "'time_limit' has an unknown key: 'b'"}},
		{map[string]any{"per_question": 1, "whole_quiz": 1}, []string{"'time_limit' must have exactly one of 'per_question' or 'whole_quiz'"}},
		{map[string]any{"whole_quiz": 1.5}, []string{"'time_limit.whole_quiz' must be a whole number of seconds (0 or more)"}},
		{map[string]any{"per_question": -1, "x": 1}, []string{"'time_limit' has an unknown key: 'x'", "'time_limit.per_question' must be a whole number of seconds (0 or more)"}},
	}
	for _, c := range cases {
		if got := Validate(map[string]any{"time_limit": c.value}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Validate(time_limit: %v) = %q, want %q", c.value, got, c.want)
		}
	}
	got := Validate(map[string]any{"questions": []any{makeQuestion(nil), makeQuestion(map[string]any{"time_limit": -5})}})
	want := []string{"Question 2 'time_limit' must be a whole number of seconds (0 or more)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("question error = %q, want %q", got, want)
	}
	_, warning := Parse(map[string]any{"time_limit": 30})
	if warning != "Ignoring malformed time_limit, quiz runs without time limits: 'time_limit' must have exactly one of 'per_question' or 'whole_quiz'" {
		t.Errorf("warning = %q", warning)
	}
}

func TestJSONNumbersCountLikePython(t *testing.T) {
	// From a JSON body (start settings), 30 is an int and 30.0 is a float.
	if errs := Validate(map[string]any{"time_limit": map[string]any{"per_question": json.Number("30")}}); len(errs) != 0 {
		t.Errorf("json 30: %v", errs)
	}
	if errs := Validate(map[string]any{"time_limit": map[string]any{"per_question": json.Number("30.0")}}); len(errs) == 0 {
		t.Error("json 30.0 is a float in Python and must be rejected")
	}
	settings, _ := Parse(map[string]any{"time_limit": map[string]any{"whole_quiz": json.Number("45")}})
	if settings != (Settings{ModeWholeQuiz, 45}) {
		t.Errorf("Parse(json 45) = %v", settings)
	}
	limits := QuestionLimits(Settings{ModePerQuestion, 10}, []map[string]any{{"id": 1, "time_limit": json.Number("7")}})
	if limits[1] != 7 {
		t.Errorf("QuestionLimits with json 7 = %v", limits)
	}
}

func TestIsWholeSeconds(t *testing.T) {
	for _, v := range []any{0, 5, json.Number("12")} {
		if !IsWholeSeconds(v) {
			t.Errorf("IsWholeSeconds(%#v) = false", v)
		}
	}
	for _, v := range []any{-1, 1.0, true, false, "5", nil} {
		if IsWholeSeconds(v) {
			t.Errorf("IsWholeSeconds(%#v) = true", v)
		}
	}
}

func TestLimitedAndModes(t *testing.T) {
	if NoLimit.Limited() || !(Settings{ModeWholeQuiz, 0}).Limited() {
		t.Error("Limited should tell whether a mode is set")
	}
	if NoLimit.Mode != "none" || ModePerQuestion != "per_question" || ModeWholeQuiz != "whole_quiz" {
		t.Error("mode names must match time_limits.py")
	}
	if !reflect.DeepEqual(Modes, []string{"per_question", "whole_quiz"}) {
		t.Errorf("Modes = %v", Modes)
	}
}

func TestGraceSecondsFromEnvironment(t *testing.T) {
	t.Setenv("WEBQUIZ_TIME_LIMIT_GRACE_SECONDS", "")
	if got := GraceSeconds(); got != 5 {
		t.Errorf("default grace = %v, want 5", got)
	}
	t.Setenv("WEBQUIZ_TIME_LIMIT_GRACE_SECONDS", "0.5")
	if got := GraceSeconds(); got != 0.5 {
		t.Errorf("grace = %v, want 0.5", got)
	}
	t.Setenv("WEBQUIZ_TIME_LIMIT_GRACE_SECONDS", " 2 ")
	if got := GraceSeconds(); got != 2 {
		t.Errorf("grace with spaces = %v, want 2", got)
	}
	t.Setenv("WEBQUIZ_TIME_LIMIT_GRACE_SECONDS", "abc")
	if got := GraceSeconds(); got != 5 {
		t.Errorf("bad grace = %v, want the default 5", got)
	}
	t.Setenv("WEBQUIZ_TIME_LIMIT_GRACE_SECONDS", "0")
	if got := GraceSeconds(); got != 0 {
		t.Errorf("grace 0 = %v", got)
	}
}

func TestCheckIntervalFromEnvironment(t *testing.T) {
	t.Setenv("WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL", "")
	if got := CheckInterval(); got != time.Second {
		t.Errorf("default interval = %v, want 1s", got)
	}
	t.Setenv("WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL", "0.05")
	if got := CheckInterval(); got != 50*time.Millisecond {
		t.Errorf("interval = %v, want 50ms", got)
	}
	// A ticker needs a positive interval; Python slept 0 s in a loop instead.
	for _, bad := range []string{"0", "-1", "nan", "inf", "x"} {
		t.Setenv("WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL", bad)
		if got := CheckInterval(); got != time.Second {
			t.Errorf("interval %q = %v, want the default 1s", bad, got)
		}
	}
}
