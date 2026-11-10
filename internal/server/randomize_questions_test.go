package server_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// paRandomQuiz makes a quiz of n questions "Q1".."Qn" with two options each,
// the way the Python tests wrote them. Option values and correct answers do
// not matter to most tests (the correct answer is 0 for odd ids, 1 for even).
func paRandomQuiz(title string, n int, settings map[string]any) map[string]any {
	questions := []any{}
	for i := 1; i <= n; i++ {
		questions = append(questions, map[string]any{
			"question":       fmt.Sprintf("Q%d", i),
			"options":        []any{fmt.Sprintf("A%d", i), fmt.Sprintf("B%d", i)},
			"correct_answer": (i + 1) % 2,
		})
	}
	quiz := map[string]any{"title": title, "questions": questions}
	for key, value := range settings {
		quiz[key] = value
	}
	return quiz
}

func paRandomServer(t *testing.T, quiz map[string]any, config map[string]any) *testutil.TestServer {
	t.Helper()
	return testutil.StartServer(t, testutil.Options{Config: config, Quizzes: map[string]any{"test.yaml": quiz}})
}

// paRegisterAndVerify registers a student and returns the id and the verify-user reply.
func paRegisterAndVerify(t *testing.T, ts *testutil.TestServer, name string) (string, map[string]any) {
	t.Helper()
	userID := ts.RegisterStudent(t, name)
	return userID, paVerifyUser(t, ts, userID)
}

func paVerifyUser(t *testing.T, ts *testutil.TestServer, userID string) map[string]any {
	t.Helper()
	res := ts.GetJSON(t, "/api/verify-user/"+userID)
	if res.Status != 200 {
		t.Fatalf("verify-user: %d %s", res.Status, res.Body)
	}
	return res.JSON(t)
}

// paOrderOf reads question_order as ints.
func paOrderOf(t *testing.T, data map[string]any) []int {
	t.Helper()
	list, ok := data["question_order"].([]any)
	if !ok {
		t.Fatalf("no question_order in %v", data)
	}
	out := []int{}
	for _, item := range list {
		n, err := item.(interface{ Int64() (int64, error) }).Int64()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, int(n))
	}
	return out
}

// paSkipIfStub skips the test while a route is still the 501 stub of another task.
func paSkipIfStub(t *testing.T, res *testutil.Response, what string) {
	t.Helper()
	if res.Status == 501 {
		t.Skip(what + " is not built yet")
	}
}

// py: tests/test_randomize_questions.py::test_randomization_disabled_by_default
func TestRandomizationDisabledByDefault(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Default Behavior Quiz", 3, nil), nil)
	_, data := paRegisterAndVerify(t, ts, "testuser")
	if data["valid"] != true {
		t.Errorf("%v", data)
	}
	if _, has := data["question_order"]; has {
		t.Errorf("question_order present when randomization is off: %v", data)
	}
}

// py: tests/test_randomize_questions.py::test_randomization_enabled
func TestRandomizationEnabled(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 5, map[string]any{"randomize_questions": true}), nil)
	_, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	slices.Sort(order)
	if data["valid"] != true || !slices.Equal(order, []int{1, 2, 3, 4, 5}) {
		t.Errorf("%v", data)
	}
}

// py: tests/test_randomize_questions.py::test_different_users_get_different_orders
func TestDifferentUsersGetDifferentOrders(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 6, map[string]any{"randomize_questions": true}), nil)
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		_, data := paRegisterAndVerify(t, ts, fmt.Sprintf("user%d", i))
		seen[fmt.Sprint(paOrderOf(t, data))] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected at least some users to have different question orders, got %v", seen)
	}
}

// py: tests/test_randomize_questions.py::test_question_order_persists_across_verifications
func TestQuestionOrderPersistsAcrossVerifications(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 3, map[string]any{"randomize_questions": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	first := paOrderOf(t, data)
	for i := 0; i < 5; i++ {
		if got := paOrderOf(t, paVerifyUser(t, ts, userID)); !slices.Equal(got, first) {
			t.Fatalf("order changed: %v then %v", first, got)
		}
	}
}

// py: tests/test_randomize_questions.py::test_randomization_with_approval_workflow
func TestRandomizationWithApprovalWorkflow(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 3, map[string]any{"randomize_questions": true}),
		map[string]any{"registration": map[string]any{"approve": true}})
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "testuser"})
	reg := res.JSON(t)
	if res.Status != 200 || reg["requires_approval"] != true || reg["approved"] != false {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	userID := reg["user_id"].(string)
	before := paOrderOf(t, paVerifyUser(t, ts, userID))
	if res := paApproveUser(t, ts, userID, testutil.WithCookies(ts.AdminCookies(t, "test123")...)); res.Status != 200 {
		t.Fatalf("approve: %d %s", res.Status, res.Body)
	}
	data := paVerifyUser(t, ts, userID)
	if data["approved"] != true || !slices.Equal(paOrderOf(t, data), before) {
		t.Errorf("after approval: %v, order before %v", data, before)
	}
}

