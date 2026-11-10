package server_test

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

const privateQuiz = `title: Private Quiz
description: A quiz from a private repository
questions:
  - question: What is 2 + 2?
    options: ["3", "4", "5", "6"]
    correct_answer: 1
`

// zipBytes builds a ZIP archive in memory from {path: content}.
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// archiveServer serves one ZIP archive over HTTP, optionally only to a
// request with a bearer token, and records every Authorization header (""
// when there was none). The tests never go to the internet.
type archiveServer struct {
	*httptest.Server
	mu      sync.Mutex
	headers []string
}

func newArchiveServer(t *testing.T, archive []byte, requiredToken string) *archiveServer {
	t.Helper()
	a := &archiveServer{}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		a.mu.Lock()
		a.headers = append(a.headers, auth)
		a.mu.Unlock()
		if requiredToken != "" && auth != "Bearer "+requiredToken {
			http.Error(w, "Not Found", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	t.Cleanup(a.Close)
	return a
}

func (a *archiveServer) url() string { return a.Server.URL + "/archive.zip" }

func (a *archiveServer) authHeaders() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.headers...)
}

func downloadQuiz(t *testing.T, ts *testutil.TestServer, name, path, folder string) *testutil.Response {
	t.Helper()
	return ts.AdminClient(t, "test123").PostJSON(t, "/api/admin/download-quiz", map[string]any{"name": name, "download_path": path, "folder": folder})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// py: tests/test_admin_download_quiz.py::test_download_quiz_path_validation_valid_subfolder
func TestDownloadQuizValidSubfolder(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	archive := newArchiveServer(t, zipBytes(t, map[string]string{"webquiz-quizzes-master/data/test_quiz.yaml": privateQuiz}), "")
	res := downloadQuiz(t, ts, "Test Quiz", archive.url(), "webquiz-quizzes-master/data/")
	body := res.JSON(t)
	if res.Status != 200 || body["success"] != true || !strings.Contains(body["message"].(string), "Test Quiz") {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if !fileExists(filepath.Join(ts.QuizzesDir, "test_quiz.yaml")) {
		t.Error("the quiz file was not copied")
	}
}

// py: tests/test_admin_download_quiz.py::test_download_quiz_path_validation_parent_directory_blocked
func TestDownloadQuizParentDirectoryBlocked(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	for _, folder := range []string{"../etc/passwd", "foo/../../etc/passwd", "foo/../../../etc/passwd"} {
		res := downloadQuiz(t, ts, "Malicious Quiz", "http://example.com/quiz.zip", folder)
		text, _ := res.JSON(t)["error"].(string)
		if res.Status != 400 || (!strings.Contains(strings.ToLower(text), "parent directory") && !strings.Contains(strings.ToLower(text), "traversal")) {
			t.Errorf("%q: %d %s", folder, res.Status, res.Text())
		}
	}
}

// py: tests/test_admin_download_quiz.py::test_download_quiz_path_validation_absolute_path_blocked
func TestDownloadQuizAbsolutePathBlocked(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	for _, folder := range []string{"/etc/passwd", "/tmp/malicious", `C:\Windows\System32`} {
		res := downloadQuiz(t, ts, "Malicious Quiz", "http://example.com/quiz.zip", folder)
		text, _ := res.JSON(t)["error"].(string)
		if res.Status != 400 || (!strings.Contains(strings.ToLower(text), "absolute") && !strings.Contains(strings.ToLower(text), "not allowed")) {
			t.Errorf("%q: %d %s", folder, res.Status, res.Text())
		}
	}
}

// py: tests/test_admin_download_quiz.py::test_download_quiz_empty_folder_path
func TestDownloadQuizEmptyFolder(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	archive := newArchiveServer(t, zipBytes(t, map[string]string{"root_quiz.yaml": privateQuiz}), "")
	res := downloadQuiz(t, ts, "Root Quiz", archive.url(), "")
	if _, ok := res.JSON(t)["message"]; !ok || res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if !fileExists(filepath.Join(ts.QuizzesDir, "root_quiz.yaml")) {
		t.Error("the quiz file was not copied to the quizzes folder")
	}
}

// py: tests/test_admin_download_quiz.py::test_download_quiz_missing_parameters
func TestDownloadQuizMissingParameters(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	for _, body := range []map[string]any{
		{"download_path": "http://example.com/quiz.zip"},
		{"name": "Test Quiz"},
	} {
		res := admin.PostJSON(t, "/api/admin/download-quiz", body)
		if _, ok := res.JSON(t)["error"]; !ok || res.Status != 400 {
			t.Errorf("%v: %d %s", body, res.Status, res.Text())
		}
	}
}

// Errors of the download itself (the old tests had no case for them).
func TestDownloadQuizFailures(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	missing := newArchiveServer(t, nil, "no-such-token") // answers 401 without the token
	notZip := newArchiveServer(t, []byte("this is not a zip file"), "")
	small := newArchiveServer(t, zipBytes(t, map[string]string{"a/b.yaml": privateQuiz, "a/c/d.yaml": privateQuiz}), "")
	cases := []struct {
		name, url, folder string
		status            int
		contains          string
	}{
		{"http error", missing.url(), "", 500, "Failed to download quiz: HTTP 401"},
		{"not a zip", notZip.url(), "", 500, "Downloaded file is not a valid ZIP archive"},
		{"unknown folder", small.url(), "nothing", 400, "Folder 'nothing' not found in ZIP archive. Available paths: ['a']"},
		{"nothing listens", "http://127.0.0.1:1/x.zip", "", 500, "Network error"},
	}
	for _, c := range cases {
		res := downloadQuiz(t, ts, "Q", c.url, c.folder)
		text, _ := res.JSON(t)["error"].(string)
		if res.Status != c.status || !strings.Contains(text, c.contains) {
			t.Errorf("%s: %d %s", c.name, res.Status, res.Text())
		}
	}
}

// The folder may be named inside the single folder of a nested archive.
func TestDownloadQuizKeepsSubfolders(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	archive := newArchiveServer(t, zipBytes(t, map[string]string{"top/../../evil.yaml": privateQuiz, "a/c/d.yaml": privateQuiz}), "")
	if res := downloadQuiz(t, ts, "Q", archive.url(), "a"); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if !fileExists(filepath.Join(ts.QuizzesDir, "c", "d.yaml")) {
		t.Error("subfolder not copied")
	}
	// A name with .. cannot write outside the extraction folder.
	if fileExists(filepath.Join(ts.Dir, "evil.yaml")) || fileExists(filepath.Join(filepath.Dir(ts.Dir), "evil.yaml")) {
		t.Error("an archive entry escaped the extraction folder")
	}
}

// Tests of the token (test_download_quiz_token.py).

// py: tests/test_download_quiz_token.py::test_download_quiz_sends_token_from_config
func TestDownloadQuizSendsTokenFromConfig(t *testing.T) {
	t.Parallel()
	archive := newArchiveServer(t, zipBytes(t, map[string]string{"owner-repo-abc123/data/private_quiz.yaml": privateQuiz}), "secret-token-123")
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"quizzes": []any{map[string]any{
		"name": "Private Quiz", "download_path": archive.url(), "folder": "owner-repo-abc123/data", "auth_header_token": "secret-token-123",
	}}}})
	res := downloadQuiz(t, ts, "Private Quiz", archive.url(), "owner-repo-abc123/data")
	if res.Status != 200 || res.JSON(t)["success"] != true {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if headers := archive.authHeaders(); len(headers) != 1 || headers[0] != "Bearer secret-token-123" {
		t.Errorf("Authorization headers %q", headers)
	}
	if !fileExists(filepath.Join(ts.QuizzesDir, "private_quiz.yaml")) {
		t.Error("the quiz file was not copied")
	}
}

