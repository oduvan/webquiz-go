package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

func paFieldsServer(t *testing.T, quizzes map[string]any, fields ...any) *testutil.TestServer {
	t.Helper()
	if fields == nil {
		fields = []any{} // fields: [] in the YAML, as Python sent, not null
	}
	return testutil.StartServer(t, testutil.Options{
		Config:        map[string]any{"registration": map[string]any{"fields": fields}},
		Quizzes:       quizzes,
		ServerOptions: partAFast(),
	})
}

// py: tests/test_registration_fields.py::test_registration_with_no_fields
func TestRegistrationWithNoFields(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil)
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "testuser"})
	data := res.JSON(t)
	if res.Status != 200 || data["user_id"] == nil || data["username"] != "testuser" {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_fields.py::test_registration_with_single_field
func TestRegistrationWithSingleField(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil, "Grade")
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "student1", "grade": "10"})
	data := res.JSON(t)
	if res.Status != 200 || data["user_id"] == nil || data["username"] != "student1" {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_fields.py::test_registration_with_multiple_fields
func TestRegistrationWithMultipleFields(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil, "Grade", "School")
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "student2", "grade": "11", "school": "Central HS"})
	if res.Status != 200 || res.JSON(t)["user_id"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_fields.py::test_registration_field_validation
func TestRegistrationFieldValidation(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil, "Grade", "School")
	for _, body := range []map[string]any{
		{"username": "student3", "school": "Test School"}, // grade missing
		{"username": "student3", "grade": "9"},            // school missing
	} {
		res := ts.PostJSON(t, "/api/register", body)
		if res.Status != 400 || res.JSON(t)["error"] == nil {
			t.Errorf("%v: %d %s", body, res.Status, res.Body)
		}
	}
}

// py: tests/test_registration_fields.py::test_user_csv_creation
func TestUserCSVCreation(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil, "Grade", "School")
	partARegister(t, ts, map[string]any{"username": "student4", "grade": "12", "school": "North HS"})
	headers, _ := partAWaitCSV(t, ts, "test_quiz", ".users", 1)
	for _, name := range []string{"user_id", "username", "grade", "school", "registered_at", "total_questions_asked", "correct_answers", "total_time"} {
		if !partAHas(headers, name) {
			t.Errorf("header %q missing in %v", name, headers)
		}
	}
}

// py: tests/test_registration_fields.py::test_user_csv_content
func TestUserCSVContent(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil, "Grade")
	userID := partARegister(t, ts, map[string]any{"username": "student5", "grade": "9"})["user_id"].(string)
	_, rows := partAWaitCSV(t, ts, "test_quiz", ".users", 1)
	if len(rows) != 1 {
		t.Fatalf("rows: %v", rows)
	}
	row := rows[0]
	if row["user_id"] != userID || row["username"] != "student5" || row["grade"] != "9" ||
		row["total_questions_asked"] != "0" || row["correct_answers"] != "0" || row["total_time"] != "0:00" {
		t.Errorf("row: %v", row)
	}
}

// py: tests/test_registration_fields.py::test_user_csv_total_time_format
func TestUserCSVTotalTimeFormat(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, map[string]any{"test_quiz.yaml": map[string]any{
		"title":       "Time Test Quiz",
		"description": "Quiz for testing time format",
		"questions": []any{
			map[string]any{"question": "Q1?", "options": []any{"A", "B", "C", "D"}, "correct_answer": 0},
			map[string]any{"question": "Q2?", "options": []any{"A", "B", "C", "D"}, "correct_answer": 1},
			map[string]any{"question": "Q3?", "options": []any{"A", "B", "C", "D"}, "correct_answer": 2},
		},
	}})
	userID := ts.RegisterStudent(t, "timeuser")
	for q := 1; q <= 3; q++ {
		if res := partAAnswer(t, ts, userID, q, 1); res.Status != 200 {
			t.Fatalf("answer %d: %d %s", q, res.Status, res.Body)
		}
	}
	// Wait until all 3 answers are counted in the users CSV.
	var row map[string]string
	partAWaitFor(t, "3 answers in the users CSV", func() bool {
		_, rows := partAWaitCSV(t, ts, "test_quiz", ".users", 1)
		row = rows[0]
		return row["total_questions_asked"] == "3"
	})
	parts := strings.Split(row["total_time"], ":")
	if len(parts) != 2 {
		t.Fatalf("total_time should be MM:SS, got %q", row["total_time"])
	}
	minutes, seconds := parts[0], parts[1]
	if minutes == "" || strings.Trim(minutes, "0123456789") != "" {
		t.Errorf("minutes should be numeric, got %q", minutes)
	}
	if len(seconds) != 2 || strings.Trim(seconds, "0123456789") != "" || seconds > "59" {
		t.Errorf("seconds should be 2 digits, 00-59, got %q", seconds)
	}
}

