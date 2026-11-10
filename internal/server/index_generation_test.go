package server_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// The Python tests read `let questions = [...]`, `<h1>` and the footer in
// static_dir/index.html. The Go page is a React page: static_dir/index.html
// has the escaped <title> and the boot JSON (<script id="webquiz-boot">).
// These ports read the same facts from there; the <h1> and the footer are
// drawn by the page from boot.title and boot.version.

func paOneQuiz(name string, quiz map[string]any) testutil.Options {
	return testutil.Options{Quizzes: map[string]any{name: quiz}}
}

func paSimpleQuestion(text string, options []any, correct int) map[string]any {
	return map[string]any{"question": text, "options": options, "correct_answer": correct}
}

func paBootQuestions(t *testing.T, html string) []any {
	t.Helper()
	questions, ok := pageBoot(t, html)["questions"].([]any)
	if !ok {
		t.Fatalf("boot has no questions list")
	}
	return questions
}

// py: tests/test_index_generation.py::test_questions_data_embedded_correctly
func TestQuestionsDataEmbeddedCorrectly(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("test_quiz.yaml", map[string]any{
		"title": "Data Embedding Test",
		"questions": []any{
			paSimpleQuestion("What is 2 + 2?", []any{"3", "4", "5", "6"}, 1),
			map[string]any{"question": "What color is the sky?", "options": []any{"Red", "Blue", "Green"}, "correct_answer": 1, "image": "/imgs/sky.png"},
		},
	}))
	questions := paBootQuestions(t, readIndex(t, ts))
	if len(questions) != 2 {
		t.Fatalf("should embed both questions, got %d", len(questions))
	}
	q1 := questions[0].(map[string]any)
	if jsonText(q1["id"]) != "1" || q1["question"] != "What is 2 + 2?" || jsonText(q1["options"]) != `["3","4","5","6"]` {
		t.Errorf("q1: %v", q1)
	}
	if _, has := q1["correct_answer"]; has {
		t.Error("correct answer should NOT be included in client data")
	}
	if _, has := q1["image"]; has {
		t.Error("image should not be present for this question")
	}
	q2 := questions[1].(map[string]any)
	if jsonText(q2["id"]) != "2" || q2["question"] != "What color is the sky?" || jsonText(q2["options"]) != `["Red","Blue","Green"]` {
		t.Errorf("q2: %v", q2)
	}
	if q2["image"] != "/imgs/sky.png" {
		t.Errorf("image should be included when present: %v", q2)
	}
	if _, has := q2["correct_answer"]; has {
		t.Error("correct answer should NOT be included in client data")
	}
}

// py: tests/test_index_generation.py::test_quiz_title_embedded
func TestQuizTitleEmbedded(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("title_test.yaml", map[string]any{
		"title": "Amazing Quiz Title!", "questions": []any{paSimpleQuestion("Test question?", []any{"A", "B"}, 0)},
	}))
	html := readIndex(t, ts)
	if !strings.Contains(html, "<title>Amazing Quiz Title!</title>") {
		t.Errorf("no title tag:\n%s", html)
	}
	// The <h1> is drawn by the page from the boot title.
	if pageBoot(t, html)["title"] != "Amazing Quiz Title!" {
		t.Errorf("boot title: %v", pageBoot(t, html)["title"])
	}
}

// py: tests/test_index_generation.py::test_webquiz_version_embedded
func TestWebquizVersionEmbedded(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// The footer "WebQuiz vX" is drawn by the page from boot.version.
	version, _ := pageBoot(t, readIndex(t, ts))["version"].(string)
	if !regexp.MustCompile(`^[\d\w.\-]+$`).MatchString(version) {
		t.Errorf("boot version %q", version)
	}
}