// py: tests/test_download_quiz_token.py::test_download_quiz_without_token_sends_no_authorization_header
func TestDownloadQuizWithoutTokenSendsNoHeader(t *testing.T) {
	t.Parallel()
	archive := newArchiveServer(t, zipBytes(t, map[string]string{"data/public_quiz.yaml": privateQuiz}), "")
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"quizzes": []any{map[string]any{
		"name": "Public Quiz", "download_path": archive.url(), "folder": "data",
	}}}})
	res := downloadQuiz(t, ts, "Public Quiz", archive.url(), "data")
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if headers := archive.authHeaders(); len(headers) != 1 || headers[0] != "" {
		t.Errorf("Authorization headers %q", headers)
	}
}

// py: tests/test_download_quiz_token.py::test_download_quiz_token_uses_configured_url_not_the_request_one
func TestDownloadQuizTokenUsesConfiguredURL(t *testing.T) {
	t.Parallel()
	entries := zipBytes(t, map[string]string{"data/private_quiz.yaml": privateQuiz})
	configured := newArchiveServer(t, entries, "secret-token-123")
	other := newArchiveServer(t, entries, "")
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"quizzes": []any{map[string]any{
		"name": "Private Quiz", "download_path": configured.url(), "folder": "data", "auth_header_token": "secret-token-123",
	}}}})
	res := downloadQuiz(t, ts, "Private Quiz", other.url(), "data")
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if headers := configured.authHeaders(); len(headers) != 1 || headers[0] != "Bearer secret-token-123" {
		t.Errorf("configured server saw %q", headers)
	}
	if headers := other.authHeaders(); len(headers) != 0 {
		t.Errorf("the other server saw %q", headers)
	}
}

