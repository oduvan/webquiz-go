package quiz

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// fakeChecker stands in for internal/checker: it knows the two broken checkers
// of testdata/validate_cases.json and gives the messages Python's compile() gave.
func fakeChecker(code string) (string, int, bool) {
	switch code {
	case "if True print(1)":
		return "invalid syntax", 1, false
	case "x = (1,\ny = 2":
		return "'(' was never closed", 1, false
	}
	return "", 0, true
}

func useFakeChecker(t *testing.T) {
	t.Helper()
	old := ValidateChecker
	ValidateChecker = fakeChecker
	t.Cleanup(func() { ValidateChecker = old })
}

func hasError(errors []string, part string) bool {
	for _, e := range errors {
		if strings.Contains(e, part) {
			return true
		}
	}
	return false
}

// py: tests/test_multiple_answers.py::TestQuizValidation::test_valid_multiple_answer_quiz
func TestValidMultipleAnswerQuiz(t *testing.T) {
	data := map[string]any{
		"title": "Test Quiz",
		"questions": []any{
			map[string]any{"question": "Multiple choice question", "options": []any{"A", "B", "C", "D"}, "correct_answer": []any{0, 2}},
			map[string]any{"question": "Single choice question", "options": []any{"Yes", "No"}, "correct_answer": 1},
		},
	}
	if errors := Validate(data); len(errors) != 0 {
		t.Errorf("Validate = %q, want no errors", errors)
	}
}

// py: tests/test_multiple_answers.py::TestQuizValidation::test_invalid_multiple_answer_quiz
func TestInvalidMultipleAnswerQuiz(t *testing.T) {
	data := map[string]any{"questions": []any{
		map[string]any{"question": "Invalid question", "options": []any{"A", "B", "C"}, "correct_answer": []any{}},
	}}
	errors := Validate(data)
	if len(errors) == 0 || !hasError(errors, "correct_answer array cannot be empty") {
		t.Errorf("Validate = %q", errors)
	}
}

// py: tests/test_multiple_answers.py::TestQuizValidation::test_min_correct_validation
func TestMinCorrectValidation(t *testing.T) {
	question := map[string]any{"question": "Test question", "options": []any{"A", "B", "C", "D"}, "correct_answer": []any{0, 1, 2}, "min_correct": 2}
	data := map[string]any{"questions": []any{question}}
	if errors := Validate(data); len(errors) != 0 {
		t.Errorf("min_correct 2: Validate = %q, want no errors", errors)
	}
	question["min_correct"] = 5
	errors := Validate(data)
	if len(errors) == 0 || !hasError(errors, "min_correct cannot exceed number of correct answers") {
		t.Errorf("min_correct 5: Validate = %q", errors)
	}
}

// py: tests/test_multiple_answers.py::TestQuizValidation::test_min_correct_single_answer_error
func TestMinCorrectSingleAnswerError(t *testing.T) {
	data := map[string]any{"questions": []any{
		map[string]any{"question": "Single answer with min_correct", "options": []any{"A", "B", "C"}, "correct_answer": 1, "min_correct": 1},
	}}
	errors := Validate(data)
	if len(errors) == 0 || !hasError(errors, "min_correct is only valid for multiple answer questions") {
		t.Errorf("Validate = %q", errors)
	}
}

// validateCase is one entry of testdata/validate_cases.json, written by
// testdata/gen_validate_cases.py with _validate_quiz_data of WebQuiz 1.19.
type validateCase struct {
	Data   json.RawMessage `json:"data"`
	Valid  bool            `json:"valid"`
	Errors []string        `json:"errors"`
}

func TestValidateMatchesPython(t *testing.T) {
	useFakeChecker(t)
	raw, err := os.ReadFile("testdata/validate_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []validateCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 100 {
		t.Fatalf("expected the full case list, got %d cases", len(cases))
	}
	for _, c := range cases {
		decoder := json.NewDecoder(bytes.NewReader(c.Data))
		decoder.UseNumber() // 1 and 1.0 stay different, as in Python
		var data any
		if err := decoder.Decode(&data); err != nil {
			t.Fatal(err)
		}
		got := Validate(data)
		want := c.Errors
		if len(want) == 0 {
			want = nil
		}
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Validate(%s)\n got %q\nwant %q", c.Data, got, want)
		}
		if (len(got) == 0) != c.Valid {
			t.Errorf("Validate(%s): valid should be %v", c.Data, c.Valid)
		}
	}
}

func TestValidateYAMLValues(t *testing.T) {
	// The same rules for values read from a quiz file: YAML gives int,
	// float64 and bool, and yes/no are booleans as in PyYAML.
	text := `
title: Тест
show_right_answer: no
randomize_questions: yes
questions:
  - question: Q1
    options: [A, B, C]
    correct_answer: [0, 2]
    min_correct: 1
    points: 1.5
  - question: Q2
    options: [A, B]
    correct_answer: 1.0
    stick_to_the_previous: on
  - question: Q3
    options:
      - Yes
      - No
    correct_answer: 0
`
	data, err := pyval.LoadYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Question 2 correct_answer must be an integer or array of integers",
		"Question 3 all options must be strings",
	}
	if got := Validate(data); !reflect.DeepEqual(got, want) {
		t.Errorf("Validate = %q, want %q", got, want)
	}
}

func TestValidateWithoutChecker(t *testing.T) {
	old := ValidateChecker
	ValidateChecker = nil
	t.Cleanup(func() { ValidateChecker = old })
	data := map[string]any{"questions": []any{map[string]any{"question": "Q", "checker": "if True print(1)"}}}
	if errors := Validate(data); len(errors) != 0 {
		t.Errorf("without ValidateChecker the syntax is not checked, got %q", errors)
	}
}

func TestValidateCheckerMessageFormat(t *testing.T) {
	old := ValidateChecker
	ValidateChecker = func(code string) (string, int, bool) { return "got newline, want ':'", 3, false }
	t.Cleanup(func() { ValidateChecker = old })
	data := map[string]any{"questions": []any{
		map[string]any{"question": "Q", "options": []any{"A", "B"}, "correct_answer": 0},
		map[string]any{"question": "Q", "checker": "x"},
	}}
	want := []string{"Question 2 checker has invalid Python syntax: got newline, want ':' (line 3)"}
	if got := Validate(data); !reflect.DeepEqual(got, want) {
		t.Errorf("Validate = %q, want %q", got, want)
	}
}
