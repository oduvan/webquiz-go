package server_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// paStickyQuestion is a two-option question; sticky adds stick_to_the_previous
// (nil leaves the key out).
func paStickyQuestion(n int, sticky any, extra map[string]any) map[string]any {
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

func paStickyQuiz(title string, randomize any, questions ...any) map[string]any {
	quiz := map[string]any{"title": title, "questions": questions}
	if randomize != nil {
		quiz["randomize_questions"] = randomize
	}
	return quiz
}

// orders registers n students and returns each one's question_order.
func paOrdersOf(t *testing.T, ts *testutil.TestServer, n int) [][]int {
	t.Helper()
	var out [][]int
	for i := 0; i < n; i++ {
		_, data := paRegisterAndVerify(t, ts, fmt.Sprintf("user%d", i))
		out = append(out, paOrderOf(t, data))
	}
	return out
}

// paFollows reports whether question b is directly after question a. Like
// Python's order.index(), it fails the test when an id is missing, and when
// the order is not a permutation of 1..len(order).
func paFollows(t *testing.T, order []int, a, b int) bool {
	t.Helper()
	sorted := slices.Clone(order)
	slices.Sort(sorted)
	for i, id := range sorted {
		if id != i+1 {
			t.Fatalf("order %v is not a permutation of 1..%d", order, len(order))
		}
	}
	ia, ib := slices.Index(order, a), slices.Index(order, b)
	if ia < 0 || ib < 0 {
		t.Fatalf("question %d or %d is missing from order %v", a, b, order)
	}
	return ib == ia+1
}

func paStickyServer(t *testing.T, quiz map[string]any) *testutil.TestServer {
	t.Helper()
	return paRandomServer(t, quiz, nil)
}

// py: tests/test_stick_to_previous.py::test_stick_to_previous_disabled_by_default
func TestStickToPreviousDisabledByDefault(t *testing.T) {
	t.Parallel()
	quiz := paStickyQuiz("Test Quiz", true, paStickyQuestion(1, nil, nil), paStickyQuestion(2, nil, nil))
	for _, q := range quiz["questions"].([]any) {
		if _, has := q.(map[string]any)["stick_to_the_previous"]; has {
			t.Fatal("the test quiz must not have stick_to_the_previous")
		}
	}
	ts := paStickyServer(t, quiz)
	_, data := paRegisterAndVerify(t, ts, "testuser")
	order := paOrderOf(t, data)
	slices.Sort(order)
	if !slices.Equal(order, []int{1, 2}) {
		t.Errorf("order %v", order)
	}
}

// py: tests/test_stick_to_previous.py::test_simple_sticky_pair
func TestSimpleStickyPair(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, paStickyQuiz("Sticky Pair Quiz", true, paStickyQuestion(1, nil, nil), paStickyQuestion(2, true, nil), paStickyQuestion(3, nil, nil)))
	for _, order := range paOrdersOf(t, ts, 10) {
		if !paFollows(t, order, 1, 2) {
			t.Errorf("Q2 should follow Q1, got order: %v", order)
		}
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_chain_of_three
func TestStickyChainOfThree(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, paStickyQuiz("Sticky Chain Quiz", true, paStickyQuestion(1, nil, nil), paStickyQuestion(2, true, nil), paStickyQuestion(3, true, nil), paStickyQuestion(4, nil, nil)))
	for _, order := range paOrdersOf(t, ts, 10) {
		if !paFollows(t, order, 1, 2) || !paFollows(t, order, 2, 3) {
			t.Errorf("Q1, Q2, Q3 should be consecutive, got order: %v", order)
		}
	}
}

func fiveWithGroups() map[string]any {
	return paStickyQuiz("Multiple Groups Quiz", true, paStickyQuestion(1, nil, nil), paStickyQuestion(2, true, nil), paStickyQuestion(3, nil, nil), paStickyQuestion(4, true, nil), paStickyQuestion(5, true, nil))
}

// py: tests/test_stick_to_previous.py::test_multiple_sticky_groups
func TestMultipleStickyGroups(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, fiveWithGroups())
	for _, order := range paOrdersOf(t, ts, 10) {
		if !paFollows(t, order, 1, 2) || !paFollows(t, order, 3, 4) || !paFollows(t, order, 4, 5) {
			t.Errorf("groups broken: %v", order)
		}
	}
}

// py: tests/test_stick_to_previous.py::test_positions_2_4_5_sticky
func TestPositions245Sticky(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, fiveWithGroups())
	seen := map[string]bool{}
	for _, order := range paOrdersOf(t, ts, 20) {
		seen[fmt.Sprint(order)] = true
		if !paFollows(t, order, 1, 2) || !paFollows(t, order, 3, 4) || !paFollows(t, order, 4, 5) {
			t.Errorf("groups broken: %v", order)
		}
	}
	// Python asserted len(orders_seen) >= 1 here (its comment hoped for 2);
	// this mirrors it. The loop above already checks every order.
	if len(seen) < 1 {
		t.Error("should have valid sticky group orderings")
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_without_randomization
func TestStickyWithoutRandomization(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, paStickyQuiz("Non-Randomized Quiz", false, paStickyQuestion(1, nil, nil), paStickyQuestion(2, true, nil), paStickyQuestion(3, nil, nil)))
	_, data := paRegisterAndVerify(t, ts, "testuser")
	if _, has := data["question_order"]; has {
		t.Errorf("question_order present: %v", data)
	}
}