func paValidateQuizContent(t *testing.T, ts *testutil.TestServer, quiz map[string]any) (*testutil.Response, map[string]any) {
	t.Helper()
	content, err := testutil.YAML(quiz)
	if err != nil {
		t.Fatal(err)
	}
	res := ts.PostJSON(t, "/api/admin/validate-quiz", map[string]any{"content": string(content)},
		testutil.WithCookies(ts.AdminCookies(t, "test123")...))
	paSkipIfStub(t, res, "POST /api/admin/validate-quiz (Task 8)")
	return res, res.JSON(t)
}

// py: tests/test_randomize_questions.py::test_yaml_validation_accepts_randomize_questions_boolean
func TestYAMLValidationAcceptsRandomizeQuestionsBoolean(t *testing.T) {
	t.Parallel()
	quiz := map[string]any{"title": "Valid Quiz", "randomize_questions": true,
		"questions": []any{map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0}}}
	ts := paRandomServer(t, quiz, nil)
	res, data := paValidateQuizContent(t, ts, quiz)
	errors, _ := data["errors"].([]any)
	if res.Status != 200 || data["valid"] != true || len(errors) != 0 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_randomize_questions.py::test_yaml_validation_rejects_non_boolean_randomize_questions
func TestYAMLValidationRejectsNonBooleanRandomizeQuestions(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, map[string]any{"title": "Default",
		"questions": []any{map[string]any{"question": "Q", "options": []any{"A"}, "correct_answer": 0}}}, nil)
	invalid := map[string]any{"title": "Invalid Quiz", "randomize_questions": "yes",
		"questions": []any{map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0}}}
	res, data := paValidateQuizContent(t, ts, invalid)
	found := false
	for _, item := range data["errors"].([]any) {
		if s, _ := item.(string); strings.Contains(s, "randomize_questions") && strings.Contains(s, "boolean") {
			found = true
		}
	}
	if res.Status != 200 || data["valid"] != false || !found {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_randomize_questions.py::test_randomization_false_behaves_like_disabled
func TestRandomizationFalseBehavesLikeDisabled(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Non-Randomized Quiz", 2, map[string]any{"randomize_questions": false}), nil)
	_, data := paRegisterAndVerify(t, ts, "testuser")
	if _, has := data["question_order"]; has {
		t.Errorf("question_order present: %v", data)
	}
}

// py: tests/test_randomize_questions.py::test_question_order_all_ids_present
func TestQuestionOrderAllIDsPresent(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 10, map[string]any{"randomize_questions": true}), nil)
	_, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	sorted := slices.Clone(order)
	slices.Sort(sorted)
	if len(order) != 10 || !slices.Equal(sorted, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
		t.Errorf("order %v", order)
	}
}

// py: tests/test_randomize_questions.py::test_randomization_with_single_question
func TestRandomizationWithSingleQuestion(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Single Question Quiz", 1, map[string]any{"randomize_questions": true}), nil)
	_, data := paRegisterAndVerify(t, ts, "testuser")
	if got := paOrderOf(t, data); !slices.Equal(got, []int{1}) {
		t.Errorf("order %v", got)
	}
}

