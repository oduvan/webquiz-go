package server_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_quiz_start_settings.py. The old tests looked for
// "let showRightAnswer = false" in the quiz page; the new page carries the
// same values in its boot data (show_right_answer, show_final_list).

func startSettingsQuiz(settings map[string]any) map[string]any {
	quiz := map[string]any{"title": "Start Settings", "questions": []any{
		map[string]any{"question": "Q1?", "options": []any{"A", "B"}, "correct_answer": 0},
		map[string]any{"question": "Q2?", "options": []any{"A", "B"}, "correct_answer": 0},
	}}
	for key, value := range settings {
		quiz[key] = value
	}
	return quiz
}

// startQuiz is switch-quiz with optional settings.
func startQuiz(t *testing.T, admin *testutil.Client, filename string, settings map[string]any) *testutil.Response {
	t.Helper()
	body := map[string]any{"quiz_filename": filename}
	if settings != nil {
		body["settings"] = settings
	}
	return admin.PostJSON(t, "/api/admin/switch-quiz", body)
}

func quizPageBoot(t *testing.T, ts *testutil.TestServer) map[string]any {
	t.Helper()
	return pageBoot(t, ts.GetJSON(t, "/").Text())
}

// questionStartReply is the reply of POST /api/question-start.
func questionStartReply(t *testing.T, ts *testutil.TestServer, userID string, questionID any) map[string]any {
	t.Helper()
	res := ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": questionID})
	if res.Status != 200 {
		t.Fatalf("question-start: %d %s", res.Status, res.Body)
	}
	return res.JSON(t)
}

func remaining(t *testing.T, reply map[string]any) float64 {
	t.Helper()
	number, ok := reply["remaining_seconds"].(json.Number)
	if !ok {
		t.Fatalf("no remaining_seconds in %v", reply)
	}
	value, _ := number.Float64()
	return value
}

// py: tests/test_quiz_start_settings.py::test_start_settings_apply_to_running_quiz
func TestStartSettingsApplyToRunningQuiz(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"q.yaml": startSettingsQuiz(map[string]any{"show_right_answer": true, "show_final_list": true})}})
	res := startQuiz(t, admin, "q.yaml", map[string]any{
		"show_right_answer": false, "show_answers_on_completion": true, "randomize_questions": true,
		"show_final_list": false, "time_limit": map[string]any{"per_question": 30},
	})
	reply := res.JSON(t)
	if res.Status != 200 || !reflect.DeepEqual(reply["settings"].(map[string]any)["time_limit"], map[string]any{"per_question": json.Number("30")}) {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	boot := quizPageBoot(t, ts)
	if boot["show_right_answer"] != false || boot["show_final_list"] != false {
		t.Errorf("page values: show_right_answer %v, show_final_list %v", boot["show_right_answer"], boot["show_final_list"])
	}
	registration := ts.PostJSON(t, "/api/register", map[string]any{"username": "S"}).JSON(t)
	order, ok := registration["question_order"].([]any) // randomized
	if !ok || len(order) == 0 {
		t.Fatalf("no question_order in %v", registration)
	}
	if left := remaining(t, questionStartReply(t, ts, registration["user_id"].(string), order[0])); left <= 29 || left > 30 {
		t.Errorf("remaining_seconds %v, want 29..30", left)
	}
	if admin.GetJSON(t, "/api/admin/list-quizzes").JSON(t)["show_answers_on_completion"] != true {
		t.Error("list-quizzes does not show show_answers_on_completion")
	}
}

// py: tests/test_quiz_start_settings.py::test_start_settings_are_not_written_to_the_quiz_file
func TestStartSettingsAreNotWrittenToTheQuizFile(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"q.yaml": startSettingsQuiz(map[string]any{"show_right_answer": true})}})
	before := readQuizFile(t, ts, "q.yaml")
	res := startQuiz(t, admin, "q.yaml", map[string]any{"show_right_answer": false, "time_limit": map[string]any{"whole_quiz": 60}})
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if after := readQuizFile(t, ts, "q.yaml"); !reflect.DeepEqual(after, before) {
		t.Errorf("the quiz file changed:\nbefore %v\nafter  %v", before, after)
	}
}

