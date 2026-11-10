package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// testWriter writes the manager's log to the test log.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
}

// ----- Name and file validation -----

// py: tests/test_transfer.py::test_server_name_validation
func TestServerNameValidation(t *testing.T) {
	for _, name := range []any{"room-12", "Teacher_PC.2"} {
		if !isValidServerName(name) {
			t.Errorf("%v should be valid", name)
		}
	}
	for _, name := range []any{"", "../x", "a/b", ".hidden", "a..b", strings.Repeat("x", 65), nil} {
		if isValidServerName(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

// py: tests/test_transfer.py::test_allowed_files
func TestAllowedFiles(t *testing.T) {
	allowed := [][2]any{
		{"quizzes", "math.yaml"}, {"quizzes", "math.YML"}, {"logs", "0001.log"}, {"csv", "results.csv"},
		{"quizzes", "Математика 5 клас.yaml"}, {"images", "map.PNG"}, {"attachments", "data.xlsx"},
		{"attachments", "README"},
	}
	for _, c := range allowed {
		if !isAllowedFile(c[0], c[1]) {
			t.Errorf("%v should be allowed", c)
		}
	}
	refused := [][2]any{
		{"quizzes", "math.yaml.backup"}, {"logs", "0001.csv"}, {"static", "index.html"},
		{"quizzes", "../math.yaml"}, {"quizzes", `sub\math.yaml`}, {"quizzes", ".math.yaml"},
		{"quizzes", "<img src=x onerror=alert(1)>.yaml"}, {"quizzes", "a:b.yaml"}, {"quizzes", "a\nb.yaml"},
		{"quizzes", nil}, {"images", "map.html"}, {"attachments", "../data.xlsx"},
	}
	for _, c := range refused {
		if isAllowedFile(c[0], c[1]) {
			t.Errorf("%q should not be allowed", c)
		}
	}
}

// ----- Full flow -----

// py: tests/test_transfer.py::test_accept_downloads_all_files
func TestAcceptDownloadsAllFiles(t *testing.T) {
	a, b, tunnel := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "title: Math\n")
	a.write(t, "logs", "0001.log", "log line\n")
	a.write(t, "csv", "results.csv", "a,b\n1,2\n")

	request := sendFiles(t, a, b, files(file("quizzes", "math.yaml"), file("logs", "0001.log"), file("csv", "results.csv")))
	if request.From != "room-a" {
		t.Errorf("from = %q", request.From)
	}
	var sizes, cases, targets []string
	for _, f := range request.Files {
		sizes = append(sizes, f.Size.String())
		cases = append(cases, f.Case)
		targets = append(targets, f.Target)
	}
	if !slices.Equal(sizes, []string{"12", "9", "8"}) {
		t.Errorf("sizes = %v", sizes)
	}
	sum := sha256.Sum256([]byte("title: Math\n"))
	b.manager.mu.Lock()
	stored := b.manager.incoming[0].files[0].sha256
	b.manager.mu.Unlock()
	if stored != hex.EncodeToString(sum[:]) {
		t.Errorf("stored sha256 = %q", stored)
	}
	if !slices.Equal(cases, []string{"new", "new", "new"}) {
		t.Errorf("cases = %v", cases)
	}
	// Logs and CSV files are saved with the sender name
	if !slices.Equal(targets, []string{"math.yaml", "room-a_0001.log", "room-a_results.csv"}) {
		t.Errorf("targets = %v", targets)
	}
	if _, ok := asJSON(t, request).(map[string]any)["token"]; ok {
		t.Error("the request view shows the token")
	}
	if b.lastMessage(t)["type"] != "transfer_request" {
		t.Errorf("last message = %v", b.lastMessage(t))
	}

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 0 {
		t.Errorf("failed = %+v", result.Failed)
	}
	var savedAs []string
	for _, f := range result.Saved {
		savedAs = append(savedAs, f.SavedAs)
	}
	if !slices.Equal(savedAs, []string{"math.yaml", "room-a_0001.log", "room-a_results.csv"}) {
		t.Errorf("saved as %v", savedAs)
	}
	if got := b.read(t, "quizzes", "math.yaml"); got != "title: Math\n" {
		t.Errorf("math.yaml = %q", got)
	}
	if got := b.read(t, "logs", "room-a_0001.log"); got != "log line\n" {
		t.Errorf("log = %q", got)
	}
	if got := b.read(t, "csv", "room-a_results.csv"); got != "a,b\n1,2\n" {
		t.Errorf("csv = %q", got)
	}
	if got := listIncoming(t, b); len(got) != 0 {
		t.Errorf("incoming = %+v", got)
	}
	if got := b.lastMessage(t); got["type"] != "transfer_request_removed" || got["id"] != request.ID || len(got) != 2 {
		t.Errorf("last message = %v", got)
	}

	// The sender learns the result and stops serving the files
	if got := state(t, a).Outgoing[0].Status; got != "accepted" {
		t.Errorf("offer status = %q", got)
	}
	if a.lastMessage(t)["type"] != "transfer_result" {
		t.Errorf("sender's last message = %v", a.lastMessage(t))
	}
	a.manager.mu.Lock()
	token := a.manager.outgoing[0].token
	a.manager.mu.Unlock()
	response, err := http.Get(tunnel.baseURL + "/room-a/api/transfer/download/" + token + "/quizzes/math.yaml")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("download after accept: %d", response.StatusCode)
	}
}

// py: tests/test_transfer.py::test_cases_and_default_actions
func TestCasesAndDefaultActions(t *testing.T) {
	// Every incoming file gets a case and a safe default action.
	a, b, _ := twoMachines(t)
	for name, content := range map[string]string{"same.yaml": "x", "diff.yaml": "new", "active.yaml": "a", "fresh.yaml": "f"} {
		a.write(t, "quizzes", name, content)
	}
	b.write(t, "quizzes", "same.yaml", "x")
	b.write(t, "quizzes", "diff.yaml", "old")
	b.write(t, "quizzes", "active.yaml", "b")
	b.setFilesInUse("", b.path("quizzes", "active.yaml")) // "" is Python's None

	names := []string{"same.yaml", "diff.yaml", "active.yaml", "fresh.yaml"}
	var offered []any
	for _, name := range names {
		offered = append(offered, file("quizzes", name))
	}
	request := sendFiles(t, a, b, offered)

	expected := map[string]struct {
		kase    string
		actions []string
		dflt    string
	}{
		"same.yaml":   {"same", []string{"replace", "rename", "skip"}, "skip"},
		"diff.yaml":   {"different", []string{"replace", "rename", "skip"}, "rename"},
		"active.yaml": {"in_use", []string{"rename", "skip"}, "rename"},
		"fresh.yaml":  {"new", []string{"save", "rename", "skip"}, "save"},
	}
	for name, want := range expected {
		plan := planOf(t, request, name)
		if plan.Case != want.kase || !slices.Equal(plan.Actions, want.actions) || plan.DefaultAction != want.dflt {
			t.Errorf("%s: got %s %v %s", name, plan.Case, plan.Actions, plan.DefaultAction)
		}
		if plan.SuggestedName != "room-a_"+name {
			t.Errorf("%s: suggested %q", name, plan.SuggestedName)
		}
	}

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved := map[string]string{}
	for _, f := range result.Saved {
		saved[f.Name] = f.SavedAs
	}
	want := map[string]string{"diff.yaml": "room-a_diff.yaml", "active.yaml": "room-a_active.yaml", "fresh.yaml": "fresh.yaml"}
	if !mapsEqual(saved, want) {
		t.Errorf("saved = %v", saved)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Name != "same.yaml" {
		t.Errorf("skipped = %+v", result.Skipped)
	}
	if got := b.read(t, "quizzes", "diff.yaml"); got != "old" {
		t.Errorf("diff.yaml = %q", got)
	}
	if got := b.read(t, "quizzes", "active.yaml"); got != "b" {
		t.Errorf("active.yaml = %q", got)
	}
	wantFiles := []string{"active.yaml", "diff.yaml", "fresh.yaml", "room-a_active.yaml", "room-a_diff.yaml", "same.yaml"}
	if got := b.files(t, "quizzes"); !slices.Equal(got, wantFiles) {
		t.Errorf("files = %v", got)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// py: tests/test_transfer.py::test_suggested_names_never_overwrite
func TestSuggestedNamesNeverOverwrite(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "new")
	a.write(t, "logs", "0001.log", "new log")
	b.write(t, "quizzes", "math.yaml", "old")
	b.write(t, "quizzes", "room-a_math.yaml", "older")
	b.write(t, "logs", "room-a_0001.log", "old log")

	request := sendFiles(t, a, b, files(file("quizzes", "math.yaml"), file("logs", "0001.log")))
	if got := planOf(t, request, "math.yaml").SuggestedName; got != "room-a_math_2.yaml" {
		t.Errorf("math.yaml suggested %q", got)
	}
	if got := planOf(t, request, "0001.log").SuggestedName; got != "room-a_0001_2.log" {
		t.Errorf("0001.log suggested %q", got)
	}

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	var savedAs []string
	for _, f := range result.Saved {
		savedAs = append(savedAs, f.SavedAs)
	}
	if !slices.Equal(savedAs, []string{"room-a_math_2.yaml", "room-a_0001_2.log"}) {
		t.Errorf("saved as %v", savedAs)
	}
	for _, c := range [][3]string{{"quizzes", "math.yaml", "old"}, {"quizzes", "room-a_math.yaml", "older"}, {"logs", "room-a_0001.log", "old log"}} {
		if got := b.read(t, c[0], c[1]); got != c[2] {
			t.Errorf("%s = %q", c[1], got)
		}
	}
}

// py: tests/test_transfer.py::test_admin_chooses_replace_rename_skip
func TestAdminChoosesReplaceRenameSkip(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "new math")
	a.write(t, "quizzes", "geo.yaml", "new geo")
	a.write(t, "logs", "0001.log", "log")
	a.write(t, "csv", "results.csv", "a,b")
	b.write(t, "quizzes", "math.yaml", "old math")

	request := sendFiles(t, a, b, files(file("quizzes", "math.yaml"), file("quizzes", "geo.yaml"), file("logs", "0001.log"), file("csv", "results.csv")))
	result, err := b.manager.Accept(t.Context(), request.ID, []any{
		map[string]any{"type": "quizzes", "name": "math.yaml", "action": "replace"},
		map[string]any{"type": "quizzes", "name": "geo.yaml", "action": "rename", "new_name": "Географія 7.yaml"},
		map[string]any{"type": "logs", "name": "0001.log", "action": "skip"},
		// results.csv: no choice, default (save as room-a_results.csv)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 0 {
		t.Errorf("failed = %+v", result.Failed)
	}
	var saved [][3]string
	for _, f := range result.Saved {
		saved = append(saved, [3]string{f.Name, f.Action, f.SavedAs})
	}
	wantSaved := [][3]string{{"math.yaml", "replace", "math.yaml"}, {"geo.yaml", "rename", "Географія 7.yaml"}, {"results.csv", "save", "room-a_results.csv"}}
	if !slices.Equal(saved, wantSaved) {
		t.Errorf("saved = %v", saved)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Name != "0001.log" || result.Skipped[0].Action != "skip" {
		t.Errorf("skipped = %+v", result.Skipped)
	}
	if got := b.read(t, "quizzes", "math.yaml"); got != "new math" {
		t.Errorf("math.yaml = %q", got)
	}
	if got := b.read(t, "quizzes", "Географія 7.yaml"); got != "new geo" {
		t.Errorf("Географія 7.yaml = %q", got)
	}
	if got := b.files(t, "logs"); len(got) != 0 {
		t.Errorf("logs = %v", got)
	}
	if got := state(t, a).Outgoing[0].Status; got != "accepted" {
		t.Errorf("offer status = %q", got)
	}
}

func TestInvalidChoicesKeepRequestOpen(t *testing.T) {
	cases := []struct {
		py      string
		choice  map[string]any
		message string
	}{
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice0-not possible]",
			choice: map[string]any{"name": "active.yaml", "action": "replace"}, message: "not possible"},
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice1-not possible]",
			choice: map[string]any{"name": "fresh.yaml", "action": "replace"}, message: "not possible"},
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice2-not possible]",
			choice: map[string]any{"name": "diff.yaml", "action": "save"}, message: "not possible"},
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice3-not possible]",
			choice: map[string]any{"name": "diff.yaml", "action": "delete"}, message: "not possible"},
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice4-already exists]",
			choice: map[string]any{"name": "fresh.yaml", "action": "rename", "new_name": "diff.yaml"}, message: "already exists"},
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice5-not a valid name]",
			choice: map[string]any{"name": "fresh.yaml", "action": "rename", "new_name": "fresh.txt"}, message: "not a valid name"},
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice6-not a valid name]",
			choice: map[string]any{"name": "fresh.yaml", "action": "rename", "new_name": "../x.yaml"}, message: "not a valid name"},
		{py: "tests/test_transfer.py::test_invalid_choices_keep_request_open[choice7-chosen for two files]",
			choice: map[string]any{"name": "diff.yaml", "action": "rename", "new_name": "fresh.yaml"}, message: "chosen for two files"},
	}
	for _, c := range cases {
		t.Run(c.message, func(t *testing.T) {
			a, b, _ := twoMachines(t)
			for _, name := range []string{"diff.yaml", "active.yaml", "fresh.yaml"} {
				a.write(t, "quizzes", name, "from a")
			}
			b.write(t, "quizzes", "diff.yaml", "b")
			b.write(t, "quizzes", "active.yaml", "b")
			b.setFilesInUse(b.path("quizzes", "active.yaml"))
			request := sendFiles(t, a, b, files(file("quizzes", "diff.yaml"), file("quizzes", "active.yaml"), file("quizzes", "fresh.yaml")))

			choice := map[string]any{"type": "quizzes"}
			for k, v := range c.choice {
				choice[k] = v
			}
			_, err := b.manager.Accept(t.Context(), request.ID, []any{choice})

			te := transferErr(t, err)
			if te.Status != 400 || !strings.Contains(te.Message, c.message) {
				t.Errorf("error %d %q, want 400 with %q", te.Status, te.Message, c.message)
			}
			if got := b.files(t, "quizzes"); !slices.Equal(got, []string{"active.yaml", "diff.yaml"}) {
				t.Errorf("files = %v (nothing should be downloaded)", got)
			}
			if got := listIncoming(t, b); len(got) != 1 || got[0].ID != request.ID {
				t.Errorf("incoming = %+v (the admin can choose again)", got)
			}
			if got := state(t, a).Outgoing[0].Status; got != "waiting" {
				t.Errorf("offer status = %q", got)
			}
		})
	}
}

