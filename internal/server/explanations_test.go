package server_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_explanations.py: explanations in the answer reply, the
// quiz settings, the checks on save, unite and the preview.

const explanationText = "**Why** it is A"

func explainQuestion(text string, explanation any) map[string]any {
	q := choice(text, []any{"A", "B"}, 0)
	if explanation != nil {
		q["explanation"] = explanation
	}
	return q
}

// explainQuiz is quiz() of the Python file: default.yaml with explanations
// on. mutate changes the quiz before it is written.
func explainQuiz(questions []any, mutate func(map[string]any)) map[string]any {
	if questions == nil {
		questions = []any{explainQuestion("Q1?", explanationText), explainQuestion("Q2?", nil)}
	}
	data := map[string]any{"title": "Explanations", "show_right_answer": true, "show_explanation": true, "questions": questions}
	if mutate != nil {
		mutate(data)
	}
	return map[string]any{"default.yaml": data}
}

func answerReply(t *testing.T, ts *testutil.TestServer, userID string, questionID any, selected any) map[string]any {
	t.Helper()
	res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": questionID, "selected_answer": selected})
	if res.Status != 200 {
		t.Fatalf("submit-answer: %d %s", res.Status, res.Body)
	}
	return res.JSON(t)
}

func hasExplanation(reply map[string]any) bool {
	_, has := reply["explanation_html"]
	return has
}

// py: tests/test_explanations.py::test_answer_reply_has_explanation_html
func TestAnswerReplyHasExplanationHTML(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	reply := answerReply(t, ts, ts.RegisterStudent(t, "S"), 1, 0)
	if html, _ := reply["explanation_html"].(string); !strings.Contains(html, "<strong>Why</strong>") {
		t.Errorf("reply: %v", reply)
	}
}

// py: tests/test_explanations.py::test_question_without_explanation_has_none
func TestQuestionWithoutExplanationHasNone(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	userID := ts.RegisterStudent(t, "S")
	answerReply(t, ts, userID, 1, 0)
	if reply := answerReply(t, ts, userID, 2, 0); hasExplanation(reply) {
		t.Errorf("reply: %v", reply)
	}
}

// py: tests/test_explanations.py::test_no_explanation_when_option_off
func TestNoExplanationWhenOptionOff(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, func(q map[string]any) { delete(q, "show_explanation") })})
	if reply := answerReply(t, ts, ts.RegisterStudent(t, "S"), 1, 0); hasExplanation(reply) {
		t.Errorf("reply: %v", reply)
	}
}

// py: tests/test_explanations.py::test_no_explanation_when_right_answer_hidden
func TestNoExplanationWhenRightAnswerHidden(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, func(q map[string]any) { q["show_right_answer"] = false })})
	if reply := answerReply(t, ts, ts.RegisterStudent(t, "S"), 1, 0); hasExplanation(reply) {
		t.Errorf("reply: %v", reply)
	}
}

// py: tests/test_explanations.py::test_explanation_is_not_in_page_before_answer
func TestExplanationIsNotInPageBeforeAnswer(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	page := ts.GetJSON(t, "/").Text()
	if strings.Contains(page, "it is A") || strings.Contains(page, "Why") {
		t.Errorf("the quiz page has the explanation:\n%s", page)
	}
}

// py: tests/test_explanations.py::test_repeated_answer_returns_same_explanation
func TestRepeatedAnswerReturnsSameExplanation(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	userID := ts.RegisterStudent(t, "S")
	first := answerReply(t, ts, userID, 1, 0)
	second := answerReply(t, ts, userID, 1, 0)
	if first["explanation_html"] == nil || second["explanation_html"] != first["explanation_html"] {
		t.Errorf("first %v, second %v", first["explanation_html"], second["explanation_html"])
	}
}

// py: tests/test_explanations.py::test_per_question_timeout_reply_has_explanation
func TestPerQuestionTimeoutReplyHasExplanation(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, func(q map[string]any) { q["time_limit"] = map[string]any{"per_question": 30} })})
	userID := ts.RegisterStudent(t, "S")
	questionStartReply(t, ts, userID, 1)
	reply := answerReply(t, ts, userID, 1, nil)
	if html, _ := reply["explanation_html"].(string); reply["timed_out"] != true || !strings.Contains(html, "<strong>Why</strong>") {
		t.Errorf("reply: %v", reply)
	}
}

// py: tests/test_explanations.py::test_whole_quiz_timeout_reply_has_no_explanation
func TestWholeQuizTimeoutReplyHasNoExplanation(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, func(q map[string]any) { q["time_limit"] = map[string]any{"whole_quiz": 30} })})
	userID := ts.RegisterStudent(t, "S")
	questionStartReply(t, ts, userID, 1)
	reply := answerReply(t, ts, userID, 1, nil)
	if reply["timed_out"] != true || hasExplanation(reply) {
		t.Errorf("reply: %v", reply)
	}
}

