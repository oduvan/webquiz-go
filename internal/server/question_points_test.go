package server_test

import (
	"encoding/json"
	"testing"
)

func pointsQuiz() map[string]any {
	return map[string]any{
		"title":             "Points Test Quiz",
		"show_right_answer": true,
		"questions": []any{
			map[string]any{"question": "What is 2+2?", "options": []any{"3", "4", "5", "6"}, "correct_answer": 1},
			map[string]any{"question": "Hard question worth 3 points", "options": []any{"A", "B", "C", "D"}, "correct_answer": 0, "points": 3},
			map[string]any{"question": "Very hard question worth 5 points", "options": []any{"X", "Y", "Z", "W"}, "correct_answer": 2, "points": 5},
		},
	}
}

// finalResults reads final_results of a finished student.
func finalResults(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	results, ok := res["final_results"].(map[string]any)
	if !ok {
		t.Fatalf("no final_results: %v", res)
	}
	return results
}

func TestQuestionPointsIntegration(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, pointsQuiz())

	// py: tests/test_question_points.py::TestQuestionPointsIntegration::test_questions_include_points_in_client_data
	t.Run("points_in_client_data", func(t *testing.T) {
		res := ts.GetJSON(t, "/")
		if res.Status != 200 {
			t.Fatalf("GET /: %d", res.Status)
		}
		boot := pageBoot(t, res.Text())
		for i, want := range []string{"1", "3", "5"} {
			numIs(t, "points of question "+string(rune('1'+i)), question(t, boot, i)["points"], want)
		}
	})

	// py: tests/test_question_points.py::TestQuestionPointsIntegration::test_correct_answer_earns_question_points
	t.Run("correct_answer_earns_question_points", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "testuser")
		for _, a := range [][2]int{{1, 1}, {2, 0}, {3, 2}} {
			if data := answerOK(t, ts, userID, a[0], a[1]); data["is_correct"] != true {
				t.Errorf("question %d: %v", a[0], data)
			}
		}
		res := ts.GetJSON(t, "/api/verify-user/"+userID)
		if res.Status != 200 {
			t.Fatalf("verify-user: %d", res.Status)
		}
		data := res.JSON(t)
		if data["test_completed"] != true {
			t.Errorf("test_completed: %v", data["test_completed"])
		}
		fr := finalResults(t, data)
		numIs(t, "correct_count", fr["correct_count"], "3")
		numIs(t, "total_count", fr["total_count"], "3")
		numIs(t, "earned_points", fr["earned_points"], "9")
		numIs(t, "total_points", fr["total_points"], "9")
		numIs(t, "points_percentage", fr["points_percentage"], "100")
	})

	// py: tests/test_question_points.py::TestQuestionPointsIntegration::test_incorrect_answer_earns_zero_points
	t.Run("incorrect_answer_earns_zero_points", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "testuser2")
		answerOK(t, ts, userID, 1, 1)
		if data := answerOK(t, ts, userID, 2, 1); data["is_correct"] != false {
			t.Errorf("question 2: %v", data)
		}
		answerOK(t, ts, userID, 3, 0)
		data := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)
		if data["test_completed"] != true {
			t.Errorf("test_completed: %v", data["test_completed"])
		}
		fr := finalResults(t, data)
		numIs(t, "correct_count", fr["correct_count"], "1")
		numIs(t, "total_count", fr["total_count"], "3")
		numIs(t, "earned_points", fr["earned_points"], "1")
		numIs(t, "total_points", fr["total_points"], "9")
		numIs(t, "points_percentage", fr["points_percentage"], "11") // 1/9
	})

	// py: tests/test_question_points.py::TestQuestionPointsIntegration::test_test_results_include_question_points
	t.Run("test_results_include_question_points", func(t *testing.T) {
		userID := ts.RegisterStudent(t, "resultstest")
		for _, a := range [][2]int{{1, 1}, {2, 1}, {3, 2}} { // right, wrong, right
			answerOK(t, ts, userID, a[0], a[1])
		}
		fr := finalResults(t, ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t))
		rows, _ := fr["test_results"].([]any)
		if len(rows) != 3 {
			t.Fatalf("test_results: %v", rows)
		}
		for i, want := range [][2]string{{"1", "1"}, {"3", "0"}, {"5", "5"}} {
			row := rows[i].(map[string]any)
			numIs(t, "points", row["points"], want[0])
			numIs(t, "earned_points", row["earned_points"], want[1])
		}
		numIs(t, "earned_points", fr["earned_points"], "6")
		numIs(t, "total_points", fr["total_points"], "9")
	})

	// py: tests/test_question_points.py::TestQuestionPointsIntegration::test_csv_includes_points_columns
	// The Python test slept 6 s for the CSV flush. The server is closed instead, which writes the waiting rows.
	t.Run("csv_includes_points_columns", func(t *testing.T) {
		ts := quizServer(t, pointsQuiz())
		userID := ts.RegisterStudent(t, "csvtest")
		for _, a := range [][2]int{{1, 1}, {2, 0}, {3, 2}} {
			answerOK(t, ts, userID, a[0], a[1])
		}
		rows := csvRecords(t, csvFileEnding(t, ts, ".users.csv"))
		if len(rows) == 0 {
			t.Fatal("no rows in the users CSV")
		}
		for _, column := range []string{"earned_points", "total_points"} {
			if _, ok := rows[0][column]; !ok {
				t.Errorf("%s column missing: %v", column, rows[0])
			}
		}
		if rows[0]["earned_points"] != "9" || rows[0]["total_points"] != "9" {
			t.Errorf("points in the CSV: %v", rows[0])
		}
	})
}