// py: tests/test_randomize_questions.py::test_yaml_validation_accepts_other_top_level_fields
func TestYAMLValidationAcceptsOtherTopLevelFields(t *testing.T) {
	t.Parallel()
	quiz := map[string]any{"title": "Full Featured Quiz", "show_right_answer": false, "randomize_questions": true,
		"questions": []any{map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0}}}
	ts := paRandomServer(t, quiz, nil)
	res, data := paValidateQuizContent(t, ts, quiz)
	if res.Status != 200 || data["valid"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

func paNextIndex(t *testing.T, data map[string]any) string {
	t.Helper()
	return fmt.Sprint(data["next_question_index"])
}

// py: tests/test_randomize_questions.py::test_progress_tracking_with_randomization
func TestProgressTrackingWithRandomization(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 5, map[string]any{"randomize_questions": true, "show_right_answer": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	if paNextIndex(t, data) != "0" {
		t.Fatalf("next_question_index %v", data["next_question_index"])
	}
	for i := 0; i < 2; i++ {
		if res := partAAnswer(t, ts, userID, order[i], 0); res.Status != 200 {
			t.Fatalf("answer %d: %d %s", order[i], res.Status, res.Body)
		}
		// A page refresh asks again: the next question is the one after the answered ones.
		if got := paNextIndex(t, paVerifyUser(t, ts, userID)); got != fmt.Sprint(i+1) {
			t.Errorf("after %d answers next_question_index = %s", i+1, got)
		}
	}
}

// py: tests/test_randomize_questions.py::test_progress_tracking_without_randomization
func TestProgressTrackingWithoutRandomization(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Non-Randomized Quiz", 3, map[string]any{"randomize_questions": false, "show_right_answer": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	if paNextIndex(t, data) != "0" {
		t.Fatalf("next_question_index %v", data["next_question_index"])
	}
	if res := partAAnswer(t, ts, userID, 1, 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := paNextIndex(t, paVerifyUser(t, ts, userID)); got != "1" {
		t.Errorf("next_question_index = %s", got)
	}
}

// py: tests/test_randomize_questions.py::test_final_results_with_randomization
func TestFinalResultsWithRandomization(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 5, map[string]any{"randomize_questions": true, "show_right_answer": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	if len(order) != 5 {
		t.Fatalf("order %v", order)
	}
	for _, id := range order {
		if res := partAAnswer(t, ts, userID, id, 0); res.Status != 200 {
			t.Fatalf("answer %d: %d %s", id, res.Status, res.Body)
		}
	}
	data = paVerifyUser(t, ts, userID)
	final, _ := data["final_results"].(map[string]any)
	results, _ := final["test_results"].([]any)
	if data["test_completed"] != true || fmt.Sprint(final["total_count"]) != "5" || len(results) != 5 {
		t.Fatalf("%v", data)
	}
	for i, item := range results {
		result := item.(map[string]any)
		for _, field := range []string{"question", "selected_answer", "correct_answer", "is_correct", "time_taken"} {
			if _, has := result[field]; !has {
				t.Errorf("result %d lacks %s: %v", i, field, result)
			}
		}
		// The results are in the order the student answered them.
		if want := fmt.Sprintf("Q%d", order[i]); result["question"] != want {
			t.Errorf("result %d is %v, want %s", i, result["question"], want)
		}
	}
}

// py: tests/test_randomize_questions.py::test_cannot_skip_questions_with_randomization
func TestCannotSkipQuestionsWithRandomization(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 5, map[string]any{"randomize_questions": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)

	res := partAAnswer(t, ts, userID, order[2], 0)
	body := res.JSON(t)
	if res.Status != 403 || body["error"] == nil || fmt.Sprint(body["expected_question_id"]) != fmt.Sprint(order[0]) {
		t.Fatalf("third question first: %d %s (expected %d)", res.Status, res.Body, order[0])
	}
	if res := partAAnswer(t, ts, userID, order[0], 0); res.Status != 200 {
		t.Fatalf("first question: %d %s", res.Status, res.Body)
	}
	res = partAAnswer(t, ts, userID, order[2], 0)
	if res.Status != 403 || fmt.Sprint(res.JSON(t)["expected_question_id"]) != fmt.Sprint(order[1]) {
		t.Errorf("skip the second: %d %s (expected %d)", res.Status, res.Body, order[1])
	}
}

// py: tests/test_randomize_questions.py::test_cannot_answer_already_answered_questions_with_randomization
func TestCannotAnswerAlreadyAnsweredQuestionsWithRandomization(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 3, map[string]any{"randomize_questions": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	if res := partAAnswer(t, ts, userID, order[0], 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	res := partAAnswer(t, ts, userID, order[0], 1)
	if res.Status != 403 || fmt.Sprint(res.JSON(t)["expected_question_id"]) != fmt.Sprint(order[1]) {
		t.Errorf("%d %s (expected %d)", res.Status, res.Body, order[1])
	}
}

// py: tests/test_randomize_questions.py::test_cannot_answer_after_completing_quiz_with_randomization
func TestCannotAnswerAfterCompletingQuizWithRandomization(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 2, map[string]any{"randomize_questions": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	for _, id := range order {
		if res := partAAnswer(t, ts, userID, id, 0); res.Status != 200 {
			t.Fatalf("answer %d: %d %s", id, res.Status, res.Body)
		}
	}
	res := partAAnswer(t, ts, userID, order[0], 1)
	text, _ := res.JSON(t)["error"].(string)
	if res.Status != 400 || !strings.Contains(text, "вже відповіли на всі питання") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_randomize_questions.py::test_sequential_answering_allowed_without_randomization
func TestSequentialAnsweringAllowedWithoutRandomization(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Non-Randomized Quiz", 3, map[string]any{"randomize_questions": false}), nil)
	userID := ts.RegisterStudent(t, "testuser")
	for _, id := range []int{1, 2, 3} {
		if res := partAAnswer(t, ts, userID, id, 0); res.Status != 200 {
			t.Errorf("question %d should be accepted: %d %s", id, res.Status, res.Body)
		}
	}
}

// py: tests/test_randomize_questions.py::test_validation_error_message_quality
func TestValidationErrorMessageQuality(t *testing.T) {
	t.Parallel()
	ts := paRandomServer(t, paRandomQuiz("Randomized Quiz", 3, map[string]any{"randomize_questions": true}), nil)
	userID, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	res := partAAnswer(t, ts, userID, order[2], 0)
	body := res.JSON(t)
	text, _ := body["error"].(string)
	if res.Status != 403 || text == "" || fmt.Sprint(body["expected_question_id"]) != fmt.Sprint(order[0]) ||
		!strings.Contains(strings.ToLower(text), "питання") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}