// py: tests/test_index_generation.py::test_correct_answers_excluded_from_client_data
func TestCorrectAnswersExcludedFromClientData(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("security_test.yaml", map[string]any{
		"title": "Security Test Quiz",
		"questions": []any{
			paSimpleQuestion("Secret answer test?", []any{"Wrong", "Correct!", "Also wrong"}, 1),
			paSimpleQuestion("Another secret?", []any{"A", "B", "C", "D"}, 3),
		},
	}))
	html := readIndex(t, ts)
	for _, q := range paBootQuestions(t, html) {
		if _, has := q.(map[string]any)["correct_answer"]; has {
			t.Errorf("question %v should not contain correct_answer", q)
		}
	}
	for _, text := range []string{`"correct_answer":`, `"correct_answer": 1`, `"correct_answer": 3`, "correct_answer: 1", "correct_answer: 3", `"correct_answer"`} {
		if strings.Contains(html, text) {
			t.Errorf("%q appears in the page", text)
		}
	}
}

// py: tests/test_index_generation.py::test_optional_fields_included
func TestOptionalFieldsIncluded(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("optional_fields.yaml", map[string]any{
		"title": "Optional Fields Test",
		"questions": []any{
			paSimpleQuestion("Text only question?", []any{"A", "B"}, 0),
			map[string]any{"question": "Question with image?", "options": []any{"Yes", "No"}, "correct_answer": 0, "image": "/imgs/test.jpg"},
			map[string]any{"options": []any{"Option 1", "Option 2"}, "correct_answer": 1, "image": "/imgs/image_only.png"},
			map[string]any{"image": "/imgs/question1.png", "options": []any{"/imgs/a.png", "/imgs/b.png", "/imgs/c.png"}, "correct_answer": 1},
		},
	}))
	questions := paBootQuestions(t, readIndex(t, ts))
	if len(questions) != 4 {
		t.Fatalf("got %d questions", len(questions))
	}
	q := func(i int) map[string]any { return questions[i].(map[string]any) }
	if q(0)["question"] != "Text only question?" || q(0)["image"] != nil {
		t.Errorf("q1: %v", q(0))
	}
	if _, has := q(0)["image"]; has {
		t.Errorf("q1 has an image: %v", q(0))
	}
	if q(1)["question"] != "Question with image?" || q(1)["image"] != "/imgs/test.jpg" {
		t.Errorf("q2: %v", q(1))
	}
	if _, has := q(2)["question"]; has {
		t.Errorf("a question without text should not have a question field: %v", q(2))
	}
	if q(2)["image"] != "/imgs/image_only.png" {
		t.Errorf("q3: %v", q(2))
	}
	if _, has := q(3)["question"]; has {
		t.Errorf("an image-only question should not have question text: %v", q(3))
	}
	if q(3)["image"] != "/imgs/question1.png" || jsonText(q(3)["options"]) != `["/imgs/a.png","/imgs/b.png","/imgs/c.png"]` {
		t.Errorf("q4: %v", q(3))
	}
}

// py: tests/test_index_generation.py::test_template_placeholder_replacement
func TestTemplatePlaceholderReplacement(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("placeholder_test.yaml", map[string]any{
		"title": "Placeholder Test Quiz", "questions": []any{paSimpleQuestion("Test?", []any{"Yes", "No"}, 0)},
	}))
	html := readIndex(t, ts)
	for _, marker := range []string{"__WEBQUIZ_BOOT__", "__WEBQUIZ_TITLE__"} {
		if strings.Contains(html, marker) {
			t.Errorf("%s was not replaced", marker)
		}
	}
	boot := pageBoot(t, html)
	version, _ := boot["version"].(string)
	if !strings.Contains(html, "Placeholder Test Quiz") || len(paBootQuestions(t, html)) != 1 || version == "" {
		t.Errorf("the data is missing:\n%s", html)
	}
}