// py: tests/test_transfer.py::test_file_used_by_server_cannot_be_replaced_during_download
func TestFileUsedByServerCannotBeReplacedDuringDownload(t *testing.T) {
	// Choices are checked again right before a file is saved. Python
	// replaced _build_plan to start using the file after the plan was
	// made; here the file starts being used when the sender serves it,
	// which is also after the plan and before the file is saved.
	a, b, tunnel := twoMachines(t)
	a.write(t, "quizzes", "diff.yaml", "from a")
	b.write(t, "quizzes", "diff.yaml", "b")
	request := sendFiles(t, a, b, files(file("quizzes", "diff.yaml")))
	tunnel.setBeforeDownload(func() { b.setFilesInUse(b.path("quizzes", "diff.yaml")) })

	result, err := b.manager.Accept(t.Context(), request.ID, []any{map[string]any{"type": "quizzes", "name": "diff.yaml", "action": "replace"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 1 || !strings.Contains(result.Failed[0].Error, "used by the running server") {
		t.Errorf("failed = %+v", result.Failed)
	}
	if got := b.read(t, "quizzes", "diff.yaml"); got != "b" {
		t.Errorf("diff.yaml = %q", got)
	}
}

const quizWithMedia = `# Geography quiz
title: Geography
questions:
- question: Where is Kyiv?
  image: "/imgs/map.png"  # main map
  options: ["/imgs/a.png", "/imgs/b.png", "Lviv"]
  correct_answer: 0
- question: Open the table
  file: data.xlsx
  options: ["1", "2"]
  correct_answer: 1
- question: Read the notes
  file: /attach/notes.pdf
  image: /imgs/missing.png
  options: ["yes", "no"]
  correct_answer: 0
`

func writeQuizWithMedia(t *testing.T, m *machine) {
	t.Helper()
	m.write(t, "quizzes", "geo.yaml", quizWithMedia)
	for _, name := range []string{"map.png", "a.png", "b.png"} {
		m.write(t, "images", name, "image "+name)
	}
	m.write(t, "attachments", "data.xlsx", "table")
	m.write(t, "attachments", "notes.pdf", "notes")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// py: tests/test_transfer.py::test_quiz_media_files
func TestQuizMediaFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geo.yaml")
	writeFile(t, path, quizWithMedia)
	want := []mediaFile{
		{"images", "map.png"}, {"images", "a.png"}, {"images", "b.png"},
		{"attachments", "data.xlsx"}, {"images", "missing.png"}, {"attachments", "notes.pdf"},
	}
	if got := quizMediaFiles(path); !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}

	writeFile(t, path, "questions:\n- image: /imgs/sub/x.png\n  file: ../secret.txt\n  options: [/imgs/ok.png]\n")
	if got := quizMediaFiles(path); !slices.Equal(got, []mediaFile{{"images", "ok.png"}}) {
		t.Errorf("got %v", got)
	}

	writeFile(t, path, "questions: [unclosed")
	if got := quizMediaFiles(path); len(got) != 0 {
		t.Errorf("broken YAML: got %v", got)
	}
	if got := quizMediaFiles(filepath.Join(t.TempDir(), "missing.yaml")); len(got) != 0 {
		t.Errorf("missing file: got %v", got)
	}
}

// py: tests/test_transfer.py::test_rewrite_quiz_media_keeps_comments
func TestRewriteQuizMediaKeepsComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geo.yaml")
	writeFile(t, path, quizWithMedia)

	changed, err := rewriteQuizMedia(path, map[mediaFile]string{
		{"images", "map.png"}: "room-a_map.png", {"attachments", "notes.pdf"}: "room-a_notes.pdf",
	})
	if err != nil {
		t.Fatal(err)
	}

	text := readFile(t, path)
	if !changed {
		t.Error("not changed")
	}
	for _, part := range []string{`"/imgs/room-a_map.png"`, "# main map", "/attach/room-a_notes.pdf", "# Geography quiz", `"/imgs/a.png"`, "file: data.xlsx"} {
		if !strings.Contains(text, part) {
			t.Errorf("%q is missing in:\n%s", part, text)
		}
	}
	changed, err = rewriteQuizMedia(path, map[mediaFile]string{{"images", "other.png"}: "x.png"})
	if err != nil || changed {
		t.Errorf("unrelated rename: changed=%v err=%v", changed, err)
	}
}

const quizWithExplanationImages = `# Explanations quiz
questions:
- question: "Q1?"
  options: ["a", "b"]
  correct_answer: 0
  image: /imgs/q.png
  explanation: |
    See ![map](/imgs/map.png) and ![room](</imgs/my room.png>).
    Outside: ![x](https://example.com/x.png)  # not a quiz image
`

// py: tests/test_transfer.py::test_quiz_media_files_finds_explanation_images
func TestQuizMediaFilesFindsExplanationImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.yaml")
	writeFile(t, path, quizWithExplanationImages)
	want := []mediaFile{{"images", "q.png"}, {"images", "map.png"}, {"images", "my room.png"}}
	if got := quizMediaFiles(path); !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

// py: tests/test_transfer.py::test_rewrite_quiz_media_rewrites_explanation_images
func TestRewriteQuizMediaRewritesExplanationImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.yaml")
	writeFile(t, path, quizWithExplanationImages)

	changed, err := rewriteQuizMedia(path, map[mediaFile]string{
		{"images", "map.png"}: "room-a_map.png", {"images", "my room.png"}: "room-a_my room.png",
	})
	if err != nil {
		t.Fatal(err)
	}

	text := readFile(t, path)
	if !changed {
		t.Error("not changed")
	}
	for _, part := range []string{
		"See ![map](/imgs/room-a_map.png) and ![room](</imgs/room-a_my room.png>).",
		"![x](https://example.com/x.png)", "explanation: |", "# Explanations quiz", "image: /imgs/q.png",
	} {
		if !strings.Contains(text, part) {
			t.Errorf("%q is missing in:\n%s", part, text)
		}
	}
}

