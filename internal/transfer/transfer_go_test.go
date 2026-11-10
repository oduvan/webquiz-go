package transfer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// Tests of the quiz reference rewrite beyond the Python tests.

// The sender's quiz changed into broken YAML after it was offered: the
// receiver saves it as it came, and logs that the references could not be
// updated (Python's warning).
func TestBrokenReceivedQuizIsSavedWithAWarning(t *testing.T) {
	a, b, _ := twoMachines(t)
	writeQuizWithMedia(t, a)
	b.write(t, "images", "map.png", "another map") // map.png gets a new name
	request := sendFiles(t, a, b, files(file("quizzes", "geo.yaml")))
	a.write(t, "quizzes", "geo.yaml", "questions: [unclosed")

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 0 {
		t.Errorf("failed = %+v", result.Failed)
	}
	if got := b.read(t, "quizzes", "geo.yaml"); got != "questions: [unclosed" {
		t.Errorf("geo.yaml = %q", got)
	}
	if !strings.Contains(b.logText(), "Cannot update file references in received quiz: ") {
		t.Errorf("no warning in the log:\n%s", b.logText())
	}
}

// Merge keys, anchors and aliases stay usable: an alias that points to a
// renamed image gets the new name, the anchored value keeps its anchor.
func TestRewriteKeepsMergeKeysAndAnchors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	writeFile(t, path, "defaults: &d\n  points: 2\nimg: &i /imgs/map.png\nquestions:\n- question: Q\n  <<: *d\n  image: *i\n  options: [*i, b]\n  correct_answer: 0\n")

	changed, err := rewriteQuizMedia(path, map[mediaFile]string{{"images", "map.png"}: "m2.png"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	text := readFile(t, path)
	for _, part := range []string{"<<: *d", "img: &i /imgs/map.png", "image: /imgs/m2.png", "options: [/imgs/m2.png, b]"} {
		if !strings.Contains(text, part) {
			t.Errorf("%q is missing in:\n%s", part, text)
		}
	}
	data, err := pyval.LoadYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	question := data.(map[string]any)["questions"].([]any)[0].(map[string]any)
	if pyval.Str(question["points"]) != "2" {
		t.Errorf("the merge key does not work any more: %v", question)
	}
}

// ruamel.yaml refused a quiz with a repeated key; here every repeated
// reference is updated and PyYAML's reading (the last key) is right.
func TestRewriteHandlesRepeatedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	writeFile(t, path, "title: A\ntitle: B\nquestions:\n- question: Q\n  image: /imgs/map.png\n  image: /imgs/map.png\n  options: [a, b]\n  correct_answer: 0\n")
	changed, err := rewriteQuizMedia(path, map[mediaFile]string{{"images", "map.png"}: "m2.png"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if text := readFile(t, path); strings.Count(text, "image: /imgs/m2.png") != 2 || !strings.Contains(text, "title: B") {
		t.Errorf("text:\n%s", text)
	}
}

// Values that are not text are not references: numbers, lists, null.
func TestRewriteSkipsValuesThatAreNotText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	text := "questions:\n- question: Q\n  image: null\n  file: 5\n  options: [[/imgs/map.png], 1, true]\n  correct_answer: 0\n"
	writeFile(t, path, text)
	changed, err := rewriteQuizMedia(path, map[mediaFile]string{{"images", "map.png"}: "m2.png"})
	if err != nil || changed || readFile(t, path) != text {
		t.Errorf("changed=%v err=%v\n%s", changed, err, readFile(t, path))
	}
}