// py: tests/test_quiz_start_settings.py::test_missing_settings_keep_the_quiz_file_values
func TestMissingSettingsKeepTheQuizFileValues(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"q.yaml": startSettingsQuiz(map[string]any{
		"show_final_list": false, "time_limit": map[string]any{"per_question": 45},
	})}})
	if res := startQuiz(t, admin, "q.yaml", map[string]any{"show_right_answer": false}); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if quizPageBoot(t, ts)["show_final_list"] != false {
		t.Error("show_final_list from the file was lost")
	}
	userID := ts.RegisterStudent(t, "S")
	if left := remaining(t, questionStartReply(t, ts, userID, 1)); left <= 44 || left > 45 {
		t.Errorf("remaining_seconds %v, want 44..45", left)
	}
}

// py: tests/test_quiz_start_settings.py::test_time_limit_null_turns_the_limit_off
func TestTimeLimitNullTurnsTheLimitOff(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"q.yaml": startSettingsQuiz(map[string]any{"time_limit": map[string]any{"per_question": 30}})}})
	if res := startQuiz(t, admin, "q.yaml", map[string]any{"time_limit": nil}); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	userID := ts.RegisterStudent(t, "S")
	if reply := questionStartReply(t, ts, userID, 1); !reflect.DeepEqual(reply, map[string]any{"status": "success"}) {
		t.Errorf("question-start: %v", reply)
	}
}

// py: tests/test_quiz_start_settings.py::test_restart_of_the_same_quiz_keeps_the_start_settings
//
// Saving the config restarts the running quiz; the start settings stay.
func TestRestartOfTheSameQuizKeepsTheStartSettings(t *testing.T) {
	t.Parallel()
	quiz := startSettingsQuiz(map[string]any{"show_right_answer": true})
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"q.yaml": quiz}})
	if res := startQuiz(t, admin, "q.yaml", map[string]any{"show_right_answer": false}); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	// Not in the Python test: a student registered before the save is gone
	// after it, so the save really restarted the quiz.
	userID := ts.PostJSON(t, "/api/register", map[string]any{"username": "Before"}).JSON(t)["user_id"].(string)
	saved := admin.Put(t, "/api/admin/config", map[string]any{"content": "registration:\n  fields:\n    - \"Grade\"\n"})
	if saved.Status != 200 {
		t.Fatalf("config save: %d %s", saved.Status, saved.Body)
	}
	if quizPageBoot(t, ts)["show_right_answer"] != false {
		t.Error("the restart dropped the start settings")
	}
	if verify := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t); verify["valid"] != false {
		t.Errorf("the config save did not restart the quiz: %v", verify)
	}
}

// py: tests/test_quiz_start_settings.py::test_another_quiz_without_settings_uses_its_file_values
func TestAnotherQuizWithoutSettingsUsesItsFileValues(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"a.yaml": startSettingsQuiz(map[string]any{"show_right_answer": true}),
		"b.yaml": startSettingsQuiz(map[string]any{"show_right_answer": true}),
	}})
	if res := startQuiz(t, admin, "a.yaml", map[string]any{"show_right_answer": false}); res.Status != 200 {
		t.Fatalf("a: %d %s", res.Status, res.Body)
	}
	if res := startQuiz(t, admin, "b.yaml", nil); res.Status != 200 {
		t.Fatalf("b: %d %s", res.Status, res.Body)
	}
	if quizPageBoot(t, ts)["show_right_answer"] != true {
		t.Error("b.yaml did not use its own value")
	}
}

// py: tests/test_quiz_start_settings.py::test_invalid_start_settings_are_rejected_and_quiz_is_not_switched
func TestInvalidStartSettingsAreRejectedAndQuizIsNotSwitched(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"a.yaml": startSettingsQuiz(nil), "b.yaml": startSettingsQuiz(nil)}})
	if res := startQuiz(t, admin, "a.yaml", nil); res.Status != 200 {
		t.Fatalf("a: %d", res.Status)
	}
	for _, bad := range []map[string]any{
		{"show_right_answer": "no"},
		{"randomize_questions": 1},
		{"time_limit": map[string]any{"per_question": -1}},
		{"time_limit": map[string]any{"per_question": 5, "whole_quiz": 5}},
		{"title": "Not a start setting"},
	} {
		res := startQuiz(t, admin, "b.yaml", bad)
		errors, _ := res.JSON(t)["errors"].([]any)
		if res.Status != 400 || len(errors) == 0 {
			t.Errorf("%v: %d %s", bad, res.Status, res.Body)
		}
	}
	if current := admin.GetJSON(t, "/api/admin/list-quizzes").JSON(t)["current_quiz"]; current != "a.yaml" {
		t.Errorf("current_quiz %v, want a.yaml", current)
	}
}