// py: tests/test_transfer.py::test_quiz_is_sent_with_its_images_and_attachments
func TestQuizIsSentWithItsImagesAndAttachments(t *testing.T) {
	a, b, _ := twoMachines(t)
	writeQuizWithMedia(t, a)

	request := sendFiles(t, a, b, files(file("quizzes", "geo.yaml")))

	// missing.png does not exist on the sender, so it is not offered
	var offered [][2]string
	for _, f := range request.Files {
		offered = append(offered, [2]string{f.Type, f.Name})
	}
	want := [][2]string{
		{"quizzes", "geo.yaml"}, {"images", "map.png"}, {"images", "a.png"}, {"images", "b.png"},
		{"attachments", "data.xlsx"}, {"attachments", "notes.pdf"},
	}
	if !slices.Equal(offered, want) {
		t.Errorf("offered %v", offered)
	}

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 0 {
		t.Errorf("failed = %+v", result.Failed)
	}
	if got := b.read(t, "quizzes", "geo.yaml"); got != quizWithMedia {
		t.Errorf("the quiz changed (no file was renamed):\n%s", got)
	}
	if got := b.files(t, "images"); !slices.Equal(got, []string{"a.png", "b.png", "map.png"}) {
		t.Errorf("images = %v", got)
	}
	if got := b.files(t, "attachments"); !slices.Equal(got, []string{"data.xlsx", "notes.pdf"}) {
		t.Errorf("attachments = %v", got)
	}
	if got := b.read(t, "images", "map.png"); got != "image map.png" {
		t.Errorf("map.png = %q", got)
	}
}