// py: tests/test_question_points.py::TestQuestionPointsIntegration::test_default_points_when_not_specified
func TestDefaultPointsWhenNotSpecified(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, map[string]any{"title": "Default Points Quiz", "show_right_answer": true, "questions": []any{
		map[string]any{"question": "Question 1", "options": []any{"A", "B", "C", "D"}, "correct_answer": 0},
		map[string]any{"question": "Question 2", "options": []any{"A", "B", "C", "D"}, "correct_answer": 1},
	}})
	userID := ts.RegisterStudent(t, "defaulttest")
	for _, id := range []int{1, 2} {
		answerOK(t, ts, userID, id, id-1)
	}
	fr := finalResults(t, ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t))
	numIs(t, "total_count", fr["total_count"], "2")
	numIs(t, "total_points", fr["total_points"], "2")
	numIs(t, "earned_points", fr["earned_points"], "2")
}

// py: tests/test_question_points.py::TestPointsWithRandomization::test_points_with_randomized_order
func TestPointsWithRandomizedOrder(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, map[string]any{"title": "Randomized Points Quiz", "show_right_answer": true, "randomize_questions": true, "questions": []any{
		map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0, "points": 2},
		map[string]any{"question": "Q2", "options": []any{"C", "D"}, "correct_answer": 1, "points": 3},
		map[string]any{"question": "Q3", "options": []any{"E", "F"}, "correct_answer": 0, "points": 5},
	}})
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "randomtest"})
	if res.Status != 200 {
		t.Fatalf("register: %d %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	userID, _ := data["user_id"].(string)
	order := []int{1, 2, 3}
	if raw, ok := data["question_order"].([]any); ok {
		order = nil
		for _, id := range raw {
			n, _ := id.(json.Number).Int64()
			order = append(order, int(n))
		}
	}
	correct := map[int]int{1: 0, 2: 1, 3: 0}
	for _, id := range order {
		if reply := answerOK(t, ts, userID, id, correct[id]); reply["is_correct"] != true {
			t.Errorf("question %d: %v", id, reply)
		}
	}
	fr := finalResults(t, ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t))
	numIs(t, "total_points", fr["total_points"], "10")
	numIs(t, "earned_points", fr["earned_points"], "10")
}
