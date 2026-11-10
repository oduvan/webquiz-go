package server_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

func answersWithUsersQuiz() map[string]any {
	return map[string]any{"default.yaml": map[string]any{
		"title":             "Answers-with-users CSV Quiz",
		"show_right_answer": true,
		"questions": []any{
			map[string]any{"question": "Q1?", "options": []any{"a", "b", "c", "d"}, "correct_answer": 1},
			map[string]any{"question": "Q2?", "options": []any{"w", "x", "y", "z"}, "correct_answer": 0},
		},
	}}
}

func extraCSVServer(t *testing.T, config map[string]any) *testutil.TestServer {
	t.Helper()
	return testutil.StartServer(t, testutil.Options{Config: config, Quizzes: answersWithUsersQuiz(), ServerOptions: partAFast()})
}

func submitAll(t *testing.T, ts *testutil.TestServer, userID string, answers [][2]int) {
	t.Helper()
	for _, a := range answers {
		if res := partAAnswer(t, ts, userID, a[0], a[1]); res.Status != 200 {
			t.Fatalf("answer %v: %d %s", a, res.Status, res.Body)
		}
	}
}

// py: tests/test_answers_with_users_csv.py::test_answers_with_users_csv_not_created_when_disabled
func TestAnswersWithUsersCSVNotCreatedWhenDisabled(t *testing.T) {
	t.Parallel()
	ts := extraCSVServer(t, nil)
	userID := ts.RegisterStudent(t, "alice")
	submitAll(t, ts, userID, [][2]int{{1, 1}, {2, 0}})
	partAWaitCSV(t, ts, "default", "", 2) // the regular answers CSV exists and was flushed
	time.Sleep(200 * time.Millisecond)    // several more flushes
	if files := partACSVFiles(t, ts, "*.answers_with_users.csv"); len(files) != 0 {
		t.Errorf("extra answers_with_users CSV must not be created when disabled: %v", files)
	}
}

// py: tests/test_answers_with_users_csv.py::test_answers_with_users_csv_created_when_enabled
func TestAnswersWithUsersCSVCreatedWhenEnabled(t *testing.T) {
	t.Parallel()
	ts := extraCSVServer(t, map[string]any{"extra_answers_with_users_csv": true})
	userID := ts.RegisterStudent(t, "bob")
	submitAll(t, ts, userID, [][2]int{{1, 1}, {2, 0}})
	headers, rows := partAWaitCSV(t, ts, "default", ".answers_with_users", 2)
	if want := []string{"user_id", "username", "question", "selected_answer", "correct_answer", "is_correct", "time_taken_seconds"}; !slices.Equal(headers, want) {
		t.Errorf("headers %v", headers)
	}
	if len(rows) != 2 {
		t.Fatalf("rows %v", rows)
	}
	var questions []string
	for _, row := range rows {
		if row["user_id"] != userID || row["username"] != "bob" {
			t.Errorf("row %v", row)
		}
		questions = append(questions, row["question"])
	}
	slices.Sort(questions)
	if !slices.Equal(questions, []string{"Q1?", "Q2?"}) {
		t.Errorf("questions %v", questions)
	}
	if files := partACSVFiles(t, ts, "default_0001.answers_with_users.csv"); len(files) != 1 {
		t.Errorf("the extra file should be default_0001.answers_with_users.csv: %v", files)
	}
}

// py: tests/test_answers_with_users_csv.py::test_answers_with_users_csv_includes_registration_fields
func TestAnswersWithUsersCSVIncludesRegistrationFields(t *testing.T) {
	t.Parallel()
	ts := extraCSVServer(t, map[string]any{
		"extra_answers_with_users_csv": true,
		"registration":                 map[string]any{"fields": []any{"School", "Grade Level"}},
	})
	userID := partARegister(t, ts, map[string]any{"username": "carol", "school": "Lyceum 1", "grade_level": "10-B"})["user_id"].(string)
	submitAll(t, ts, userID, [][2]int{{1, 1}, {2, 0}})
	headers, rows := partAWaitCSV(t, ts, "default", ".answers_with_users", 2)
	want := []string{"user_id", "username", "school", "grade_level", "question", "selected_answer", "correct_answer", "is_correct", "time_taken_seconds"}
	if !slices.Equal(headers, want) || len(rows) != 2 {
		t.Fatalf("headers %v rows %v", headers, rows)
	}
	for _, row := range rows {
		if row["username"] != "carol" || row["school"] != "Lyceum 1" || row["grade_level"] != "10-B" {
			t.Errorf("row %v", row)
		}
	}
}

// py: tests/test_answers_with_users_csv.py::test_answers_with_users_csv_appends_across_flushes
func TestAnswersWithUsersCSVAppendsAcrossFlushes(t *testing.T) {
	t.Parallel()
	ts := extraCSVServer(t, map[string]any{"extra_answers_with_users_csv": true})
	user1 := ts.RegisterStudent(t, "user1")
	submitAll(t, ts, user1, [][2]int{{1, 1}})
	partAWaitCSV(t, ts, "default", ".answers_with_users", 1) // the first flush happened
	user2 := ts.RegisterStudent(t, "user2")
	submitAll(t, ts, user2, [][2]int{{1, 0}, {2, 0}})
	_, rows := partAWaitCSV(t, ts, "default", ".answers_with_users", 3)
	names := map[string]bool{}
	for _, row := range rows {
		names[row["username"]] = true
	}
	if len(rows) != 3 || len(names) != 2 || !names["user1"] || !names["user2"] {
		t.Errorf("1 row from user1 + 2 rows from user2 expected, got %v", rows)
	}
	time.Sleep(100 * time.Millisecond) // later flushes must not add copies
	if _, again := partAWaitCSV(t, ts, "default", ".answers_with_users", 3); len(again) != 3 {
		t.Errorf("rows after more flushes: %d", len(again))
	}
}

// py: tests/test_answers_with_users_csv.py::test_answers_with_users_csv_config_validation_rejects_non_bool
func TestAnswersWithUsersCSVConfigValidationRejectsNonBool(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: answersWithUsersQuiz()})
	res := ts.Put(t, "/api/admin/config", map[string]any{"content": "extra_answers_with_users_csv: \"yes\"\n"},
		testutil.WithCookies(ts.AdminCookies(t, "test123")...))
	skipWithoutConfigSave(t, res)
	found := false
	if errors, ok := res.JSON(t)["errors"].([]any); ok {
		for _, item := range errors {
			if s, _ := item.(string); strings.Contains(s, "extra_answers_with_users_csv") {
				found = true
			}
		}
	}
	if res.Status != 400 || !found {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}
