package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_quiz_validation_edge_cases.py: each quiz goes through
// POST /api/admin/validate-quiz and must come back invalid with an error
// that has the wanted words. "any" lists alternatives (Python wrote
// "a in error or b in error.lower()"); within one alternative all words
// must be in the same error.
type validationCase struct {
	py      string // the Python test id
	name    string
	content string
	any     [][]string
}

var validationCases = []validationCase{
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_invalid_data_type_not_dict", name: "invalid_data_type_not_dict", content: `- invalid
- structure`, any: [][]string{{"словником"}, {"dictionary"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_missing_questions_field", name: "missing_questions_field", content: `title: No Questions Quiz
description: This quiz has no questions field`, any: [][]string{{"questions"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_questions_not_list", name: "questions_not_list", content: `title: Invalid Questions Type
questions: "not a list"  # Should be array`, any: [][]string{{"списком"}, {"list"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_empty_questions_array", name: "empty_questions_array", content: `title: Empty Questions
questions: []`, any: [][]string{{"принаймні одне питання"}, {"at least"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_question_not_dict", name: "question_not_dict", content: `title: Invalid Question Type
questions:
  - "string instead of object"
  - question: Valid question
    options: ['A', 'B']
    correct_answer: 0`, any: [][]string{{"dictionary"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_missing_required_fields", name: "missing_required_fields_1", content: `title: Missing Options
questions:
  - question: Where are options?
    correct_answer: 0`, any: [][]string{{"options"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_missing_required_fields", name: "missing_required_fields_2", content: `title: Missing Correct Answer
questions:
  - question: What's the answer?
    options: ['A', 'B', 'C']`, any: [][]string{{"correct_answer"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_no_question_text_or_image", name: "no_question_text_or_image", content: `title: No Question Content
questions:
  - options: ['A', 'B']
    correct_answer: 0`, any: [][]string{{}, {}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_options_not_list", name: "options_not_list", content: `title: Options Not List
questions:
  - question: Test?
    options: "not a list"
    correct_answer: 0`, any: [][]string{{"options must be a list"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_options_too_few", name: "options_too_few", content: `title: Too Few Options
questions:
  - question: Only one option?
    options: ['A']
    correct_answer: 0`, any: [][]string{{"at least 2 options"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_options_not_all_strings", name: "options_not_all_strings", content: `title: Non-String Options
questions:
  - question: Test?
    options: ['A', 123, 'C']  # 123 is not a string
    correct_answer: 0`, any: [][]string{{"all options must be strings"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_correct_answer_out_of_range", name: "correct_answer_out_of_range", content: `title: Answer Out of Range
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 5  # Index 5 doesn't exist`, any: [][]string{{"out of range"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_correct_answer_array_empty", name: "correct_answer_array_empty", content: `title: Empty Answer Array
questions:
  - question: Test?
    options: ['A', 'B', 'C']
    correct_answer: []  # Empty array`, any: [][]string{{"cannot be empty"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_correct_answer_array_non_integers", name: "correct_answer_array_non_integers", content: `title: Non-Integer Answers
questions:
  - question: Test?
    options: ['A', 'B', 'C']
    correct_answer: [0, "1", 2]  # "1" is string`, any: [][]string{{"only integers"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_correct_answer_array_out_of_range", name: "correct_answer_array_out_of_range", content: `title: Answer Index Out of Range
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: [0, 5]  # 5 is out of range`, any: [][]string{{"out of range"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_correct_answer_array_duplicates", name: "correct_answer_array_duplicates", content: `title: Duplicate Answer Indices
questions:
  - question: Test?
    options: ['A', 'B', 'C']
    correct_answer: [0, 1, 0]  # 0 appears twice`, any: [][]string{{"duplicate"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_correct_answer_wrong_type", name: "correct_answer_wrong_type", content: `title: Wrong Answer Type
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: "zero"  # String instead of int`, any: [][]string{{}, {}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_min_correct_without_correct_answer", name: "min_correct_without_correct_answer", content: `title: Min Correct Without Answer
questions:
  - question: Test?
    options: ['A', 'B', 'C']
    min_correct: 2`, any: [][]string{{"min_correct but no correct_answer"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_min_correct_with_single_answer", name: "min_correct_with_single_answer", content: `title: Min Correct With Single Answer
questions:
  - question: Test?
    options: ['A', 'B', 'C']
    correct_answer: 1  # Single answer
    min_correct: 1  # min_correct only for multiple answers`, any: [][]string{{"only valid for multiple answer"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_min_correct_not_integer", name: "min_correct_not_integer", content: `title: Min Correct Not Integer
questions:
  - question: Test?
    options: ['A', 'B', 'C']
    correct_answer: [0, 1, 2]
    min_correct: "two"  # String instead of int`, any: [][]string{{"min_correct must be an integer"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_min_correct_too_low", name: "min_correct_too_low", content: `title: Min Correct Too Low
questions:
  - question: Test?
    options: ['A', 'B', 'C']
    correct_answer: [0, 1]
    min_correct: 0  # Must be at least 1`, any: [][]string{{"at least 1"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_min_correct_exceeds_answers", name: "min_correct_exceeds_answers", content: `title: Min Correct Exceeds Answers
questions:
  - question: Test?
    options: ['A', 'B', 'C', 'D']
    correct_answer: [0, 1]  # 2 correct answers
    min_correct: 5  # Requires 5 but only 2 exist`, any: [][]string{{"cannot exceed"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_show_right_answer_not_boolean", name: "show_right_answer_not_boolean", content: `title: Invalid Show Right Answer Type
show_right_answer: "yes"  # Should be boolean
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0`, any: [][]string{{"show_right_answer", "boolean"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_randomize_questions_not_boolean", name: "randomize_questions_not_boolean", content: `title: Invalid Randomize Type
randomize_questions: 1  # Should be boolean
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0`, any: [][]string{{"randomize_questions", "boolean"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_show_answers_on_completion_not_boolean", name: "show_answers_on_completion_not_boolean", content: `title: Invalid Show Answers On Completion Type
show_right_answer: false
show_answers_on_completion: "false"  # Should be boolean
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0`, any: [][]string{{"show_answers_on_completion", "boolean"}}},
	{py: "tests/test_quiz_validation_edge_cases.py::test_validate_quiz_title_not_string", name: "title_not_string", content: `title: 123  # Number instead of string
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0`, any: [][]string{{"title", "string"}}},
}

func TestValidateQuizEdgeCases(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	for _, c := range validationCases {
		t.Run(c.name, func(t *testing.T) {
			data := validate(t, admin, c.content)
			if data["valid"] != false {
				t.Fatalf("valid: %v, want false (%s)", data["valid"], c.py)
			}
			for _, message := range errorList(data) {
				lower := strings.ToLower(message)
				for _, words := range c.any {
					found := true
					for _, word := range words {
						if !strings.Contains(lower, word) {
							found = false
						}
					}
					if found {
						return
					}
				}
			}
			t.Errorf("no error with %v in %v", c.any, errorList(data))
		})
	}
}

// py: tests/test_quiz_validation_edge_cases.py::test_create_quiz_rejects_non_boolean_show_answers_on_completion
func TestCreateQuizRejectsNonBooleanShowAnswersOnCompletion(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{
		"filename": "bad_setting", "mode": "text",
		"content": `title: Bad Setting
show_right_answer: false
show_answers_on_completion: "yes"
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0`,
	})
	if res.Status != 400 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	for _, item := range admin.GetJSON(t, "/api/admin/list-quizzes").JSON(t)["quizzes"].([]any) {
		if item.(map[string]any)["filename"] == "bad_setting.yaml" {
			t.Error("bad_setting.yaml was created")
		}
	}
}
