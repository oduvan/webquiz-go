package server_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// stickyQuestion is a two-option question; sticky adds stick_to_the_previous
// (nil leaves the key out).
func stickyQuestion(n int, sticky any, extra map[string]any) map[string]any {
	question := map[string]any{
		"question":       fmt.Sprintf("Q%d", n),
		"options":        []any{"A", "B"},
		"correct_answer": (n + 1) % 2,
	}
	if sticky != nil {
		question["stick_to_the_previous"] = sticky
	}
	for key, value := range extra {
		question[key] = value
	}
	return question
}

func stickyQuiz(title string, randomize any, questions ...any) map[string]any {
	quiz := map[string]any{"title": title, "questions": questions}
	if randomize != nil {
		quiz["randomize_questions"] = randomize
	}
	return quiz
}

// orders registers n students and returns each one's question_order.
func ordersOf(t *testing.T, ts *testutil.TestServer, n int) [][]int {
	t.Helper()
	var out [][]int
	for i := 0; i < n; i++ {
		_, data := registerAndVerify(t, ts, fmt.Sprintf("user%d", i))
		out = append(out, orderOf(t, data))
	}
	return out
}

// follows reports whether question b is directly after question a.
func follows(order []int, a, b int) bool {
	return slices.Index(order, b) == slices.Index(order, a)+1
}

func stickyServer(t *testing.T, quiz map[string]any) *testutil.TestServer {
	t.Helper()
	return randomServer(t, quiz, nil)
}

// py: tests/test_stick_to_previous.py::test_stick_to_previous_disabled_by_default
func TestStickToPreviousDisabledByDefault(t *testing.T) {
	t.Parallel()
	quiz := stickyQuiz("Test Quiz", true, stickyQuestion(1, nil, nil), stickyQuestion(2, nil, nil))
	for _, q := range quiz["questions"].([]any) {
		if _, has := q.(map[string]any)["stick_to_the_previous"]; has {
			t.Fatal("the test quiz must not have stick_to_the_previous")
		}
	}
	ts := stickyServer(t, quiz)
	_, data := registerAndVerify(t, ts, "testuser")
	order := orderOf(t, data)
	slices.Sort(order)
	if !slices.Equal(order, []int{1, 2}) {
		t.Errorf("order %v", order)
	}
}

// py: tests/test_stick_to_previous.py::test_simple_sticky_pair
func TestSimpleStickyPair(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, stickyQuiz("Sticky Pair Quiz", true, stickyQuestion(1, nil, nil), stickyQuestion(2, true, nil), stickyQuestion(3, nil, nil)))
	for _, order := range ordersOf(t, ts, 10) {
		if !follows(order, 1, 2) {
			t.Errorf("Q2 should follow Q1, got order: %v", order)
		}
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_chain_of_three
func TestStickyChainOfThree(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, stickyQuiz("Sticky Chain Quiz", true, stickyQuestion(1, nil, nil), stickyQuestion(2, true, nil), stickyQuestion(3, true, nil), stickyQuestion(4, nil, nil)))
	for _, order := range ordersOf(t, ts, 10) {
		if !follows(order, 1, 2) || !follows(order, 2, 3) {
			t.Errorf("Q1, Q2, Q3 should be consecutive, got order: %v", order)
		}
	}
}

func fiveWithGroups() map[string]any {
	return stickyQuiz("Multiple Groups Quiz", true, stickyQuestion(1, nil, nil), stickyQuestion(2, true, nil), stickyQuestion(3, nil, nil), stickyQuestion(4, true, nil), stickyQuestion(5, true, nil))
}

// py: tests/test_stick_to_previous.py::test_multiple_sticky_groups
func TestMultipleStickyGroups(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, fiveWithGroups())
	for _, order := range ordersOf(t, ts, 10) {
		if !follows(order, 1, 2) || !follows(order, 3, 4) || !follows(order, 4, 5) {
			t.Errorf("groups broken: %v", order)
		}
	}
}

// py: tests/test_stick_to_previous.py::test_positions_2_4_5_sticky
func TestPositions245Sticky(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, fiveWithGroups())
	seen := map[string]bool{}
	for _, order := range ordersOf(t, ts, 20) {
		seen[fmt.Sprint(order)] = true
		if !follows(order, 1, 2) || !follows(order, 3, 4) || !follows(order, 4, 5) {
			t.Errorf("groups broken: %v", order)
		}
	}
	if len(seen) < 1 {
		t.Error("should have valid sticky group orderings")
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_without_randomization
func TestStickyWithoutRandomization(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, stickyQuiz("Non-Randomized Quiz", false, stickyQuestion(1, nil, nil), stickyQuestion(2, true, nil), stickyQuestion(3, nil, nil)))
	_, data := registerAndVerify(t, ts, "testuser")
	if _, has := data["question_order"]; has {
		t.Errorf("question_order present: %v", data)
	}
}

