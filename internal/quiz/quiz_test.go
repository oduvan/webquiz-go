package quiz

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/explanations"
	"github.com/oduvan/webquiz-go/internal/timelimits"
)

func writeQuiz(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "тест.yaml") // Ukrainian file names work
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadGivesIDsAndDefaults(t *testing.T) {
	path := writeQuiz(t, `
questions:
  - question: "2 + 2?"
    options: ["3", "4"]
    correct_answer: 1
  - question: "Name the capital"
    checker: ""
    correct_value: Kyiv
`)
	q, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Questions) != 2 || q.Questions[0].ID() != 1 || q.Questions[1].ID() != 2 {
		t.Fatalf("questions = %v", q.Questions)
	}
	if q.Questions[1]["id"] != 2 {
		t.Errorf("the id is stored in the question too, as in Python: %v", q.Questions[1])
	}
	if q.Title != DefaultTitle || DefaultTitle != "Система Тестування" {
		t.Errorf("Title = %q", q.Title)
	}
	if !q.ShowRightAnswer || q.RandomizeQuestions || q.ShowAnswersOnCompletion || !q.ShowFinalList || q.ShowExplanation {
		t.Errorf("defaults: %+v", q)
	}
	if q.TimeLimit != timelimits.NoLimit || q.TimeLimitWarning != "" {
		t.Errorf("time limit = %v, %q", q.TimeLimit, q.TimeLimitWarning)
	}
	if !reflect.DeepEqual(q.QuestionTimeLimits, map[int]int{1: 0, 2: 0}) {
		t.Errorf("QuestionTimeLimits = %v", q.QuestionTimeLimits)
	}
	if len(q.ExplanationsHTML) != 0 {
		t.Errorf("ExplanationsHTML = %v", q.ExplanationsHTML)
	}
	if q.Data["questions"] == nil {
		t.Error("Data should hold the whole file")
	}
}

func TestLoadReadsSettings(t *testing.T) {
	path := writeQuiz(t, `
title: Мій тест
show_right_answer: no
randomize_questions: yes
show_answers_on_completion: true
show_final_list: false
show_explanation: on
time_limit:
  per_question: 30
questions:
  - question: Q1
    options: [A, B]
    correct_answer: 0
    time_limit: 10
    explanation: "**Why**: ![a](/imgs/a.png)"
  - question: Q2
    options: [A, B]
    correct_answer: 1
    explanation: "   "
`)
	q, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if q.Title != "Мій тест" {
		t.Errorf("Title = %q", q.Title)
	}
	if q.ShowRightAnswer || !q.RandomizeQuestions || !q.ShowAnswersOnCompletion || q.ShowFinalList || !q.ShowExplanation {
		t.Errorf("settings: %+v", q)
	}
	if q.TimeLimit != (timelimits.Settings{Mode: timelimits.ModePerQuestion, Seconds: 30}) {
		t.Errorf("TimeLimit = %v", q.TimeLimit)
	}
	if !reflect.DeepEqual(q.QuestionTimeLimits, map[int]int{1: 10, 2: 30}) {
		t.Errorf("QuestionTimeLimits = %v", q.QuestionTimeLimits)
	}
	if len(q.ExplanationsHTML) != 1 {
		t.Fatalf("only question 1 has an explanation: %v", q.ExplanationsHTML)
	}
	html := q.ExplanationsHTML[1]
	if !strings.Contains(html, "<strong>Why</strong>") || !strings.Contains(html, `src="`+explanations.StudentImagesURL+`a.png"`) {
		t.Errorf("ExplanationsHTML[1] = %q", html)
	}
}