// py: tests/test_transfer.py::test_identical_images_are_skipped_by_default
func TestIdenticalImagesAreSkippedByDefault(t *testing.T) {
	a, b, _ := twoMachines(t)
	writeQuizWithMedia(t, a)
	b.write(t, "images", "map.png", "image map.png")

	request := sendFiles(t, a, b, files(file("quizzes", "geo.yaml")))
	if got := planOf(t, request, "map.png").Case; got != "same" {
		t.Errorf("map.png case = %q", got)
	}
	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Skipped) != 1 || result.Skipped[0].Name != "map.png" {
		t.Errorf("skipped = %+v", result.Skipped)
	}
	var savedAs []string
	for _, f := range result.Saved {
		savedAs = append(savedAs, f.SavedAs)
	}
	slices.Sort(savedAs)
	if !slices.Equal(savedAs, []string{"a.png", "b.png", "data.xlsx", "geo.yaml", "notes.pdf"}) {
		t.Errorf("saved as %v", savedAs)
	}
	if got := b.files(t, "images"); !slices.Equal(got, []string{"a.png", "b.png", "map.png"}) {
		t.Errorf("images = %v", got)
	}
	if got := b.read(t, "quizzes", "geo.yaml"); got != quizWithMedia {
		t.Errorf("the quiz changed:\n%s", got)
	}
}