// py: tests/test_registration_fields.py::test_answers_csv_uses_user_id
func TestAnswersCSVUsesUserID(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil)
	userID := ts.RegisterStudent(t, "answeruser")
	if res := partAAnswer(t, ts, userID, 1, 1); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	headers, rows := partAWaitCSV(t, ts, "test_quiz", "", 1)
	if !partAHas(headers, "user_id") || partAHas(headers, "username") {
		t.Errorf("headers: %v", headers)
	}
	if rows[0]["user_id"] != userID {
		t.Errorf("row: %v", rows[0])
	}
}

// py: tests/test_registration_fields.py::test_csv_naming_convention
func TestCSVNamingConvention(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil, "Grade")
	userID := partARegister(t, ts, map[string]any{"username": "pairtest", "grade": "10"})["user_id"].(string)
	partAAnswer(t, ts, userID, 1, 1)
	partAWaitCSV(t, ts, "test_quiz", ".users", 1)
	partAWaitCSV(t, ts, "test_quiz", "", 1)
	users := partACSVFiles(t, ts, "test_quiz_*.users.csv")
	answers := partAAnswerCSVs(t, ts, "test_quiz")
	if len(users) == 0 || len(answers) == 0 {
		t.Fatalf("users %v answers %v", users, answers)
	}
	number := func(name, suffix string) string {
		name = strings.TrimSuffix(name[strings.LastIndex(name, "_")+1:], suffix)
		return name
	}
	userNumber := number(users[0], ".users.csv")
	answerNumber := number(answers[0], ".csv")
	if len(userNumber) != 4 || userNumber != answerNumber {
		t.Errorf("CSV files should use the same 4-digit number: %s and %s", users[0], answers[0])
	}
}

// py: tests/test_registration_fields.py::test_user_id_format
func TestUserIDFormat(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil)
	userID := ts.RegisterStudent(t, "formattest")
	if len(userID) != 6 || strings.Trim(userID, "0123456789") != "" || userID < "100000" {
		t.Errorf("user_id %q is not 6 digits in 100000..999999", userID)
	}
}

// py: tests/test_registration_fields.py::test_user_id_uniqueness
func TestUserIDUniqueness(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil)
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		userID := ts.RegisterStudent(t, "user"+string(rune('0'+i)))
		if seen[userID] {
			t.Fatalf("duplicate user_id: %s", userID)
		}
		seen[userID] = true
	}
	if len(seen) != 10 {
		t.Errorf("%d ids", len(seen))
	}
}

// py: tests/test_registration_fields.py::test_registration_with_cyrillic_field_labels
func TestRegistrationWithCyrillicFieldLabels(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, nil, "Клас", "Школа")
	userID := partARegister(t, ts, map[string]any{"username": "ukrainian_student", "клас": "8", "школа": "Гімназія №5"})["user_id"].(string)
	headers, rows := partAWaitCSV(t, ts, "test_quiz", ".users", 1)
	if !partAHas(headers, "клас") || !partAHas(headers, "школа") {
		t.Errorf("headers: %v", headers)
	}
	found := false
	for _, row := range rows {
		if row["user_id"] == userID {
			found = true
			if row["клас"] != "8" || row["школа"] != "Гімназія №5" {
				t.Errorf("row: %v", row)
			}
		}
	}
	if !found {
		t.Errorf("no row for %s in %v", userID, rows)
	}
}

// py: tests/test_registration_fields.py::test_user_csv_statistics
func TestUserCSVStatistics(t *testing.T) {
	t.Parallel()
	ts := paFieldsServer(t, map[string]any{"stats_quiz.yaml": map[string]any{
		"title": "Statistics Test Quiz",
		"questions": []any{
			map[string]any{"question": "Question 1", "options": []any{"A", "B", "C"}, "correct_answer": 0},
			map[string]any{"question": "Question 2", "options": []any{"A", "B", "C"}, "correct_answer": 1},
			map[string]any{"question": "Question 3", "options": []any{"A", "B", "C"}, "correct_answer": 2},
		},
	}})
	userID := ts.RegisterStudent(t, "stats_user")
	for q, answer := range []int{0, 0, 2} { // correct, wrong, correct
		if res := partAAnswer(t, ts, userID, q+1, answer); res.Status != 200 {
			t.Fatalf("answer %d: %d %s", q+1, res.Status, res.Body)
		}
	}
	var row map[string]string
	partAWaitFor(t, "3 answers in the users CSV", func() bool {
		_, rows := partAWaitCSV(t, ts, "stats_quiz", ".users", 1)
		row = rows[0]
		return row["total_questions_asked"] == "3"
	})
	if row["user_id"] != userID || row["username"] != "stats_user" || row["total_questions_asked"] != "3" || row["correct_answers"] != "2" {
		t.Errorf("row: %v", row)
	}
}