// py: tests/test_download_quiz_token.py::test_download_quiz_token_refused_over_plain_http
func TestDownloadQuizTokenRefusedOverPlainHTTP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"quizzes": []any{map[string]any{
		"name": "Private Quiz", "download_path": "http://example.com/archive.zip", "folder": "data", "auth_header_token": "secret-token-123",
	}}}})
	res := downloadQuiz(t, ts, "Private Quiz", "http://example.com/archive.zip", "data")
	text, _ := res.JSON(t)["error"].(string)
	if res.Status != 400 || !strings.Contains(strings.ToLower(text), "https") {
		t.Errorf("%d %s", res.Status, res.Text())
	}
}

// py: tests/test_download_quiz_token.py::test_download_quiz_folder_resolved_inside_archive_root
func TestDownloadQuizFolderInsideArchiveRoot(t *testing.T) {
	t.Parallel()
	archive := newArchiveServer(t, zipBytes(t, map[string]string{"owner-repo-9f8e7d6/data/private_quiz.yaml": privateQuiz}), "")
	ts := testutil.StartServer(t, testutil.Options{})
	res := downloadQuiz(t, ts, "Private Quiz", archive.url(), "data")
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if !fileExists(filepath.Join(ts.QuizzesDir, "private_quiz.yaml")) {
		t.Error("the quiz file was not copied")
	}
}

// py: tests/test_download_quiz_token.py::test_admin_page_does_not_expose_the_token
func TestAdminPageDoesNotExposeTheToken(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"quizzes": []any{map[string]any{
		"name": "Private Quiz", "download_path": "https://example.com/archive.zip", "folder": "data", "auth_header_token": "secret-token-123",
	}}}})
	page := ts.AdminClient(t, "test123").Do(t, "GET", "/admin/", nil)
	if page.Status != 200 || !strings.Contains(page.Text(), "Private Quiz") || strings.Contains(page.Text(), "secret-token-123") {
		t.Errorf("status %d, has name %v, has token %v", page.Status, strings.Contains(page.Text(), "Private Quiz"), strings.Contains(page.Text(), "secret-token-123"))
	}
}

// A token must not follow a redirect to another origin (httpx and Go both
// drop it there).
func TestDownloadQuizTokenNotSentAfterRedirectToOtherOrigin(t *testing.T) {
	t.Parallel()
	entries := zipBytes(t, map[string]string{"data/q.yaml": privateQuiz})
	target := newArchiveServer(t, entries, "")
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.url(), http.StatusFound)
	}))
	t.Cleanup(redirector.Close)
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"quizzes": []any{map[string]any{
		"name": "Private Quiz", "download_path": redirector.URL + "/archive.zip", "folder": "data", "auth_header_token": "secret-token-123",
	}}}})
	if res := downloadQuiz(t, ts, "Private Quiz", redirector.URL+"/archive.zip", "data"); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	if headers := target.authHeaders(); len(headers) != 1 || headers[0] != "" {
		t.Errorf("the redirect target saw %q", headers)
	}
}

// Tests of the file attachments (test_quiz_file_download.py).

// listFilesOrSkip calls GET /api/admin/list-files. That handler belongs to
// the quiz admin API (Task 8); until it is merged it answers 501 and the
// test is skipped.
func listFilesOrSkip(t *testing.T, client *testutil.Client) *testutil.Response {
	t.Helper()
	res := client.GetJSON(t, "/api/admin/list-files")
	if res.Status == http.StatusNotImplemented {
		t.Skip("GET /api/admin/list-files is built in Task 8")
	}
	return res
}

// py: tests/test_quiz_file_download.py::test_admin_list_files_empty_directory
func TestAdminListFilesEmptyDirectory(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := listFilesOrSkip(t, ts.AdminClient(t, "test123"))
	files, ok := res.JSON(t)["files"].([]any)
	if res.Status != 200 || !ok || len(files) != 0 {
		t.Errorf("%d %s", res.Status, res.Text())
	}
}