// py: tests/test_transfer.py::test_renamed_image_by_choice_updates_quiz
func TestRenamedImageByChoiceUpdatesQuiz(t *testing.T) {
	a, b, _ := twoMachines(t)
	writeQuizWithMedia(t, a)
	request := sendFiles(t, a, b, files(file("quizzes", "geo.yaml")))

	if _, err := b.manager.Accept(t.Context(), request.ID, []any{
		map[string]any{"type": "images", "name": "a.png", "action": "rename", "new_name": "circle.png"},
	}); err != nil {
		t.Fatal(err)
	}

	quiz := b.read(t, "quizzes", "geo.yaml")
	if !strings.Contains(quiz, `"/imgs/circle.png"`) || !strings.Contains(quiz, `"/imgs/b.png"`) {
		t.Errorf("quiz:\n%s", quiz)
	}
	if got := b.files(t, "images"); !slices.Equal(got, []string{"b.png", "circle.png", "map.png"}) {
		t.Errorf("images = %v", got)
	}
}

// py: tests/test_transfer.py::test_skipped_image_keeps_quiz_reference
func TestSkippedImageKeepsQuizReference(t *testing.T) {
	a, b, _ := twoMachines(t)
	writeQuizWithMedia(t, a)
	b.write(t, "images", "map.png", "receiver map")
	request := sendFiles(t, a, b, files(file("quizzes", "geo.yaml")))

	if _, err := b.manager.Accept(t.Context(), request.ID, []any{
		map[string]any{"type": "images", "name": "map.png", "action": "skip"},
	}); err != nil {
		t.Fatal(err)
	}

	if quiz := b.read(t, "quizzes", "geo.yaml"); !strings.Contains(quiz, `"/imgs/map.png"`) {
		t.Errorf("quiz:\n%s", quiz)
	}
	if got := b.read(t, "images", "map.png"); got != "receiver map" {
		t.Errorf("map.png = %q", got)
	}
}

