package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// textInputQuiz is the quiz of the text_input_server fixture.
func textInputQuiz() map[string]any {
	return map[string]any{
		"title":             "Text Input Test Quiz",
		"show_right_answer": true,
		"questions": []any{
			map[string]any{
				"question":      "What is 2+2? (Enter the number)",
				"default_value": "",
				"correct_value": "4",
				"checker":       "assert user_answer.strip() == '4', 'Expected 4'",
				"points":        1,
			},
			map[string]any{
				"question":      "Calculate the square root of 16",
				"default_value": "",
				"correct_value": "4",
				"checker":       "result = float(user_answer.strip())\nassert abs(result - math.sqrt(16)) < 0.01, f'Expected 4, got {result}'",
				"points":        2,
			},
			map[string]any{
				"question":      "What is the capital of France?",
				"correct_value": "Paris",
				"checker":       "",
				"points":        1,
			},
			map[string]any{"question": "What color is the sky?", "options": []any{"Red", "Blue", "Green"}, "correct_answer": 1},
		},
	}
}

func TestTextInputQuestions(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, textInputQuiz())

	// py: tests/test_text_input_questions.py::TestTextInputQuestions::test_text_question_correct_answer
	t.Run("correct_answer", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "testuser")
		data := answerOK(t, ts, userID, 1, "4")
		if data["is_correct"] != true || data["is_text_question"] != true || data["correct_value"] != "4" {
			t.Errorf("reply: %v", data)
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuestions::test_text_question_incorrect_answer
	t.Run("incorrect_answer", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "testuser2")
		data := answerOK(t, ts, userID, 1, "5")
		if data["is_correct"] != false || data["is_text_question"] != true || data["correct_value"] != "4" {
			t.Errorf("reply: %v", data)
		}
		if msg, ok := data["checker_error"].(string); !ok || !strings.Contains(msg, "Expected 4") {
			t.Errorf("checker_error: %v", data["checker_error"])
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuestions::test_text_question_with_math_checker
	t.Run("math_checker", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "mathuser")
		ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": "4"}) // skip the first question
		data := answerOK(t, ts, userID, 2, "4")
		if data["is_correct"] != true {
			t.Errorf("reply: %v", data)
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuestions::test_text_question_without_checker
	t.Run("without_checker", func(t *testing.T) {
		for _, tc := range []struct {
			name, answer string
			correct      bool
		}{{"nocheck", "Paris", true}, {"nocheck2", "London", false}} {
			userID := ts.RegisterStudent(t, tc.name)
			answerOK(t, ts, userID, 1, "4")
			answerOK(t, ts, userID, 2, "4")
			data := answerOK(t, ts, userID, 3, tc.answer)
			if data["is_correct"] != tc.correct {
				t.Errorf("%s: reply %v", tc.answer, data)
			}
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuestions::test_mixed_question_types
	t.Run("mixed_question_types", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "mixeduser")
		if data := answerOK(t, ts, userID, 1, "4"); data["is_correct"] != true {
			t.Errorf("q1: %v", data)
		}
		answerOK(t, ts, userID, 2, "4")
		answerOK(t, ts, userID, 3, "Paris")
		data := answerOK(t, ts, userID, 4, 1)
		if data["is_correct"] != true {
			t.Errorf("q4: %v", data)
		}
		if data["is_text_question"] == true {
			t.Errorf("a choice question must not be a text question: %v", data)
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuestions::test_text_question_whitespace_handling
	t.Run("whitespace_handling", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "whitespace")
		if data := answerOK(t, ts, userID, 1, "  4  "); data["is_correct"] != true {
			t.Errorf("reply: %v", data)
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuestions::test_client_receives_text_question_data
	// The Python test looked for JSON text in the page. The Go page keeps the
	// same data in its boot JSON (the questions without answers).
	t.Run("client_receives_text_question_data", func(t *testing.T) {
		res := ts.GetJSON(t, "/")
		if res.Status != 200 {
			t.Fatalf("GET /: %d", res.Status)
		}
		html := res.Text()
		if !strings.Contains(html, "What is 2+2? (Enter the number)") {
			t.Errorf("the question text is missing from the page")
		}
		first := question(t, pageBoot(t, html), 0)
		if first["type"] != "text" {
			t.Errorf(`question 1 must have "type": "text": %v`, first)
		}
		if _, ok := first["default_value"]; !ok {
			t.Errorf("default_value is missing: %v", first)
		}
		if _, ok := first["checker"]; ok {
			t.Errorf("the checker must not reach the page: %v", first)
		}
	})
}

func TestTextInputQuizValidation(t *testing.T) {
	t.Parallel()

	// py: tests/test_text_input_questions.py::TestTextInputQuizValidation::test_valid_text_question_quiz
	t.Run("valid_text_question_quiz", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, map[string]any{"title": "Valid Text Quiz", "questions": []any{map[string]any{
			"question": "Enter your answer:", "default_value": "", "correct_value": "test", "checker": "assert user_answer == 'test'",
		}}})
		if res := ts.GetJSON(t, "/"); res.Status != 200 {
			t.Errorf("GET /: %d", res.Status)
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuizValidation::test_text_question_with_image
	t.Run("text_question_with_image", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, map[string]any{"title": "Image Text Quiz", "questions": []any{map[string]any{
			"question": "Look at the image and answer:", "image": "/imgs/test.png", "correct_value": "answer", "checker": "",
		}}})
		res := ts.GetJSON(t, "/")
		if res.Status != 200 {
			t.Fatalf("GET /: %d", res.Status)
		}
		if image := question(t, pageBoot(t, res.Text()), 0)["image"]; image != "/imgs/test.png" {
			t.Errorf("image: %v", image)
		}
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuizValidation::test_text_question_with_points
	t.Run("text_question_with_points", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, map[string]any{"title": "Points Text Quiz", "questions": []any{map[string]any{
			"question": "Enter answer:", "correct_value": "test", "checker": "", "points": 5,
		}}})
		userID := ts.RegisterStudent(t, "pointsuser")
		answerOK(t, ts, userID, 1, "test")
	})

	// py: tests/test_text_input_questions.py::TestTextInputQuizValidation::test_invalid_checker_python_syntax
	t.Run("invalid_checker_python_syntax", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, map[string]any{"title": "Invalid Checker Quiz", "questions": []any{map[string]any{
			"question": "Enter answer:", "correct_value": "test", "checker": "if True\n    print('missing colon')",
		}}})
		admin := ts.AdminClient(t, "test123")
		invalid := map[string]any{"title": "Bad Checker Quiz", "questions": []any{map[string]any{
			"question": "Test question", "checker": "def broken(\n    pass", "correct_value": "answer",
		}}}
		res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{"filename": "invalid_checker.yaml", "quiz_data": invalid})
		if res.Status != 400 {
			t.Fatalf("status %d: %s", res.Status, res.Body)
		}
		if msg, _ := res.JSON(t)["error"].(string); !strings.Contains(msg, "Неправильна структура даних квізу") {
			t.Errorf("error: %v", msg)
		}
	})
}

func TestCheckerTemplates(t *testing.T) {
	t.Parallel()
	quiz := map[string]any{"title": "Test Quiz", "questions": []any{map[string]any{"question": "Test?", "options": []any{"A", "B"}, "correct_answer": 0}}}

	// py: tests/test_text_input_questions.py::TestCheckerTemplates::test_list_checker_templates_empty
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, quiz)
		admin := ts.AdminClient(t, "test123")
		res := admin.GetJSON(t, "/api/admin/list-checker-templates")
		if res.Status != 200 {
			t.Fatalf("status %d: %s", res.Status, res.Body)
		}
		if templates, ok := res.JSON(t)["templates"].([]any); !ok {
			t.Errorf("templates must be a list: %s", res.Body)
		} else if len(templates) != 0 {
			t.Errorf("no templates configured, got %v", templates)
		}
	})

	// py: tests/test_text_input_questions.py::TestCheckerTemplates::test_list_checker_templates_with_config
	t.Run("with_config", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, quiz, func(o *testutil.Options) {
			o.Config = map[string]any{"checker_templates": []any{
				map[string]any{"name": "Exact Match", "code": "assert user_answer.strip() == 'expected'"},
				map[string]any{"name": "Numeric Check", "code": "assert float(user_answer) == 42"},
			}}
		})
		admin := ts.AdminClient(t, "test123")
		res := admin.GetJSON(t, "/api/admin/list-checker-templates")
		if res.Status != 200 {
			t.Fatalf("status %d: %s", res.Status, res.Body)
		}
		templates, _ := res.JSON(t)["templates"].([]any)
		if len(templates) != 2 || templates[0].(map[string]any)["name"] != "Exact Match" || templates[1].(map[string]any)["name"] != "Numeric Check" {
			t.Errorf("templates: %v", templates)
		}
	})
}