// py: tests/test_quiz_file_download.py::test_admin_list_files_with_files
func TestAdminListFilesWithFiles(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	dir := filepath.Join(ts.QuizzesDir, "attach")
	for name, content := range map[string]string{
		"data.xlsx": "spreadsheet content here", "code.py": "print('hello')", "report.pdf": strings.Repeat("x", 1000),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := listFilesOrSkip(t, admin)
	files, _ := res.JSON(t)["files"].([]any)
	if res.Status != 200 || len(files) != 3 {
		t.Fatalf("%d %s", res.Status, res.Text())
	}
	var names []string
	for _, item := range files {
		file := item.(map[string]any)
		path, _ := file["path"].(string)
		size, _ := file["size"].(interface{ Int64() (int64, error) })
		length, _ := size.Int64()
		if file["filename"] == nil || !strings.HasPrefix(path, "/attach/") || length <= 0 {
			t.Errorf("file %v", file)
		}
		names = append(names, strings.ToLower(file["filename"].(string)))
	}
	if strings.Join(names, ",") != "code.py,data.xlsx,report.pdf" {
		t.Errorf("order %v", names)
	}
}

// py: tests/test_quiz_file_download.py::test_admin_list_files_requires_auth
func TestAdminListFilesRequiresAuth(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := ts.GetJSON(t, "/api/admin/list-files"); res.Status != 401 {
		t.Errorf("status %d", res.Status)
	}
}

// py: tests/test_quiz_file_download.py::test_quiz_file_download
func TestQuizFileDownload(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	content := "This is test file content"
	if err := os.WriteFile(filepath.Join(ts.QuizzesDir, "attach", "testfile.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ts.Do(t, "GET", "/attach/testfile.txt", nil) // no login needed
	disposition := res.Header.Get("Content-Disposition")
	if res.Status != 200 || res.Text() != content || !strings.Contains(disposition, "attachment") || !strings.Contains(disposition, `filename="testfile.txt"`) {
		t.Errorf("%d %q %q", res.Status, res.Text(), disposition)
	}
}

// py: tests/test_quiz_file_download.py::test_quiz_file_download_not_found
func TestQuizFileDownloadNotFound(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.Do(t, "GET", "/attach/nonexistent.txt", nil)
	if res.Status != 404 || res.JSON(t)["error"] != "File not found" {
		t.Errorf("%d %s", res.Status, res.Text())
	}
}

// py: tests/test_quiz_file_download.py::test_quiz_file_download_path_traversal_blocked
func TestQuizFileDownloadPathTraversalBlocked(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if err := os.WriteFile(filepath.Join(ts.QuizzesDir, "attach", "safe.txt"), []byte("safe content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ts.QuizzesDir, "secret.yaml"), []byte("secret: data"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret.yaml", "..%2Fsecret.yaml", "subfolder/../../../etc/passwd"} {
		res := ts.Do(t, "GET", "/attach/"+path, nil)
		if (res.Status != 400 && res.Status != 404) || strings.Contains(strings.ToLower(res.Text()), "secret") {
			t.Errorf("%s: %d %s", path, res.Status, res.Text())
		}
	}
	// A folder is not a file.
	if err := os.Mkdir(filepath.Join(ts.QuizzesDir, "attach", "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if res := ts.Do(t, "GET", "/attach/folder", nil); res.Status != 400 || res.JSON(t)["error"] != "Not a file" {
		t.Errorf("folder: %d %s", res.Status, res.Text())
	}
	if res := ts.Do(t, "GET", "/attach/..%2Fsecret.yaml", nil); res.JSON(t)["error"] != "Invalid filename" {
		t.Errorf("encoded slash: %d %s", res.Status, res.Text())
	}
}

// py: tests/test_quiz_file_download.py::test_question_with_file_field_in_index
func TestQuestionWithFileFieldInIndex(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"file_test.yaml": map[string]any{
		"title": "File Download Test",
		"questions": []any{
			map[string]any{"question": "Text only question?", "options": []any{"A", "B"}, "correct_answer": 0},
			map[string]any{"question": "Question with file?", "options": []any{"Yes", "No"}, "correct_answer": 0, "file": "data.xlsx"},
			map[string]any{"question": "Question with image and file?", "options": []any{"Option 1", "Option 2"}, "correct_answer": 1, "image": "/imgs/diagram.png", "file": "analysis.csv"},
			map[string]any{"options": []any{"A", "B", "C"}, "correct_answer": 0, "image": "/imgs/chart.png", "file": "raw_data.json"},
		},
	}}})
	boot := pageBoot(t, readIndex(t, ts))
	questions, _ := boot["questions"].([]any)
	if len(questions) != 4 {
		t.Fatalf("%d questions", len(questions))
	}
	q := func(i int) map[string]any { return questions[i].(map[string]any) }
	if _, ok := q(0)["question"]; !ok {
		t.Error("question 1 has no text")
	}
	if _, ok := q(0)["file"]; ok {
		t.Error("question 1 has a file")
	}
	for i, want := range map[int]string{1: "/attach/data.xlsx", 2: "/attach/analysis.csv", 3: "/attach/raw_data.json"} {
		if q(i)["file"] != want {
			t.Errorf("question %d file %v, want %s", i+1, q(i)["file"], want)
		}
	}
	if _, ok := q(2)["image"]; !ok {
		t.Error("question 3 has no image")
	}
	if _, ok := q(3)["image"]; !ok {
		t.Error("question 4 has no image")
	}
}

// py: tests/test_quiz_file_download.py::test_files_directory_created_on_startup
func TestAttachDirectoryCreatedOnStartup(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	info, err := os.Stat(filepath.Join(ts.QuizzesDir, "attach"))
	if err != nil || !info.IsDir() {
		t.Errorf("attach folder: %v", err)
	}
}