func TestLoadSettingsUseTruthiness(t *testing.T) {
	// Loading does not validate; Python used "if value" on these settings.
	q, err := Parse([]byte("show_right_answer: \"\"\nrandomize_questions: \"no\"\nquestions: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if q.ShowRightAnswer || !q.RandomizeQuestions {
		t.Errorf("an empty text is false, a non-empty text is true: %+v", q)
	}
	if len(q.Questions) != 0 {
		t.Errorf("an empty question list loads, as in Python: %v", q.Questions)
	}
}

func TestLoadWarnsAboutBrokenTimeLimit(t *testing.T) {
	q, err := Parse([]byte("time_limit: {per_question: 5, whole_quiz: 9}\nquestions:\n  - {question: Q, options: [A, B], correct_answer: 0}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if q.TimeLimit != timelimits.NoLimit || !strings.HasPrefix(q.TimeLimitWarning, "Ignoring malformed time_limit") {
		t.Errorf("TimeLimit = %v, warning %q", q.TimeLimit, q.TimeLimitWarning)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"- a\n- b\n":                   "",
		"title: x\n":                   "questions",
		"questions: {a: 1}\n":          "questions",
		"questions: [5]\n":             "Question 1",
		"questions: [{question: Q}]\n": "Question 1",
		"questions: [{question: Q, options: [A, B]}]\n":                                  "correct_answer",
		"title: 5\nquestions: []\n":                                                      "title",
		"questions: [{question: Q, options: [A, B], correct_answer: 0, points: abc}]\n":  "points",
		"questions: [{question: Q, options: [A, B], correct_answer: 0, points: .inf}]\n": "points",
		"questions: [{question: Q, options: [A, B], correct_answer: 0, file: 5}]\n":      "file",
		"questions: [\n": "",
	}
	for text, part := range cases {
		_, err := Parse([]byte(text))
		if err == nil {
			t.Errorf("Parse(%q) should fail", text)
			continue
		}
		if !strings.Contains(err.Error(), part) {
			t.Errorf("Parse(%q) error %q should mention %q", text, err, part)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("a missing file should give an error")
	}
	// Text questions need neither options nor correct_answer; points may be text that float() reads.
	if _, err := Parse([]byte("questions: [{question: Q, checker: x}, {question: Q, options: [A, B], correct_answer: 0, points: '2'}]\n")); err != nil {
		t.Errorf("Parse should accept these questions: %v", err)
	}
}

func TestQuestionAccessors(t *testing.T) {
	q, err := Parse([]byte(`
questions:
  - question: Q1
    options: [A, B]
    correct_answer: [0, 1]
    points: 0.5
    file: data.xlsx
  - question: Q2
    checker: "x"
    stick_to_the_previous: yes
    points: "2"
  - image: /imgs/a.png
    options: [A, B]
    correct_answer: 0
    file: /attach/b.pdf
`))
	if err != nil {
		t.Fatal(err)
	}
	q1, q2, q3 := q.Questions[0], q.Questions[1], q.Questions[2]
	if q1.IsText() || !q2.IsText() || q3.IsText() {
		t.Error("IsText")
	}
	if !q1.IsMultipleChoice() || q3.IsMultipleChoice() {
		t.Error("IsMultipleChoice")
	}
	if q1.Points() != 0.5 || q2.Points() != 2 || q3.Points() != 1 {
		t.Errorf("Points = %v %v %v", q1.Points(), q2.Points(), q3.Points())
	}
	if q1.Sticky() || !q2.Sticky() || q3.Sticky() {
		t.Error("Sticky")
	}
	if q1.File() != "/attach/data.xlsx" || q2.File() != "" || q3.File() != "/attach/b.pdf" {
		t.Errorf("File = %q %q %q", q1.File(), q2.File(), q3.File())
	}
	if found, ok := q.QuestionByID(json.Number("2")); !ok || found.ID() != 2 {
		t.Errorf("QuestionByID(2) = %v, %v", found, ok)
	}
	if found, ok := q.QuestionByID(3.0); !ok || found.ID() != 3 {
		t.Error("QuestionByID(3.0) should find question 3, as Python's == does")
	}
	if _, ok := q.QuestionByID("2"); ok {
		t.Error(`QuestionByID("2") should find nothing`)
	}
	if _, ok := q.QuestionByID(4); ok {
		t.Error("QuestionByID(4) should find nothing")
	}
}

func TestNormalizePoints(t *testing.T) {
	cases := map[float64]float64{
		1:                   1,
		0.5:                 0.5,
		0.1 + 0.2:           0.3,
		1.005:               1, // the float is 1.00499..., as in Python
		2.675:               2.67,
		-0.001:              0, // no negative zero
		123456789.123456789: 123456789.12,
	}
	for value, want := range cases {
		got := NormalizePoints(value)
		if got != want || math.Signbit(got) {
			t.Errorf("NormalizePoints(%v) = %v, want %v", value, got, want)
		}
	}
	a, b := 0.1, 0.2
	if got := SumPoints(a, b); got != 0.3 {
		t.Errorf("SumPoints(0.1, 0.2) = %v", got)
	}
	if got := SumPoints(); got != 0 {
		t.Errorf("SumPoints() = %v", got)
	}
	if got := SumPoints([]float64{1, 0.25, 0.25}...); got != 1.5 {
		t.Errorf("SumPoints = %v", got)
	}
}

func TestFormatPoints(t *testing.T) {
	// str(normalize_points(x)) in Python: whole values print without ".0".
	cases := map[float64]string{
		3:                   "3",
		0.5:                 "0.5",
		1.25:                "1.25",
		0.30000000000000004: "0.3",
		100:                 "100",
		1e16:                "10000000000000000",
		0:                   "0",
	}
	for value, want := range cases {
		if got := FormatPoints(value); got != want {
			t.Errorf("FormatPoints(%v) = %q, want %q", value, got, want)
		}
	}
	data, _ := json.Marshal(map[string]float64{"a": NormalizePoints(3), "b": NormalizePoints(0.1 + 0.2)})
	if string(data) != `{"a":3,"b":0.3}` {
		t.Errorf("points in JSON: %s", data)
	}
}

func questionsWithSticky(sticky ...bool) []Question {
	questions := make([]Question, len(sticky))
	for i, s := range sticky {
		questions[i] = Question{"id": i + 1, "question": "Q", "options": []any{"A", "B"}, "correct_answer": 0}
		if s {
			questions[i]["stick_to_the_previous"] = true
		}
	}
	return questions
}

// checkGroupsStayTogether fails when a group is not one block, in its own order.
func checkGroupsStayTogether(t *testing.T, order []int, groups [][]int) {
	t.Helper()
	for _, group := range groups {
		start := slices.Index(order, group[0])
		if start < 0 || start+len(group) > len(order) || !slices.Equal(order[start:start+len(group)], group) {
			t.Fatalf("order %v breaks the group %v", order, group)
		}
	}
}

func TestRandomOrderKeepsStickyGroups(t *testing.T) {
	// Questions 2, 4 and 5 stick to the previous one: groups [1 2] [3 4 5] [6].
	questions := questionsWithSticky(false, true, false, true, true, false)
	groups := [][]int{{1, 2}, {3, 4, 5}, {6}}
	seen := map[string]bool{}
	rnd := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		order := RandomOrder(questions, rnd)
		if len(order) != 6 {
			t.Fatalf("order %v", order)
		}
		sorted := slices.Clone(order)
		slices.Sort(sorted)
		if !slices.Equal(sorted, []int{1, 2, 3, 4, 5, 6}) {
			t.Fatalf("order %v is not a permutation", order)
		}
		checkGroupsStayTogether(t, order, groups)
		seen[jsonText(order)] = true
	}
	if len(seen) != 6 { // 3 groups can be ordered in 3! = 6 ways
		t.Errorf("saw %d different orders, want all 6", len(seen))
	}
}

func jsonText(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestRandomOrderEdgeCases(t *testing.T) {
	if order := RandomOrder(nil, nil); len(order) != 0 {
		t.Errorf("no questions: %v", order)
	}
	// A sticky first question has nothing to stick to and starts a group.
	order := RandomOrder(questionsWithSticky(true, true, true), nil)
	if !slices.Equal(order, []int{1, 2, 3}) {
		t.Errorf("all sticky: %v", order)
	}
	// Truthiness, as in Python: "yes" text sticks, 0 does not.
	questions := questionsWithSticky(false, false, false)
	questions[1]["stick_to_the_previous"] = "yes"
	questions[2]["stick_to_the_previous"] = 0
	for range 50 {
		checkGroupsStayTogether(t, RandomOrder(questions, nil), [][]int{{1, 2}, {3}})
	}
	// The same seed gives the same order.
	a := RandomOrder(questionsWithSticky(false, false, false, false, false), rand.New(rand.NewPCG(7, 7)))
	b := RandomOrder(questionsWithSticky(false, false, false, false, false), rand.New(rand.NewPCG(7, 7)))
	if !slices.Equal(a, b) {
		t.Errorf("same seed, different orders: %v %v", a, b)
	}
}