// py: tests/test_transfer.py::test_different_images_get_new_names_and_quiz_points_to_them
func TestDifferentImagesGetNewNamesAndQuizPointsToThem(t *testing.T) {
	a, b, _ := twoMachines(t)
	writeQuizWithMedia(t, a)
	b.write(t, "images", "map.png", "another map")
	b.write(t, "attachments", "notes.pdf", "other notes")
	b.write(t, "quizzes", "geo.yaml", "title: Old geography\n")

	request := sendFiles(t, a, b, files(file("quizzes", "geo.yaml")))
	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	saved := map[string]string{}
	for _, f := range result.Saved {
		saved[f.Name] = f.SavedAs
	}
	want := map[string]string{
		"geo.yaml": "room-a_geo.yaml", "map.png": "room-a_map.png", "a.png": "a.png", "b.png": "b.png",
		"data.xlsx": "data.xlsx", "notes.pdf": "room-a_notes.pdf",
	}
	if !mapsEqual(saved, want) {
		t.Errorf("saved = %v", saved)
	}
	// Files of the receiver are untouched
	for _, c := range [][3]string{{"images", "map.png", "another map"}, {"attachments", "notes.pdf", "other notes"}, {"quizzes", "geo.yaml", "title: Old geography\n"}} {
		if got := b.read(t, c[0], c[1]); got != c[2] {
			t.Errorf("%s = %q", c[1], got)
		}
	}
	// The received quiz points to the received files
	quiz := b.read(t, "quizzes", "room-a_geo.yaml")
	for _, part := range []string{`"/imgs/room-a_map.png"`, "file: /attach/room-a_notes.pdf", `"/imgs/a.png"`} {
		if !strings.Contains(quiz, part) {
			t.Errorf("%q is missing in:\n%s", part, quiz)
		}
	}
	if got := b.read(t, "images", "room-a_map.png"); got != "image map.png" {
		t.Errorf("room-a_map.png = %q", got)
	}
}

// py: tests/test_transfer.py::test_media_limit_counts_images
func TestMediaLimitCountsImages(t *testing.T) {
	a, _, _ := twoMachines(t)
	writeQuizWithMedia(t, a)
	a.manager.maxFilesPerOffer = 3 // Python changed MAX_FILES_PER_OFFER
	_, err := a.manager.Send(t.Context(), "room-b", files(file("quizzes", "geo.yaml")))
	if te := transferErr(t, err); !strings.Contains(te.Message, "images and attachments") {
		t.Errorf("message = %q", te.Message)
	}
}

// py: tests/test_transfer.py::test_reject_tells_sender_and_saves_nothing
func TestRejectTellsSenderAndSavesNothing(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	request := sendFiles(t, a, b, files(file("quizzes", "math.yaml")))

	if err := b.manager.Reject(t.Context(), request.ID); err != nil {
		t.Fatal(err)
	}

	if got := b.files(t, "quizzes"); len(got) != 0 {
		t.Errorf("quizzes = %v", got)
	}
	if got := listIncoming(t, b); len(got) != 0 {
		t.Errorf("incoming = %+v", got)
	}
	if got := state(t, a).Outgoing[0].Status; got != "rejected" {
		t.Errorf("offer status = %q", got)
	}
	_, err := b.manager.Accept(t.Context(), request.ID, nil)
	if te := transferErr(t, err); te.Status != 404 {
		t.Errorf("accept after reject: %d %q", te.Status, te.Message)
	}
}

// py: tests/test_transfer.py::test_sender_offline_after_offer
func TestSenderOfflineAfterOffer(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	request := sendFiles(t, a, b, files(file("quizzes", "math.yaml")))

	a.setOnline(false)
	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Saved) != 0 {
		t.Errorf("saved = %+v", result.Saved)
	}
	if len(result.Failed) != 1 || !strings.Contains(result.Failed[0].Error, "not online") {
		t.Errorf("failed = %+v", result.Failed)
	}
	if got := b.files(t, "quizzes"); len(got) != 0 {
		t.Errorf("quizzes = %v (no temporary files left)", got)
	}
}

// py: tests/test_transfer.py::test_fake_offer_cannot_deliver_files
func TestFakeOfferCannotDeliverFiles(t *testing.T) {
	// A stranger can post an offer, but B downloads only from the named
	// server with its token.
	a, b, tunnel := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	body := `{"from": "room-a", "token": "` + strings.Repeat("f", 43) + `", "files": [{"type": "quizzes", "name": "math.yaml"}]}`
	response, err := http.Post(tunnel.baseURL+"/room-b/api/transfer/offer", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("offer: %d", response.StatusCode)
	}

	result, err := b.manager.Accept(t.Context(), listIncoming(t, b)[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Saved) != 0 {
		t.Errorf("saved = %+v", result.Saved)
	}
	if len(result.Failed) != 1 || result.Failed[0].Error != "The sender does not offer this file anymore" {
		t.Errorf("failed = %+v", result.Failed)
	}
	if got := b.files(t, "quizzes"); len(got) != 0 {
		t.Errorf("quizzes = %v", got)
	}
}

// py: tests/test_transfer.py::test_download_only_offered_files_with_token
func TestDownloadOnlyOfferedFilesWithToken(t *testing.T) {
	a, b, tunnel := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	a.write(t, "quizzes", "secret.yaml", "y")
	sendFiles(t, a, b, files(file("quizzes", "math.yaml")))
	a.manager.mu.Lock()
	token := a.manager.outgoing[0].token
	a.manager.mu.Unlock()
	url := tunnel.baseURL + "/room-a/api/transfer/download"

	for path, want := range map[string]int{
		"/" + token + "/quizzes/math.yaml":                   200,
		"/" + token + "/quizzes/secret.yaml":                 404,
		"/" + strings.Repeat("x", 43) + "/quizzes/math.yaml": 404,
		"/" + token + "/logs/math.yaml":                      404,
	} {
		response, err := http.Get(url + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, response.StatusCode, want)
		}
	}
}

