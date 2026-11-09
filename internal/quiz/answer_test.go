package quiz

import (
	"encoding/json"
	"testing"
)

// multipleAnswersQuestions are the questions of TestMultipleAnswersValidation.setup_method.
func multipleAnswersQuestions() []Question {
	return []Question{
		{"id": 1, "question": "Single answer question", "options": []any{"A", "B", "C", "D"}, "correct_answer": 1},
		{
			"id":             2,
			"question":       "Multiple answers - all required",
			"options":        []any{"Python", "HTML", "JavaScript", "CSS"},
			"correct_answer": []any{0, 2},
		},
		{
			"id":             3,
			"question":       "Multiple answers - minimum required",
			"options":        []any{"Red", "Green", "Blue", "Yellow"},
			"correct_answer": []any{0, 2, 3},
			"min_correct":    2,
		},
	}
}

func checkCorrect(t *testing.T, q Question, answer any, want bool) {
	t.Helper()
	got, message := CheckAnswer(q, answer)
	if got != want {
		t.Errorf("CheckAnswer(question %v, %#v) = %v, want %v", q["id"], answer, got, want)
	}
	if message != "" {
		t.Errorf("CheckAnswer(question %v, %#v) gave a checker message for a choice question: %q", q["id"], answer, message)
	}
}

// py: tests/test_multiple_answers.py::TestMultipleAnswersValidation::test_single_answer_validation
func TestSingleAnswerValidation(t *testing.T) {
	q := multipleAnswersQuestions()[0]
	checkCorrect(t, q, 1, true)
	checkCorrect(t, q, 0, false)
	checkCorrect(t, q, 2, false)
}

// py: tests/test_multiple_answers.py::TestMultipleAnswersValidation::test_multiple_answers_all_required
func TestMultipleAnswersAllRequired(t *testing.T) {
	q := multipleAnswersQuestions()[1]
	checkCorrect(t, q, []any{0, 2}, true)
	checkCorrect(t, q, []any{2, 0}, true) // order does not matter
	checkCorrect(t, q, []any{0}, false)   // partly correct
	checkCorrect(t, q, []any{2}, false)
	checkCorrect(t, q, []any{0, 1, 2}, false) // a wrong answer included
	checkCorrect(t, q, []any{0, 2, 3}, false)
	checkCorrect(t, q, []any{1, 3}, false) // all wrong
}

// py: tests/test_multiple_answers.py::TestMultipleAnswersValidation::test_multiple_answers_minimum_required
func TestMultipleAnswersMinimumRequired(t *testing.T) {
	q := multipleAnswersQuestions()[2]
	checkCorrect(t, q, []any{0, 2, 3}, true)
	checkCorrect(t, q, []any{0, 2}, true) // 2 of 3 is enough
	checkCorrect(t, q, []any{0, 3}, true)
	checkCorrect(t, q, []any{2, 3}, true)
	checkCorrect(t, q, []any{0}, false) // fewer than min_correct
	checkCorrect(t, q, []any{2}, false)
	checkCorrect(t, q, []any{0, 1, 2}, false) // a wrong answer included
	checkCorrect(t, q, []any{0, 2, 1}, false)
}

// py: tests/test_multiple_answers.py::TestMultipleAnswersValidation::test_invalid_answer_formats
func TestInvalidAnswerFormats(t *testing.T) {
	single, multiple := multipleAnswersQuestions()[0], multipleAnswersQuestions()[1]
	checkCorrect(t, single, []any{1}, false) // wrong type for a single answer
	checkCorrect(t, single, "1", false)
	checkCorrect(t, multiple, 0, false) // wrong type for multiple answers
	checkCorrect(t, multiple, "0,2", false)
	checkCorrect(t, multiple, []any{0, 5}, false) // index out of range
	checkCorrect(t, single, []any{-1}, false)
}

// py: tests/test_multiple_answers.py::TestMultipleAnswersValidation::test_csv_formatting
func TestCSVFormatting(t *testing.T) {
	cases := []struct {
		answer  any
		options []any
		want    string
	}{
		{1, []any{"A", "B", "C"}, "B"},
		{[]any{2, 0}, []any{"A", "B", "C", "D"}, "A|C"}, // sorted
		{[]any{1, 3, 0}, []any{"A", "B", "C", "D"}, "A|B|D"},
		{[]any{}, []any{"A", "B"}, ""},
	}
	for _, c := range cases {
		if got := FormatAnswer(c.answer, c.options, false); got != c.want {
			t.Errorf("FormatAnswer(%#v, %v) = %q, want %q", c.answer, c.options, got, c.want)
		}
	}
}