// py: tests/test_index_generation.py::test_index_html_file_created_in_static_dir
func TestIndexHTMLFileCreatedInStaticDir(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if info, err := os.Stat(ts.StaticDir); err != nil || !info.IsDir() {
		t.Fatalf("static directory should be created at %s: %v", ts.StaticDir, err)
	}
	info, err := os.Stat(filepath.Join(ts.StaticDir, "index.html"))
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		t.Errorf("index.html should be a file with content: %v", err)
	}
}

// py: tests/test_index_generation.py::test_index_html_content_is_valid_html
func TestIndexHTMLContentIsValidHTML(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	html := readIndex(t, ts)
	if !strings.HasPrefix(strings.ToLower(html), "<!doctype html>") {
		t.Errorf("no doctype:\n%s", html)
	}
	// Python wrote lang="en" in a fixed template; the Go page carries the
	// language of the interface (uk by default).
	for _, text := range []string{`<html lang="`, "<head>", "</head>", "<body>", "</body>", "</html>", `<meta charset="UTF-8"`, `<meta name="viewport"`, "<script", "</script>", `id="webquiz-boot"`} {
		if !strings.Contains(html, text) {
			t.Errorf("%q missing", text)
		}
	}
}

// py: tests/test_index_generation.py::test_regeneration_on_quiz_switch
func TestRegenerationOnQuizSwitch(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
		"default.yaml": map[string]any{"title": "First Quiz", "questions": []any{paSimpleQuestion("Question 1?", []any{"A", "B"}, 0)}},
		"quiz2.yaml":   map[string]any{"title": "Second Quiz", "questions": []any{paSimpleQuestion("Question 2?", []any{"X", "Y", "Z"}, 1)}},
	}})
	initial := readIndex(t, ts)
	if !strings.Contains(initial, "First Quiz") || !strings.Contains(initial, "Question 1?") {
		t.Fatalf("initial page:\n%s", initial)
	}
	res := ts.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "quiz2.yaml"}, testutil.WithCookies(ts.AdminCookies(t, "test123")...))
	if res.Status != 200 {
		t.Fatalf("switch: %d %s", res.Status, res.Body)
	}
	updated := readIndex(t, ts)
	if !strings.Contains(updated, "Second Quiz") || !strings.Contains(updated, "Question 2?") ||
		strings.Contains(updated, "First Quiz") || strings.Contains(updated, "Question 1?") {
		t.Errorf("updated page:\n%s", updated)
	}
	questions := paBootQuestions(t, updated)
	if len(questions) != 1 || jsonText(questions[0].(map[string]any)["options"]) != `["X","Y","Z"]` {
		t.Errorf("questions: %v", questions)
	}
}

// py: tests/test_index_generation.py::test_multiple_quiz_data_formats
func TestMultipleQuizDataFormats(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("varied.yaml", map[string]any{
		"title": "Varied Format Quiz", "description": "Testing different formats",
		"questions": []any{
			paSimpleQuestion("Simple text question?", []any{"A", "B"}, 0),
			map[string]any{"image": "/imgs/visual.png", "options": []any{"Visual A", "Visual B", "Visual C"}, "correct_answer": 2},
			map[string]any{"question": "Mixed question with image?", "image": "/imgs/mixed.jpg", "options": []any{"/imgs/opt1.png", "/imgs/opt2.png"}, "correct_answer": 1},
		},
	}))
	questions := paBootQuestions(t, readIndex(t, ts))
	if len(questions) != 3 {
		t.Fatalf("got %d questions", len(questions))
	}
	q := func(i int) map[string]any { return questions[i].(map[string]any) }
	if _, has := q(0)["question"]; !has {
		t.Error("q1 has no question")
	}
	if _, has := q(0)["image"]; has {
		t.Error("q1 has an image")
	}
	if _, has := q(1)["question"]; has {
		t.Error("q2 should have no question")
	}
	if q(1)["image"] != "/imgs/visual.png" {
		t.Errorf("q2: %v", q(1))
	}
	if _, has := q(2)["question"]; !has || q(2)["image"] != "/imgs/mixed.jpg" {
		t.Errorf("q3: %v", q(2))
	}
}

