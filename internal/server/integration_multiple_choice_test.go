package server_test

import (
	"strings"
	"testing"
)

func multipleChoiceQuiz() map[string]any {
	return map[string]any{
		"title":             "Integration Test Quiz",
		"show_right_answer": true,
		"questions": []any{
			map[string]any{"question": "Single answer: What is 2+2?", "options": []any{"3", "4", "5", "6"}, "correct_answer": 1},
			map[string]any{
				"question": "Multiple answers: Which are programming languages?",
				"options":  []any{"Python", "HTML", "JavaScript", "CSS"}, "correct_answer": []any{0, 2},
			},
			map[string]any{
				"question": "Minimum correct: Select at least 2 colors",
				"options":  []any{"Red", "Green", "Blue", "Yellow"}, "correct_answer": []any{0, 2, 3}, "min_correct": 2,
			},
		},
	}
}

func TestMultipleChoiceIntegration(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, multipleChoiceQuiz())

	// py: tests/test_integration_multiple_choice.py::TestMultipleChoiceIntegration::test_user_registration_and_progress
	t.Run("user_registration_and_progress", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "testuser")
		res := ts.GetJSON(t, "/api/verify-user/"+userID)
		if res.Status != 200 || res.JSON(t)["username"] != "testuser" {
			t.Errorf("verify-user: %d %s", res.Status, res.Body)
		}
	})

	// py: tests/test_integration_multiple_choice.py::TestMultipleChoiceIntegration::test_mixed_question_types_submission
	t.Run("mixed_question_types_submission", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "mixed")
		if data := answerOK(t, ts, userID, 1, 1); data["is_correct"] != true {
			t.Errorf("single answer: %v", data)
		}
		if data := answerOK(t, ts, userID, 2, []any{0, 2}); data["is_correct"] != true || data["is_multiple_choice"] != true {
			t.Errorf("multiple answers: %v", data)
		}
		if data := answerOK(t, ts, userID, 3, []any{0, 2}); data["is_correct"] != true { // 2 out of 3 correct
			t.Errorf("minimum correct: %v", data)
		}
	})

	// py: tests/test_integration_multiple_choice.py::TestMultipleChoiceIntegration::test_csv_export_format
	// The Python test only checked the replies (it says so in a comment). The
	// Go port also reads the answers CSV, where a list is written as the
	// option texts joined with "|".
	t.Run("csv_export_format", func(t *testing.T) {
		ts := quizServer(t, multipleChoiceQuiz()) // its own server: reading the CSV closes it
		userID := ts.RegisterStudent(t, "csvuser")
		if data := answerOK(t, ts, userID, 1, 1); data["is_correct"] != true {
			t.Errorf("single answer: %v", data)
		}
		if data := answerOK(t, ts, userID, 2, []any{0, 2}); data["is_correct"] != true || data["is_multiple_choice"] != true {
			t.Errorf("multiple answers: %v", data)
		}
		text := csvFileEnding(t, ts, "_0001.csv")
		var found bool
		for _, row := range csvRecords(t, text) {
			if row["user_id"] == userID && row["question"] == "Multiple answers: Which are programming languages?" {
				found = true
				if row["selected_answer"] != "Python|JavaScript" || row["is_correct"] != "True" {
					t.Errorf("multiple answer row: %v", row)
				}
			}
		}
		if !found {
			t.Errorf("no row of the multiple choice answer:\n%s", text)
		}
	})

	// py: tests/test_integration_multiple_choice.py::TestMultipleChoiceIntegration::test_incorrect_multiple_answers
	t.Run("incorrect_multiple_answers", func(t *testing.T) {
		ts := quizServer(t, multipleChoiceQuiz())
		userID := ts.RegisterStudent(t, "testuser")
		if data := answerOK(t, ts, userID, 2, []any{0}); data["is_correct"] != false { // only Python, JavaScript is missing
			t.Errorf("partial answer: %v", data)
		}
		// Another user, because a second, different answer to the same question is rejected.
		second := ts.RegisterStudent(t, "testuser2")
		if data := answerOK(t, ts, second, 2, []any{0, 1, 2}); data["is_correct"] != false { // includes HTML
			t.Errorf("answer with a wrong option: %v", data)
		}
		if data := answerOK(t, ts, userID, 3, []any{0}); data["is_correct"] != false { // 1 selected, 2 needed
			t.Errorf("less than the minimum: %v", data)
		}
	})

	// py: tests/test_integration_multiple_choice.py::TestMultipleChoiceIntegration::test_question_data_sent_to_client
	t.Run("question_data_sent_to_client", func(t *testing.T) {
		res := ts.GetJSON(t, "/")
		if res.Status != 200 {
			t.Fatalf("GET /: %d", res.Status)
		}
		html := res.Text()
		for _, text := range []string{"Single answer: What is 2+2?", "Multiple answers: Which are programming languages?", "Minimum correct: Select at least 2 colors"} {
			if !strings.Contains(html, text) {
				t.Errorf("the page does not hold %q", text)
			}
		}
		// show_right_answer is "show_right_answer" in the boot JSON.
		if boot := pageBoot(t, html); boot["show_right_answer"] != true {
			t.Errorf("show_right_answer in the boot data: %v", boot["show_right_answer"])
		}
	})
}

// py: tests/test_integration_multiple_choice.py::TestMultipleChoiceIntegration::test_backward_compatibility_existing_quiz
func TestBackwardCompatibilityExistingQuiz(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, map[string]any{"title": "Traditional Quiz", "questions": []any{
		map[string]any{"question": "What is the capital of France?", "options": []any{"London", "Berlin", "Paris", "Madrid"}, "correct_answer": 2},
		map[string]any{"question": "Which language is Python?", "options": []any{"Programming", "Spoken", "Dead"}, "correct_answer": 0},
	}})
	userID := ts.RegisterStudent(t, "traditionaluser")
	if data := answerOK(t, ts, userID, 1, 2); data["is_correct"] != true {
		t.Errorf("reply: %v", data)
	}
	res := ts.GetJSON(t, "/")
	if res.Status != 200 || !strings.Contains(res.Text(), "What is the capital of France?") {
		t.Errorf("GET /: %d", res.Status)
	}
}
