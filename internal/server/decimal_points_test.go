package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

func decimalPointsQuiz() map[string]any {
	return map[string]any{
		"title":             "Decimal Points Quiz",
		"show_right_answer": true,
		"questions": []any{
			map[string]any{"question": "Half a point", "options": []any{"A", "B"}, "correct_answer": 0, "points": 0.5},
			map[string]any{"question": "One and a quarter", "options": []any{"A", "B"}, "correct_answer": 1, "points": 1.25},
			map[string]any{"question": "Two and a quarter", "options": []any{"A", "B"}, "correct_answer": 0, "points": 2.25},
		},
	}
}

func TestDecimalPointsScoring(t *testing.T) {
	t.Parallel()

	// py: tests/test_decimal_points.py::TestDecimalPointsScoring::test_decimal_points_sent_to_client
	t.Run("decimal_points_sent_to_client", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, decimalPointsQuiz())
		boot := pageBoot(t, ts.GetJSON(t, "/").Text())
		for i, want := range []string{"0.5", "1.25", "2.25"} {
			numIs(t, "points", question(t, boot, i)["points"], want)
		}
	})

	// py: tests/test_decimal_points.py::TestDecimalPointsScoring::test_all_correct_earns_decimal_total
	t.Run("all_correct_earns_decimal_total", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, decimalPointsQuiz())
		userID := ts.RegisterStudent(t, "decimal_all")
		for _, a := range [][2]int{{1, 0}, {2, 1}, {3, 0}} {
			if data := answerOK(t, ts, userID, a[0], a[1]); data["is_correct"] != true {
				t.Errorf("question %d: %v", a[0], data)
			}
		}
		fr := replyFinalResults(t, ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t))
		numIs(t, "total_points", fr["total_points"], "4")
		numIs(t, "earned_points", fr["earned_points"], "4")
		numIs(t, "points_percentage", fr["points_percentage"], "100")
	})

	// py: tests/test_decimal_points.py::TestDecimalPointsScoring::test_partial_score_has_no_float_noise
	t.Run("partial_score_has_no_float_noise", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, decimalPointsQuiz())
		userID := ts.RegisterStudent(t, "decimal_part")
		for _, a := range [][2]int{{1, 0}, {2, 1}, {3, 1}} { // 0.5 + 1.25 right, the third wrong
			answerOK(t, ts, userID, a[0], a[1])
		}
		fr := replyFinalResults(t, ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t))
		numIs(t, "earned_points", fr["earned_points"], "1.75")
		numIs(t, "total_points", fr["total_points"], "4")
		numIs(t, "points_percentage", fr["points_percentage"], "44") // 43.75 rounded
	})

	// py: tests/test_decimal_points.py::TestDecimalPointsScoring::test_sum_of_inexact_points_is_rounded
	t.Run("sum_of_inexact_points_is_rounded", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, map[string]any{"title": "Inexact Points Quiz", "show_right_answer": true, "questions": []any{
			map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0, "points": 0.1},
			map[string]any{"question": "Q2", "options": []any{"A", "B"}, "correct_answer": 0, "points": 0.2},
			map[string]any{"question": "Q3", "options": []any{"A", "B"}, "correct_answer": 0, "points": 0.7},
		}})
		userID := ts.RegisterStudent(t, "inexact")
		for _, a := range [][2]int{{1, 0}, {2, 0}, {3, 1}} { // 0.1 + 0.2 right, the last wrong
			answerOK(t, ts, userID, a[0], a[1])
		}
		res := ts.GetJSON(t, "/api/verify-user/"+userID)
		fr := replyFinalResults(t, res.JSON(t))
		numIs(t, "earned_points", fr["earned_points"], "0.3") // the number as written: 0.3, not 0.30000000000000004
		if !strings.Contains(res.Text(), `"earned_points": 0.3`) && !strings.Contains(res.Text(), `"earned_points":0.3`) {
			t.Errorf("the reply text must hold earned_points 0.3: %s", res.Body)
		}
		numIs(t, "total_points", fr["total_points"], "1")
		numIs(t, "points_percentage", fr["points_percentage"], "30")
	})

	// py: tests/test_decimal_points.py::TestDecimalPointsScoring::test_users_csv_contains_decimal_points
	// The Python test slept 7 s for the CSV flush; closing the server writes the rows.
	t.Run("users_csv_contains_decimal_points", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, decimalPointsQuiz())
		userID := ts.RegisterStudent(t, "decimal_csv")
		for _, a := range [][2]int{{1, 0}, {2, 1}, {3, 1}} {
			answerOK(t, ts, userID, a[0], a[1])
		}
		var row map[string]string
		for _, r := range csvRecords(t, csvFileEnding(t, ts, ".users.csv")) {
			if r["username"] == "decimal_csv" {
				row = r
			}
		}
		if row == nil {
			t.Fatal("the student is not in the users CSV")
		}
		if row["earned_points"] != "1.75" || row["total_points"] != "4" {
			t.Errorf("points in the CSV: earned %q total %q", row["earned_points"], row["total_points"])
		}
	})
}