// py: tests/test_index_generation.py::test_empty_quiz_handling
func TestEmptyQuizHandling(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("empty.yaml", map[string]any{"title": "Empty Quiz", "questions": []any{}}))
	html := readIndex(t, ts)
	if !strings.Contains(html, "<title>Empty Quiz</title>") {
		t.Errorf("no title:\n%s", html)
	}
	if got := paBootQuestions(t, html); len(got) != 0 {
		t.Errorf("questions should be empty: %v", got)
	}
}

// py: tests/test_index_generation.py::test_special_characters_in_quiz_data
func TestSpecialCharactersInQuizData(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("special_chars.yaml", map[string]any{
		"title": `Quiz "with" special & characters`,
		"questions": []any{paSimpleQuestion(`What about "quotes" & <tags>?`,
			[]any{`Option with "quotes"`, "Option with & ampersand", "Option with <html> tags", "Option with 'single quotes'"}, 0)},
	}))
	html := readIndex(t, ts)
	if !strings.Contains(html, `<title>Quiz "with" special &amp; characters</title>`) {
		t.Errorf("title not escaped:\n%s", html)
	}
	boot := pageBoot(t, html) // fails if the JSON is broken
	if boot["title"] != `Quiz "with" special & characters` {
		t.Errorf("boot title: %v", boot["title"])
	}
	q := paBootQuestions(t, html)[0].(map[string]any)
	if q["question"] != `What about "quotes" & <tags>?` {
		t.Errorf("question: %v", q["question"])
	}
	options := q["options"].([]any)
	for i, want := range []string{`Option with "quotes"`, "Option with & ampersand", "Option with <html> tags", "Option with 'single quotes'"} {
		if options[i] != want {
			t.Errorf("option %d = %v, want %q", i, options[i], want)
		}
	}
	// No raw tag from the quiz text may appear in the page.
	if strings.Contains(html, "<tags>") || strings.Contains(html, "<html> tags") {
		t.Errorf("raw tags in the page:\n%s", html)
	}
}

// Python looked for user-select: none, a copy listener and a
// .text-answer-input rule in the page's inline script and style. The Go page
// keeps them in its built files (assets/*.css and *.js).
// py: tests/test_index_generation.py::test_question_text_copy_prevention
func TestQuestionTextCopyPrevention(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	html := readIndex(t, ts)
	var all strings.Builder
	for _, match := range regexp.MustCompile(`(?:src|href)="\./(assets/[^"]+)"`).FindAllStringSubmatch(html, -1) {
		res := ts.GetJSON(t, "/"+match[1])
		if res.Status != 200 {
			t.Fatalf("GET /%s: %d", match[1], res.Status)
		}
		all.Write(res.Body)
	}
	assets := all.String()
	if !regexp.MustCompile(`user-select:\s*none`).MatchString(assets) {
		t.Error("should have user-select: none to prevent text selection")
	}
	// A copy handler: addEventListener("copy" ...) or React's onCopy prop
	// (minified bundles keep the property name "onCopy" in a props object).
	if !regexp.MustCompile(`addEventListener\(\s*["']copy["']|onCopy\s*:`).MatchString(assets) {
		t.Error("should have a copy event handler to prevent copying")
	}
	if !strings.Contains(assets, "text-answer-input") {
		t.Error("text answer input should have a user-select override")
	}
}

// py: tests/test_index_generation.py::test_very_long_quiz_titles
func TestVeryLongQuizTitles(t *testing.T) {
	t.Parallel()
	longTitle := strings.Repeat("This is a very ", 50) + "long quiz title!"
	ts := testutil.StartServer(t, paOneQuiz("long_title.yaml", map[string]any{
		"title": longTitle, "questions": []any{paSimpleQuestion("Short question?", []any{"A", "B"}, 0)},
	}))
	html := readIndex(t, ts)
	if !strings.Contains(html, "<title>"+longTitle+"</title>") || pageBoot(t, html)["title"] != longTitle {
		t.Errorf("long title missing")
	}
	if strings.Count(html, "<title>") != 1 || strings.Count(html, "</title>") != 1 {
		t.Errorf("title tags: %d, %d", strings.Count(html, "<title>"), strings.Count(html, "</title>"))
	}
}