// py: tests/test_transfer.py::test_expired_offer
func TestExpiredOffer(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	request := sendFiles(t, a, b, files(file("quizzes", "math.yaml")))
	a.manager.mu.Lock()
	a.manager.outgoing[0].created = a.manager.outgoing[0].created.Add(-(offerLifetime + time.Second))
	a.manager.mu.Unlock()

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Saved) != 0 {
		t.Errorf("saved = %+v", result.Saved)
	}
	if got := state(t, a).Outgoing[0].Status; got != "expired" {
		t.Errorf("offer status = %q", got)
	}
}

// py: tests/test_transfer.py::test_old_incoming_requests_are_removed
func TestOldIncomingRequestsAreRemoved(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	sendFiles(t, a, b, files(file("quizzes", "math.yaml")))
	b.manager.mu.Lock()
	b.manager.incoming[0].received = time.Now().Add(-offerLifetime - time.Second)
	b.manager.mu.Unlock()

	if got := listIncoming(t, b); len(got) != 0 {
		t.Errorf("incoming = %+v", got)
	}
}

// py: tests/test_transfer.py::test_file_size_limit
func TestFileSizeLimit(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "logs", "0001.log", strings.Repeat("x", 100))
	request := sendFiles(t, a, b, files(file("logs", "0001.log")))
	b.manager.maxFileSize = 10 // Python changed MAX_FILE_SIZE

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Saved) != 0 {
		t.Errorf("saved = %+v", result.Saved)
	}
	if len(result.Failed) != 1 || !strings.Contains(result.Failed[0].Error, "larger") {
		t.Errorf("failed = %+v", result.Failed)
	}
	if got := b.files(t, "logs"); len(got) != 0 {
		t.Errorf("logs = %v", got)
	}
}

// ----- Sender errors -----

// py: tests/test_transfer.py::test_send_requires_tunnel
func TestSendRequiresTunnel(t *testing.T) {
	a, _, _ := twoMachines(t)
	a.setOnline(false)
	_, err := a.manager.Send(t.Context(), "room-b", files(file("quizzes", "math.yaml")))
	if te := transferErr(t, err); te.Status != 409 {
		t.Errorf("status %d", te.Status)
	}
}

// py: tests/test_transfer.py::test_send_to_unknown_server
func TestSendToUnknownServer(t *testing.T) {
	a, _, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	_, err := a.manager.Send(t.Context(), "room-x", files(file("quizzes", "math.yaml")))
	if te := transferErr(t, err); !strings.Contains(te.Message, "not online") {
		t.Errorf("message %q", te.Message)
	}
	a.manager.mu.Lock()
	defer a.manager.mu.Unlock()
	if len(a.manager.outgoing) != 0 {
		t.Errorf("outgoing = %+v", a.manager.outgoing)
	}
}

func TestSendValidation(t *testing.T) {
	math := files(file("quizzes", "math.yaml"))
	cases := []struct {
		py      string
		target  string
		files   any
		message string
	}{
		{py: "tests/test_transfer.py::test_send_validation[../x-files0-Invalid server name]", target: "../x", files: math, message: "Invalid server name"},
		{py: "tests/test_transfer.py::test_send_validation[room-a-files1-name of this server]", target: "room-a", files: math, message: "name of this server"},
		{py: "tests/test_transfer.py::test_send_validation[room-b-files2-No files selected]", target: "room-b", files: []any{}, message: "No files selected"},
		{py: "tests/test_transfer.py::test_send_validation[room-b-math.yaml-No files selected]", target: "room-b", files: "math.yaml", message: "No files selected"},
		{py: "tests/test_transfer.py::test_send_validation[room-b-files4-cannot be transferred]", target: "room-b", files: files(file("static", "math.yaml")), message: "cannot be transferred"},
		{py: "tests/test_transfer.py::test_send_validation[room-b-files5-cannot be transferred]", target: "room-b", files: files(file("quizzes", "../math.yaml")), message: "cannot be transferred"},
		{py: "tests/test_transfer.py::test_send_validation[room-b-files6-File not found]", target: "room-b", files: files(file("quizzes", "missing.yaml")), message: "File not found"},
	}
	for _, c := range cases {
		t.Run(c.message, func(t *testing.T) {
			a, b, _ := twoMachines(t)
			a.write(t, "quizzes", "math.yaml", "x")
			_, err := a.manager.Send(t.Context(), c.target, c.files)
			if te := transferErr(t, err); !strings.Contains(te.Message, c.message) {
				t.Errorf("message %q, want %q", te.Message, c.message)
			}
			if got := listIncoming(t, b); len(got) != 0 {
				t.Errorf("incoming = %+v", got)
			}
		})
	}
}

// py: tests/test_transfer.py::test_send_skips_duplicate_files
func TestSendSkipsDuplicateFiles(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	f := file("quizzes", "math.yaml")
	request := sendFiles(t, a, b, files(f, f))
	if len(request.Files) != 1 {
		t.Errorf("files = %+v", request.Files)
	}
}