// py: tests/test_decimal_points.py::test_live_stats_broadcast_carries_decimal_points
func TestLiveStatsBroadcastCarriesDecimalPoints(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, map[string]any{"title": "Decimal Points Quiz", "questions": []any{
		map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0, "points": 0.5},
		map[string]any{"question": "Q2", "options": []any{"A", "B"}, "correct_answer": 1, "points": 1.25},
	}})
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // initial_state
	userID := ts.RegisterStudent(t, "wsuser")
	answerOK(t, ts, userID, 2, 1)
	var update map[string]any
	for update == nil {
		if message := live.next(); message["type"] == "state_update" {
			update = message
		}
	}
	if update["question_points"] != 1.25 || update["earned_points"] != 1.25 || update["total_points"] != 1.75 {
		t.Errorf("state_update: %v", update)
	}
}

// pointsQuizOf is a quiz whose only question has the given points value.
func pointsQuizOf(points any) map[string]any {
	return map[string]any{"title": "Points Quiz", "questions": []any{
		map[string]any{"question": "Test question", "options": []any{"A", "B"}, "correct_answer": 0, "points": points},
	}}
}

// createQuiz asks the admin API to make a quiz file.
func createQuiz(t *testing.T, admin *testutil.Client, filename string, quiz map[string]any) *testutil.Response {
	t.Helper()
	return admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{"filename": filename, "quiz_data": quiz})
}

func TestDecimalPointsValidation(t *testing.T) {
	t.Parallel()
	base := map[string]any{"title": "Test Quiz", "questions": []any{map[string]any{"question": "Test?", "options": []any{"A", "B"}, "correct_answer": 0}}}
	check := func(t *testing.T, points any, status int) {
		t.Parallel()
		ts := quizServer(t, base)
		admin := ts.AdminClient(t, "test123")
		if res := createQuiz(t, admin, "points.yaml", pointsQuizOf(points)); res.Status != status {
			t.Errorf("points %v: status %d, want %d: %s", points, res.Status, status, res.Body)
		}
	}

	for _, tc := range []struct {
		py     string
		points any
	}{
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_valid_points_accepted[0.5]", points: 0.5},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_valid_points_accepted[1.25]", points: 1.25},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_valid_points_accepted[2.75]", points: 2.75},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_valid_points_accepted[0.01]", points: 0.01},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_valid_points_accepted[3]", points: 3},
	} {
		t.Run("valid_"+tc.py[strings.Index(tc.py, "["):], func(t *testing.T) { check(t, tc.points, 200) })
	}
	for _, tc := range []struct {
		py     string
		points any
	}{
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_more_than_two_decimals_rejected[1.234]", points: 1.234},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_more_than_two_decimals_rejected[0.001]", points: 0.001},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_more_than_two_decimals_rejected[2.5001]", points: 2.5001},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_non_positive_or_non_numeric_points_rejected[0]", points: 0},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_non_positive_or_non_numeric_points_rejected[-1]", points: -1},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_non_positive_or_non_numeric_points_rejected[-0.5]", points: -0.5},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_non_positive_or_non_numeric_points_rejected[abc]", points: "abc"},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_non_positive_or_non_numeric_points_rejected[True]", points: true},
		{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_non_positive_or_non_numeric_points_rejected[None]", points: nil},
	} {
		t.Run("rejected_"+tc.py[strings.Index(tc.py, "["):], func(t *testing.T) { check(t, tc.points, 400) })
	}

	// py: tests/test_decimal_points.py::TestDecimalPointsValidation::test_points_validated_on_text_questions_too
	t.Run("text_questions_too", func(t *testing.T) {
		t.Parallel()
		ts := quizServer(t, base)
		admin := ts.AdminClient(t, "test123")
		text := func(points any) map[string]any {
			return map[string]any{"title": "Text Points Quiz", "questions": []any{
				map[string]any{"question": "Enter:", "correct_value": "x", "checker": "", "points": points},
			}}
		}
		if res := createQuiz(t, admin, "text_good.yaml", text(1.5)); res.Status != 200 {
			t.Errorf("1.5 on a text question: %d %s", res.Status, res.Body)
		}
		if res := createQuiz(t, admin, "text_bad.yaml", text(1.555)); res.Status != 400 {
			t.Errorf("1.555 on a text question: %d %s", res.Status, res.Body)
		}
	})
}

// py: tests/test_decimal_points.py::TestDecimalPointsRoundTrip::test_decimal_points_preserved_in_saved_quiz
func TestDecimalPointsPreservedInSavedQuiz(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, map[string]any{"title": "Test Quiz", "questions": []any{map[string]any{"question": "Test?", "options": []any{"A", "B"}, "correct_answer": 0}}})
	admin := ts.AdminClient(t, "test123")
	newQuiz := map[string]any{"title": "Decimal Quiz", "questions": []any{
		map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0, "points": 0.5},
		map[string]any{"question": "Q2", "options": []any{"A", "B"}, "correct_answer": 1, "points": 2.25},
	}}
	if res := createQuiz(t, admin, "decimal_quiz.yaml", newQuiz); res.Status != 200 {
		t.Fatalf("create-quiz: %d %s", res.Status, res.Body)
	}
	res := admin.GetJSON(t, "/api/admin/quiz/decimal_quiz.yaml")
	if res.Status != 200 {
		t.Fatalf("get quiz: %d %s", res.Status, res.Body)
	}
	content, _ := res.JSON(t)["content"].(string)
	if !strings.Contains(content, "0.5") || !strings.Contains(content, "2.25") {
		t.Errorf("content: %s", content)
	}
}