// py: tests/test_index_generation.py::test_tags_in_quiz_title_are_escaped
func TestTagsInQuizTitleAreEscaped(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, paOneQuiz("tags_title.yaml", map[string]any{
		"title": "VTX <vtx_band> setup", "questions": []any{paSimpleQuestion("Q?", []any{"A", "B"}, 0)},
	}))
	html := readIndex(t, ts)
	if !strings.Contains(html, "<title>VTX &lt;vtx_band&gt; setup</title>") {
		t.Errorf("title not escaped:\n%s", html)
	}
	// The page shows the boot title as text (React escapes it).
	if pageBoot(t, html)["title"] != "VTX <vtx_band> setup" {
		t.Errorf("boot title: %v", pageBoot(t, html)["title"])
	}
	if strings.Contains(html, "VTX <vtx_band> setup") {
		t.Error("the raw title is in the page")
	}
}

// py: tests/test_index_generation.py::test_script_end_tag_in_question_does_not_break_page
func TestScriptEndTagInQuestionDoesNotBreakPage(t *testing.T) {
	t.Parallel()
	text := "What does </script><b>bold</b> do?"
	ts := testutil.StartServer(t, paOneQuiz("script_tag.yaml", map[string]any{
		"title": "Script tag quiz", "questions": []any{paSimpleQuestion(text, []any{"</script>", "B"}, 0)},
	}))
	html := readIndex(t, ts)
	if strings.Contains(html, "</script><b>") {
		t.Error("the raw text is in the page: it would close the script")
	}
	q := paBootQuestions(t, html)[0].(map[string]any)
	if q["question"] != text || q["options"].([]any)[0] != "</script>" {
		t.Errorf("question: %v", q)
	}
}

// py: tests/test_index_generation.py::test_tags_in_registration_labels_are_escaped
func TestTagsInRegistrationLabelsAreEscaped(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{
		"registration": map[string]any{"username_label": "Name <full>", "fields": []any{"Group <class>"}},
	}})
	html := readIndex(t, ts)
	if strings.Contains(html, "<full>") || strings.Contains(html, "<class>") {
		t.Errorf("raw labels in the page:\n%s", html)
	}
	boot := pageBoot(t, html)
	fields, _ := boot["registration_fields"].([]any)
	if boot["username_label"] != "Name <full>" || len(fields) != 1 ||
		fields[0].(map[string]any)["label"] != "Group <class>" || fields[0].(map[string]any)["name"] != "group_<class>" {
		t.Errorf("boot: %s", jsonText(boot))
	}
}

// py: tests/test_index_generation.py::test_tags_in_quiz_titles_are_escaped_on_selection_page
func TestTagsInQuizTitlesAreEscapedOnSelectionPage(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
		"first.yaml":  map[string]any{"title": "First <one>", "questions": []any{paSimpleQuestion("Q?", []any{"A", "B"}, 0)}},
		"second.yaml": map[string]any{"title": "Second", "questions": []any{paSimpleQuestion("Q?", []any{"A", "B"}, 0)}},
	}})
	html := readIndex(t, ts)
	if strings.Contains(html, "<one>") {
		t.Errorf("raw title in the page:\n%s", html)
	}
	boot := pageBoot(t, html)
	if boot["state"] != "quiz_selection_required" ||
		!strings.Contains(jsonText(boot["quizzes"]), `{"filename":"first.yaml","title":"First \u003cone\u003e"}`) {
		t.Errorf("boot: %s", jsonText(boot))
	}
}