func TestCheckAnswerPythonEquality(t *testing.T) {
	single, multiple := multipleAnswersQuestions()[0], multipleAnswersQuestions()[1]
	// From a JSON body (UseNumber) and with Python's ==: 1 == 1.0 == True.
	checkCorrect(t, single, json.Number("1"), true)
	checkCorrect(t, single, json.Number("1.0"), true)
	checkCorrect(t, single, 1.0, true)
	checkCorrect(t, single, true, true)
	checkCorrect(t, single, nil, false)
	checkCorrect(t, multiple, []any{json.Number("0"), json.Number("2")}, true)
	checkCorrect(t, multiple, []any{0.0, 2.0}, true)
	checkCorrect(t, multiple, []any{0, 2, 2}, true) // a set: the repeat does not count
	checkCorrect(t, multiple, []any{false, 2}, true)
	checkCorrect(t, multiple, []any{"0", 2}, false)
	checkCorrect(t, multiple, []any{0.5, 2}, false)
	checkCorrect(t, multiple, []any{[]any{0}, 2}, false) // Python raised TypeError (unhashable)
	checkCorrect(t, multiple, []any{}, false)
	// A malformed correct_answer is never matched.
	checkCorrect(t, Question{"id": 9, "options": []any{"A", "B"}, "correct_answer": "0"}, "0", false)
	// min_correct may be any number; a value that cannot be compared fails.
	checkCorrect(t, Question{"id": 9, "options": []any{"A", "B", "C"}, "correct_answer": []any{0, 1, 2}, "min_correct": 1.5}, []any{0, 1}, true)
	checkCorrect(t, Question{"id": 9, "options": []any{"A", "B", "C"}, "correct_answer": []any{0, 1}, "min_correct": nil}, []any{0, 1}, false)
}

func TestCheckAnswerTextQuestion(t *testing.T) {
	oldRun := RunChecker
	t.Cleanup(func() { RunChecker = oldRun })

	var gotCode, gotAnswer string
	RunChecker = func(code, answer string) (bool, string) {
		gotCode, gotAnswer = code, answer
		return answer == "5", "wrong"
	}
	q := Question{"id": 1, "question": "2+3?", "checker": "assert user_answer == '5'"}
	if correct, message := CheckAnswer(q, "5"); !correct || message != "wrong" {
		t.Errorf("CheckAnswer = %v, %q", correct, message)
	}
	if gotCode != "assert user_answer == '5'" || gotAnswer != "5" {
		t.Errorf("RunChecker got %q, %q", gotCode, gotAnswer)
	}
	// A number sent by hand reaches the checker as its text.
	CheckAnswer(q, json.Number("5"))
	if gotAnswer != "5" {
		t.Errorf("answer 5 reached the checker as %q", gotAnswer)
	}

	// An empty checker compares with correct_value, without the spaces around.
	exact := Question{"id": 2, "question": "Q", "checker": "", "correct_value": " Kyiv "}
	if correct, message := CheckAnswer(exact, "Kyiv\n"); !correct || message != "" {
		t.Errorf("exact match = %v, %q", correct, message)
	}
	if correct, _ := CheckAnswer(exact, "Lviv"); correct {
		t.Error("Lviv should not match Kyiv")
	}
	if correct, _ := CheckAnswer(Question{"id": 3, "question": "Q", "checker": nil}, ""); !correct {
		t.Error("no checker and no correct_value: an empty answer matches the empty correct value")
	}
	// Python's exec() refused code that is not text, and the answer was wrong.
	if correct, message := CheckAnswer(Question{"id": 4, "question": "Q", "checker": 5}, "5"); correct ||
		message != "exec() arg 1 must be a string, bytes or code object" {
		t.Errorf("checker 5: %v, %q", correct, message)
	}

	RunChecker = nil
	if correct, message := CheckAnswer(q, "5"); correct || message == "" {
		t.Errorf("without RunChecker: %v, %q", correct, message)
	}
}

func TestFormatAnswerPythonStr(t *testing.T) {
	options := []any{"A", "B", "C"}
	cases := []struct {
		answer any
		isText bool
		want   string
	}{
		{"free text", false, "free text"},
		{json.Number("7"), true, "7"},
		{5, false, "Invalid index: 5"},
		{-1, false, "Invalid index: -1"},
		{true, false, "B"}, // True is the index 1 in Python
		{json.Number("2"), false, "C"},
		{[]any{json.Number("2"), true}, false, "B|C"},
		{[]any{0, 7}, false, "A|Invalid index: 7"},
		{1.5, false, "1.5"},
		{nil, false, "None"},
		{map[string]any{"a": json.Number("1")}, false, "{'a': 1}"},
	}
	for _, c := range cases {
		if got := FormatAnswer(c.answer, options, c.isText); got != c.want {
			t.Errorf("FormatAnswer(%#v, isText=%v) = %q, want %q", c.answer, c.isText, got, c.want)
		}
	}
	if got := FormatAnswer(0, nil, false); got != "Invalid index: 0" {
		t.Errorf("no options: %q", got)
	}
	if got := FormatAnswer(0, []any{}, false); got != "Invalid index: 0" {
		t.Errorf("empty options: %q", got)
	}
}