// ----- Receiver validation -----

func TestIncomingValidation(t *testing.T) {
	token := strings.Repeat("t", 43)
	aLog := []any{map[string]any{"type": "logs", "name": "a.log"}}
	cases := []struct {
		py      string
		data    any
		message string
	}{
		{py: "tests/test_transfer.py::test_incoming_validation[not a dict-Invalid request]", data: "not a dict", message: "Invalid request"},
		{py: "tests/test_transfer.py::test_incoming_validation[data1-Invalid sender]",
			data: map[string]any{"from": "../x", "token": token, "files": aLog}, message: "Invalid sender"},
		{py: "tests/test_transfer.py::test_incoming_validation[data2-Invalid sender]",
			data: map[string]any{"from": "room-b", "token": token, "files": aLog}, message: "Invalid sender"},
		{py: "tests/test_transfer.py::test_incoming_validation[data3-Invalid token]",
			data: map[string]any{"from": "room-a", "token": "short", "files": aLog}, message: "Invalid token"},
		{py: "tests/test_transfer.py::test_incoming_validation[data4-cannot be]",
			data: map[string]any{"from": "room-a", "token": token, "files": []any{map[string]any{"type": "logs", "name": "../a.log"}}}, message: "cannot be"},
		{py: "tests/test_transfer.py::test_incoming_validation[data5-No files]",
			data: map[string]any{"from": "room-a", "token": token, "files": []any{}}, message: "No files"},
	}
	for _, c := range cases {
		t.Run(c.message, func(t *testing.T) {
			_, b, _ := twoMachines(t)
			_, err := b.manager.addIncoming(c.data)
			if te := transferErr(t, err); !strings.Contains(te.Message, c.message) {
				t.Errorf("message %q, want %q", te.Message, c.message)
			}
		})
	}
}

// py: tests/test_transfer.py::test_incoming_ignores_invalid_checksum
func TestIncomingIgnoresInvalidChecksum(t *testing.T) {
	_, b, _ := twoMachines(t)
	request, err := b.manager.addIncoming(map[string]any{
		"from": "room-a", "token": strings.Repeat("t", 43),
		"files": []any{map[string]any{"type": "logs", "name": "a.log", "size": json.Number("1"), "sha256": "not-a-checksum"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.files[0].sha256 != "" {
		t.Errorf("sha256 = %q, want none", request.files[0].sha256)
	}
}

// py: tests/test_transfer.py::test_incoming_requires_tunnel
func TestIncomingRequiresTunnel(t *testing.T) {
	_, b, _ := twoMachines(t)
	b.setOnline(false)
	_, err := b.manager.addIncoming(map[string]any{"from": "room-a", "token": strings.Repeat("t", 43), "files": []any{map[string]any{"type": "logs", "name": "a.log"}}})
	if te := transferErr(t, err); te.Status != 409 {
		t.Errorf("status %d", te.Status)
	}
}

// py: tests/test_transfer.py::test_incoming_request_limit
func TestIncomingRequestLimit(t *testing.T) {
	_, b, _ := twoMachines(t)
	data := map[string]any{"from": "room-a", "token": strings.Repeat("t", 43), "files": []any{map[string]any{"type": "logs", "name": "a.log", "size": true}}}
	var request *request
	for range maxIncomingRequests {
		var err error
		if request, err = b.manager.addIncoming(data); err != nil {
			t.Fatal(err)
		}
	}
	if got := request.files[0].size.String(); got != "0" {
		t.Errorf("size = %s (an invalid size is ignored)", got)
	}
	_, err := b.manager.addIncoming(data)
	if te := transferErr(t, err); te.Status != 429 {
		t.Errorf("status %d", te.Status)
	}
}

// py: tests/test_transfer.py::test_result_endpoint_validation
func TestResultEndpointValidation(t *testing.T) {
	_, _, tunnel := twoMachines(t)
	url := tunnel.baseURL + "/room-a/api/transfer/result"
	token := strings.Repeat("x", 43)
	for body, want := range map[string]int{
		"not json": 400,
		`{"token": "` + token + `", "status": "hacked"}`:   400,
		`{"token": "` + token + `", "status": "accepted"}`: 404,
	} {
		response, err := http.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Errorf("%s: %d, want %d", body, response.StatusCode, want)
		}
	}
}

// py: tests/test_transfer.py::test_state
func TestState(t *testing.T) {
	a, b, _ := twoMachines(t)
	a.write(t, "quizzes", "math.yaml", "x")
	sendFiles(t, a, b, files(file("quizzes", "math.yaml")))

	s := state(t, a)
	if !s.Available || s.Name == nil || *s.Name != "room-a" {
		t.Errorf("available %v, name %v", s.Available, s.Name)
	}
	if s.Outgoing[0].To != "room-b" || s.Outgoing[0].Status != "waiting" {
		t.Errorf("outgoing = %+v", s.Outgoing[0])
	}
	if _, ok := asJSON(t, s.Outgoing[0]).(map[string]any)["token"]; ok {
		t.Error("the offer view shows the token")
	}

	b.setOnline(false)
	s = state(t, b)
	if s.Available || s.Name != nil {
		t.Errorf("offline: available %v, name %v", s.Available, s.Name)
	}
}