func paStickyValidate(t *testing.T, invalid map[string]any) map[string]any {
	t.Helper()
	ts := paStickyServer(t, map[string]any{"title": "Default",
		"questions": []any{map[string]any{"question": "Q", "options": []any{"A", "B"}, "correct_answer": 0}}})
	res, data := paValidateQuizContent(t, ts, invalid)
	if res.Status != 200 || data["valid"] != false {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	return data
}

func paAnyError(data map[string]any, test func(string) bool) bool {
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
	data := paStickyValidate(t, paStickyQuiz("Invalid Quiz", nil, paStickyQuestion(1, true, nil), paStickyQuestion(2, nil, nil)))
	if !paAnyError(data, func(s string) bool {
		return strings.Contains(s, "Question 1") && strings.Contains(s, "stick_to_the_previous")
	}) {
		t.Errorf("errors: %v", data["errors"])
	}
}

// py: tests/test_stick_to_previous.py::test_validation_stick_to_previous_must_be_boolean
func TestValidationStickToPreviousMustBeBoolean(t *testing.T) {
	t.Parallel()
	data := paStickyValidate(t, paStickyQuiz("Invalid Quiz", nil, paStickyQuestion(1, nil, nil), paStickyQuestion(2, "yes", nil)))
	if !paAnyError(data, func(s string) bool { return strings.Contains(s, "boolean") }) {
		t.Errorf("errors: %v", data["errors"])
	}
}

// py: tests/test_stick_to_previous.py::test_validation_stick_to_previous_integer_is_invalid
func TestValidationStickToPreviousIntegerIsInvalid(t *testing.T) {
	t.Parallel()
	data := paStickyValidate(t, paStickyQuiz("Invalid Quiz", nil, paStickyQuestion(1, nil, nil), paStickyQuestion(2, 1, nil)))
	if !paAnyError(data, func(s string) bool { return strings.Contains(s, "boolean") }) {
		t.Errorf("errors: %v", data["errors"])
	}
}

// py: tests/test_stick_to_previous.py::test_all_questions_sticky_except_first
func TestAllQuestionsStickyExceptFirst(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, paStickyQuiz("Single Group Quiz", true, paStickyQuestion(1, nil, nil), paStickyQuestion(2, true, nil), paStickyQuestion(3, true, nil), paStickyQuestion(4, true, nil)))
	_, data := paRegisterAndVerify(t, ts, "testuser")
	if order := paOrderOf(t, data); !slices.Equal(order, []int{1, 2, 3, 4}) {
		t.Errorf("Expected [1 2 3 4], got %v", order)
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_with_text_questions
func TestStickyWithTextQuestions(t *testing.T) {
	t.Parallel()
	text := map[string]any{"question": "Q2 (text)", "checker": "assert True", "stick_to_the_previous": true}
	ts := paStickyServer(t, paStickyQuiz("Mixed Quiz", true, paStickyQuestion(1, nil, nil), text, paStickyQuestion(3, nil, nil)))
	for _, order := range paOrdersOf(t, ts, 5) {
		if !paFollows(t, order, 1, 2) {
			t.Errorf("Q2 should follow Q1, got: %v", order)
		}
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_groups_actually_shuffle
func TestStickyGroupsActuallyShuffle(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, paStickyQuiz("Shuffle Test Quiz", true, paStickyQuestion(1, nil, nil), paStickyQuestion(2, true, nil),
		paStickyQuestion(3, nil, nil), paStickyQuestion(4, nil, nil), paStickyQuestion(5, nil, nil), paStickyQuestion(6, nil, nil)))
	firsts := map[int]bool{}
	for _, order := range paOrdersOf(t, ts, 20) {
		firsts[order[0]] = true
		if !paFollows(t, order, 1, 2) {
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
	ts := paStickyServer(t, paStickyQuiz("Explicit False Quiz", true, paStickyQuestion(1, nil, nil), paStickyQuestion(2, false, nil), paStickyQuestion(3, nil, nil)))
	seen := map[string]bool{}
	for _, order := range paOrdersOf(t, ts, 20) {
		seen[fmt.Sprint(order)] = true
	}
	if len(seen) < 2 {
		t.Error("questions should shuffle independently when stick_to_the_previous is false")
	}
}

// py: tests/test_stick_to_previous.py::test_sticky_with_points
func TestStickyWithPoints(t *testing.T) {
	t.Parallel()
	ts := paStickyServer(t, paStickyQuiz("Points Quiz", true, paStickyQuestion(1, nil, map[string]any{"points": 2}),
		paStickyQuestion(2, true, map[string]any{"points": 3}), paStickyQuestion(3, nil, nil)))
	for _, order := range paOrdersOf(t, ts, 5) {
		if !paFollows(t, order, 1, 2) {
			t.Errorf("Q2 should follow Q1, got: %v", order)
		}
	}
}
