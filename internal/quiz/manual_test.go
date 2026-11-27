package quiz

import "testing"

// A question with manual_check: true is a text question: the student types
// the answer and the teacher marks it right or wrong.
func TestManualQuestionIsAText(t *testing.T) {
	cases := []struct {
		name           string
		question       Question
		manual, isText bool
	}{
		{"manual without checker", Question{"question": "Q", "manual_check": true}, true, true},
		{"manual with empty checker", Question{"question": "Q", "manual_check": true, "checker": ""}, true, true},
		{"manual false", Question{"question": "Q", "manual_check": false, "options": []any{"a", "b"}, "correct_answer": 0}, false, false},
		{"manual as text is not manual", Question{"question": "Q", "manual_check": "yes"}, false, false},
		{"manual as 1 is not manual", Question{"question": "Q", "manual_check": 1}, false, false},
		{"checker only", Question{"question": "Q", "checker": "x"}, false, true},
		{"choice", Question{"question": "Q", "options": []any{"a", "b"}, "correct_answer": 0}, false, false},
	}
	for _, c := range cases {
		if got := c.question.IsManual(); got != c.manual {
			t.Errorf("%s: IsManual = %v, want %v", c.name, got, c.manual)
		}
		if got := c.question.IsText(); got != c.isText {
			t.Errorf("%s: IsText = %v, want %v", c.name, got, c.isText)
		}
	}
}

// A quiz with a manual question and no checker, options or correct_answer loads.
func TestParseManualQuestion(t *testing.T) {
	q, err := Parse([]byte("questions:\n- question: Why?\n  manual_check: true\n  correct_value: Because\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !q.Questions[0].IsManual() || !q.Questions[0].IsText() {
		t.Errorf("question = %v, want a manual text question", q.Questions[0])
	}
}

func validateOne(question map[string]any) []string {
	return Validate(map[string]any{"title": "T", "questions": []any{question}})
}

func TestValidateManualCheck(t *testing.T) {
	useFakeChecker(t)
	if errors := validateOne(map[string]any{"question": "Q", "manual_check": true}); len(errors) != 0 {
		t.Errorf("manual question without checker: %q, want no errors", errors)
	}
	if errors := validateOne(map[string]any{"question": "Q", "manual_check": true, "checker": "  "}); len(errors) != 0 {
		t.Errorf("manual question with a blank checker: %q, want no errors", errors)
	}
	if errors := validateOne(map[string]any{"question": "Q", "manual_check": false, "options": []any{"a", "b"}, "correct_answer": 0}); len(errors) != 0 {
		t.Errorf("manual_check false on a choice question: %q, want no errors", errors)
	}

	errors := validateOne(map[string]any{"question": "Q", "manual_check": "yes", "checker": ""})
	if !hasError(errors, "Question 1 'manual_check' must be a boolean (true or false)") {
		t.Errorf("manual_check as text: %q", errors)
	}

	errors = validateOne(map[string]any{"question": "Q", "manual_check": true, "checker": "assert user_answer"})
	if !hasError(errors, "Question 1 cannot have both 'manual_check: true' and checker code") {
		t.Errorf("manual with checker code: %q", errors)
	}

	errors = validateOne(map[string]any{"question": "Q", "manual_check": true, "options": []any{"a", "b"}, "correct_answer": 0})
	if !hasError(errors, "Question 1 with 'manual_check: true' cannot have options or correct_answer") {
		t.Errorf("manual with options: %q", errors)
	}
	if len(errors) != 1 {
		t.Errorf("manual with options: %q, want only the manual_check error (it is a text question)", errors)
	}

	// Text question fields are still checked
	errors = validateOne(map[string]any{"question": "Q", "manual_check": true, "correct_value": 5})
	if !hasError(errors, "Question 1 correct_value must be a string") {
		t.Errorf("manual with a number as correct_value: %q", errors)
	}
}