// checkerQuiz is a one-question text quiz with a checker.
func checkerQuiz(title, correct, checker string) map[string]any {
	return map[string]any{"title": title, "questions": []any{map[string]any{
		"question": "Enter:", "correct_value": correct, "checker": checker,
	}}}
}

func TestCheckerBuiltinFunctions(t *testing.T) {
	t.Parallel()
	// Each case registers one student and sends one answer.
	cases := []struct {
		py       string
		name     string
		quiz     map[string]any
		user     string
		answer   string
		correct  bool
		errorMsg string // the checker_error must contain it ("*" = any message)
	}{
		{"test_to_int_function", "to_int", checkerQuiz("Checker Functions Quiz", "42", "assert to_int(user_answer) == 42"), "toint_user", "  42  ", true, ""},
		{"test_distance_function", "distance", checkerQuiz("Distance Quiz", "2000", "assert distance(user_answer) == 2000"), "dist_user", "2km", true, ""},
		{"test_distance_function_cyrillic", "distance_cyrillic", checkerQuiz("Distance Quiz Cyrillic", "2000", "assert distance(user_answer) == 2000"), "dist_cyr", "2км", true, ""},
		{"test_direction_angle_function", "direction_angle", checkerQuiz("Direction Quiz", "2030", "assert direction_angle(user_answer) == 2030"), "dir_user", "20-30", true, ""},
		{"test_checker_error_message", "error_message", checkerQuiz("Error Message Quiz", "42", "assert to_int(user_answer) == 42, 'Expected 42'"), "err_user", "10", false, "Expected 42"},
		{"test_distance_invalid_format_error", "distance_invalid_format", checkerQuiz("Distance Error Quiz", "2000", "assert distance(user_answer) == 2000"), "dist_err", "abc", false, "*"},
	}
	for _, tc := range cases {
		// py: tests/test_text_input_questions.py::TestCheckerBuiltinFunctions::test_to_int_function
		// py: tests/test_text_input_questions.py::TestCheckerBuiltinFunctions::test_distance_function
		// py: tests/test_text_input_questions.py::TestCheckerBuiltinFunctions::test_distance_function_cyrillic
		// py: tests/test_text_input_questions.py::TestCheckerBuiltinFunctions::test_direction_angle_function
		// py: tests/test_text_input_questions.py::TestCheckerBuiltinFunctions::test_checker_error_message
		// py: tests/test_text_input_questions.py::TestCheckerBuiltinFunctions::test_distance_invalid_format_error
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := quizServer(t, tc.quiz)
			userID := ts.RegisterStudent(t, tc.user)
			data := answerOK(t, ts, userID, 1, tc.answer)
			if data["is_correct"] != tc.correct {
				t.Fatalf("is_correct: %v", data)
			}
			if tc.errorMsg == "" {
				return
			}
			msg, ok := data["checker_error"].(string)
			if !ok || (tc.errorMsg != "*" && !strings.Contains(msg, tc.errorMsg)) {
				t.Errorf("checker_error: %v", data["checker_error"])
			}
		})
	}
}