func stickyValidate(t *testing.T, invalid map[string]any) map[string]any {
	t.Helper()
	ts := stickyServer(t, map[string]any{"title": "Default",
		"questions": []any{map[string]any{"question": "Q", "options": []any{"A", "B"}, "correct_answer": 0}}})
	res, data := validateQuizContent(t, ts, invalid)
	if res.Status != 200 || data["valid"] != false {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	return data
}

func anyError(data map[string]any, test func(string) bool) bool {
	for _, item := range data["errors"].([]any) {
		if s, _ := item.(string); test(s) {
			return true
		}
	}
	return false
}

// py: tests/test_stick_to_previous.py::test_validation_first_question_cannot_be_sticky
func TestValidationFirstQuestionCannotBeSticky(t *testing.T) {
	t.Parallel()
	data := stickyValidate(t, stickyQuiz("Invalid Quiz", nil, stickyQuestion(1, true, nil), stickyQuestion(2, nil, nil)))
	if !anyError(data, func(s string) bool {
		return strings.Contains(s, "Question 1") && strings.Contains(s, "stick_to_the_previous")
	}) {
		t.Errorf("errors: %v", data["errors"])
	}
}

// py: tests/test_stick_to_previous.py::test_validation_stick_to_previous_must_be_boolean
func TestValidationStickToPreviousMustBeBoolean(t *testing.T) {
	t.Parallel()
	data := stickyValidate(t, stickyQuiz("Invalid Quiz", nil, stickyQuestion(1, nil, nil), stickyQuestion(2, "yes", nil)))
	if !anyError(data, func(s string) bool { return strings.Contains(s, "boolean") }) {
		t.Errorf("errors: %v", data["errors"])
	}
}

// py: tests/test_stick_to_previous.py::test_validation_stick_to_previous_integer_is_invalid
func TestValidationStickToPreviousIntegerIsInvalid(t *testing.T) {
	t.Parallel()
	data := stickyValidate(t, stickyQuiz("Invalid Quiz", nil, stickyQuestion(1, nil, nil), stickyQuestion(2, 1, nil)))
	if !anyError(data, func(s string) bool { return strings.Contains(s, "boolean") }) {
		t.Errorf("errors: %v", data["errors"])
	}
}

// py: tests/test_stick_to_previous.py::test_all_questions_sticky_except_first
func TestAllQuestionsStickyExceptFirst(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, stickyQuiz("Single Group Quiz", true, stickyQuestion(1, nil, nil), stickyQuestion(2, true, nil), stickyQuestion(3, true, nil), stickyQuestion(4, true, nil)))
	_, data := registerAndVerify(t, ts, "testuser")
	if order := orderOf(t, data); !slices.Equal(order, []int{1, 2, 3, 4}) {
		t.Errorf("Expected [1 2 3 4], got %v", order)
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_with_text_questions
func TestStickyWithTextQuestions(t *testing.T) {
	t.Parallel()
	text := map[string]any{"question": "Q2 (text)", "checker": "assert True", "stick_to_the_previous": true}
	ts := stickyServer(t, stickyQuiz("Mixed Quiz", true, stickyQuestion(1, nil, nil), text, stickyQuestion(3, nil, nil)))
	for _, order := range ordersOf(t, ts, 5) {
		if !follows(order, 1, 2) {
			t.Errorf("Q2 should follow Q1, got: %v", order)
		}
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_groups_actually_shuffle
func TestStickyGroupsActuallyShuffle(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, stickyQuiz("Shuffle Test Quiz", true, stickyQuestion(1, nil, nil), stickyQuestion(2, true, nil),
		stickyQuestion(3, nil, nil), stickyQuestion(4, nil, nil), stickyQuestion(5, nil, nil), stickyQuestion(6, nil, nil)))
	firsts := map[int]bool{}
	for _, order := range ordersOf(t, ts, 20) {
		firsts[order[0]] = true
		if !follows(order, 1, 2) {
			t.Errorf("Q2 should follow Q1, got: %v", order)
		}
	}
	if len(firsts) < 2 {
		t.Error("groups should be shuffled, but the first position is always the same")
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_false_explicit
func TestStickyFalseExplicit(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, stickyQuiz("Explicit False Quiz", true, stickyQuestion(1, nil, nil), stickyQuestion(2, false, nil), stickyQuestion(3, nil, nil)))
	seen := map[string]bool{}
	for _, order := range ordersOf(t, ts, 20) {
		seen[fmt.Sprint(order)] = true
	}
	if len(seen) < 2 {
		t.Error("questions should shuffle independently when stick_to_the_previous is false")
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_with_points
func TestStickyWithPoints(t *testing.T) {
	t.Parallel()
	ts := stickyServer(t, stickyQuiz("Points Quiz", true, stickyQuestion(1, nil, map[string]any{"points": 2}),
		stickyQuestion(2, true, map[string]any{"points": 3}), stickyQuestion(3, nil, nil)))
	for _, order := range ordersOf(t, ts, 5) {
		if !follows(order, 1, 2) {
			t.Errorf("Q2 should follow Q1, got: %v", order)
		}
	}
}