// py: tests/test_explanations.py::test_random_order_explanation_follows_question_id
func TestRandomOrderExplanationFollowsQuestionID(t *testing.T) {
	t.Parallel()
	var questions []any
	for n := 1; n <= 3; n++ {
		questions = append(questions, explainQuestion(fmt.Sprintf("Q%d?", n), fmt.Sprintf("E%d", n)))
	}
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(questions, func(q map[string]any) { q["randomize_questions"] = true })})
	registration := ts.PostJSON(t, "/api/register", map[string]any{"username": "Student"}).JSON(t)
	firstID := registration["question_order"].([]any)[0].(json.Number)
	reply := answerReply(t, ts, registration["user_id"].(string), firstID, 0)
	if html, _ := reply["explanation_html"].(string); !strings.Contains(html, "E"+firstID.String()) {
		t.Errorf("question %s: %v", firstID, reply["explanation_html"])
	}
}

// py: tests/test_explanations.py::test_hand_edited_bad_values_still_load
func TestHandEditedBadValuesStillLoad(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(
		[]any{explainQuestion("Q1?", 5), explainQuestion("Q2?", nil)},
		func(q map[string]any) { q["show_explanation"] = "yes" })})
	if reply := answerReply(t, ts, ts.RegisterStudent(t, "S"), 1, 0); hasExplanation(reply) {
		t.Errorf("reply: %v", reply)
	}
}

// py: tests/test_explanations.py::test_create_quiz_rejects_bad_explanation_values
func TestCreateQuizRejectsBadExplanationValues(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	create := func(name string, data map[string]any) int {
		return admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{"filename": name, "mode": "wizard", "quiz_data": data}).Status
	}
	badOption := map[string]any{"show_explanation": "yes", "questions": []any{explainQuestion("Q1?", nil)}}
	badText := map[string]any{"questions": []any{explainQuestion("Q1?", []any{"x"})}}
	good := map[string]any{"show_explanation": true, "questions": []any{explainQuestion("Q1?", "Text")}}
	if got := create("bad1.yaml", badOption); got != 400 {
		t.Errorf("bad option: %d", got)
	}
	if got := create("bad2.yaml", badText); got != 400 {
		t.Errorf("bad text: %d", got)
	}
	if got := create("good.yaml", good); got != 200 {
		t.Errorf("good: %d", got)
	}
}

// py: tests/test_explanations.py::test_start_setting_turns_explanations_off
func TestStartSettingTurnsExplanationsOff(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	res := startQuiz(t, admin, "default.yaml", map[string]any{"show_explanation": false})
	if res.Status != 200 || res.JSON(t)["settings"].(map[string]any)["show_explanation"] != false {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if reply := answerReply(t, ts, ts.RegisterStudent(t, "S"), 1, 0); hasExplanation(reply) {
		t.Errorf("reply: %v", reply)
	}
	if readQuizFile(t, ts, "default.yaml")["show_explanation"] != true {
		t.Error("the start setting was written to the quiz file")
	}
}

// py: tests/test_explanations.py::test_start_setting_rejects_non_boolean
func TestStartSettingRejectsNonBoolean(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	if res := startQuiz(t, admin, "default.yaml", map[string]any{"show_explanation": "no"}); res.Status != 400 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_explanations.py::test_unite_copies_show_explanation
func TestUniteCopiesShowExplanation(t *testing.T) {
	t.Parallel()
	quizzes := explainQuiz(nil, nil)
	quizzes["other.yaml"] = map[string]any{"title": "Other", "questions": []any{explainQuestion("Q3?", nil)}}
	ts, admin := withAdmin(t, testutil.Options{Quizzes: quizzes})
	res := unite(t, admin, []any{"default.yaml", "other.yaml"}, "united")
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	united := readQuizFile(t, ts, "united.yaml")
	if united["show_explanation"] != true || united["questions"].([]any)[0].(map[string]any)["explanation"] != explanationText {
		t.Errorf("%v", united)
	}
}

func preview(t *testing.T, ts *testutil.TestServer, admin *testutil.Client, body any) *testutil.Response {
	t.Helper()
	if admin == nil {
		return ts.PostJSON(t, "/api/admin/preview-explanation", body)
	}
	return admin.PostJSON(t, "/api/admin/preview-explanation", body)
}

// py: tests/test_explanations.py::test_preview_needs_admin_session
func TestPreviewNeedsAdminSession(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	if res := preview(t, ts, nil, map[string]any{"markdown": "x"}); res.Status != 401 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_explanations.py::test_preview_renders_with_admin_image_path
func TestPreviewRendersWithAdminImagePath(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	res := preview(t, ts, admin, map[string]any{"markdown": "![a](/imgs/a.png) <b>x</b>"})
	html, _ := res.JSON(t)["html"].(string)
	if res.Status != 200 || !strings.Contains(html, `src="../imgs/a.png"`) || !strings.Contains(html, "&lt;b&gt;") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_explanations.py::test_preview_rejects_non_string
func TestPreviewRejectsNonString(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: explainQuiz(nil, nil)})
	if res := preview(t, ts, admin, map[string]any{"markdown": 5}); res.Status != 400 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}
