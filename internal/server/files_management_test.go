package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of tests/test_files_management.py and tests/test_csv_table_viewer.py.
//
// The Python tests also searched the HTML of files.html and csv_table.html
// for element ids, CSS rules and JavaScript functions. The Go server sends a
// small page that loads the React app with its data (the boot JSON); the
// elements are made by the app (Task 16 tests them in the browser). Those
// ports check what the server gives such a page instead: the page itself,
// its boot data, its assets and the API it reads.

// fastFlush writes the answers to the CSV files every 20 ms, so a test does
// not wait 5 s for the flush (Python's tests slept).
var fastFlush = server.Options{FlushInterval: 20 * time.Millisecond}

// filesEntry is a file of GET /api/files/list.
type filesEntry struct {
	Name     string `json:"name"`
	Size     any    `json:"size"`
	Modified any    `json:"modified"`
	Type     any    `json:"type"`
}

type filesListing struct {
	Logs    []filesEntry `json:"logs"`
	CSV     []filesEntry `json:"csv"`
	Quizzes []filesEntry `json:"quizzes"`
}

func listFiles(t *testing.T, admin *testutil.Client) filesListing {
	t.Helper()
	res := admin.GetJSON(t, "/api/files/list")
	if res.Status != http.StatusOK {
		t.Fatalf("list: %d %s", res.Status, res.Body)
	}
	var listing filesListing
	res.Decode(t, &listing)
	if listing.Logs == nil || listing.CSV == nil || listing.Quizzes == nil {
		t.Fatalf("a list is missing or null: %s", res.Body)
	}
	return listing
}

// answerFirstQuestion registers a student and answers question 1.
func answerFirstQuestion(t *testing.T, ts *testutil.TestServer, name string) string {
	t.Helper()
	userID := ts.RegisterStudent(t, name)
	res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": 0})
	if res.Status != http.StatusOK {
		t.Fatalf("answer: %d %s", res.Status, res.Body)
	}
	return userID
}

// waitForCSV waits until the CSV list has a file whose name ends with suffix.
func waitForCSV(t *testing.T, admin *testutil.Client, suffix string) filesEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, f := range listFiles(t, admin).CSV {
			if strings.HasSuffix(f.Name, suffix) && f.Size != json.Number("0") {
				return f
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no CSV file %q was written", suffix)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Authentication & Authorization Tests

// py: tests/test_files_management.py::test_files_page_serves_html
func TestFilesPageServesHTML(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/files/")
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("%d %s", res.Status, res.Header.Get("Content-Type"))
	}
	// The login form (auth-section) and the files panel are parts of the
	// React page; the server gives it what they need.
	boot := pageBoot(t, res.Text())
	for _, key := range []string{"is_trusted_ip", "config_content", "config_path", "config_data"} {
		if _, ok := boot[key]; !ok {
			t.Errorf("boot data has no %q: %v", key, boot)
		}
	}
}

// py: tests/test_files_management.py::test_files_api_requires_master_key
func TestFilesAPIRequiresMasterKey(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	for _, path := range []string{"/api/files/list", "/api/files/logs/view/test.log", "/api/files/logs/download/test.log"} {
		res := ts.GetJSON(t, path)
		if res.Status != 401 || res.JSON(t)["error"] == nil {
			t.Errorf("%s: %d %s", path, res.Status, res.Body)
		}
	}
}

// py: tests/test_files_management.py::test_files_trusted_ip_access
func TestFilesTrustedIPAccess(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{
		"admin": map[string]any{"master_key": "test123", "trusted_ips": []any{"127.0.0.1"}},
	}})
	res := ts.GetJSON(t, "/api/files/list") // no session: the address is trusted
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	if _, ok := data["logs"]; !ok {
		t.Errorf("no logs: %v", data)
	}
	if _, ok := data["csv"]; !ok {
		t.Errorf("no csv: %v", data)
	}
}

// py: tests/test_files_management.py::test_files_invalid_master_key_rejection
func TestFilesInvalidMasterKeyRejection(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/files/list", testutil.WithCookies(&http.Cookie{Name: "admin_session", Value: "invalid_token_12345"}))
	if res.Status != 401 || res.JSON(t)["error"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_files_api_without_master_key_disabled
func TestFilesAPIWithoutMasterKeyDisabled(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"admin": map[string]any{"master_key": nil}}})
	res := ts.GetJSON(t, "/api/files/list")
	if res.Status != 403 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if text, _ := res.JSON(t)["error"].(string); !strings.Contains(strings.ToLower(text), "disabled") {
		t.Errorf("error %q", text)
	}
}

// py: tests/test_files_management.py::test_files_page_renders_with_auth
func TestFilesPageRendersWithAuth(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/files/", testutil.WithCookies(ts.AdminCookies(t, "test123")...))
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("%d %s", res.Status, res.Header.Get("Content-Type"))
	}
	if !strings.Contains(res.Text(), "File Manager") {
		t.Errorf("no \"File Manager\" in the page:\n%s", res.Text())
	}
}

// File Listing Tests

// py: tests/test_files_management.py::test_list_files_empty_directories
func TestListFilesEmptyDirectories(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	listFiles(t, ts.AdminClient(t, "test123")) // checks that logs, csv and quizzes are lists
}

// py: tests/test_files_management.py::test_list_files_with_log_files
func TestListFilesWithLogFiles(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	logs := listFiles(t, ts.AdminClient(t, "test123")).Logs
	if len(logs) == 0 {
		t.Fatal("the server's own log file is not listed")
	}
	for _, f := range logs {
		if f.Name == "" || f.Size == nil || f.Modified == nil || f.Type != "log" {
			t.Errorf("log entry %+v", f)
		}
	}
}

// py: tests/test_files_management.py::test_list_files_with_csv_files
func TestListFilesWithCSVFiles(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: fastFlush})
	answerFirstQuestion(t, ts, "testuser")
	admin := ts.AdminClient(t, "test123")
	waitForCSV(t, admin, "_0001.csv")
	for _, f := range listFiles(t, admin).CSV {
		if f.Type != "csv" {
			t.Errorf("csv entry %+v", f)
		}
	}
}

// py: tests/test_files_management.py::test_list_files_mixed_content
func TestListFilesMixedContent(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: fastFlush})
	answerFirstQuestion(t, ts, "testuser")
	admin := ts.AdminClient(t, "test123")
	waitForCSV(t, admin, ".users.csv")
	listing := listFiles(t, admin)
	if len(listing.Logs) == 0 || len(listing.CSV) == 0 {
		t.Errorf("logs %d, csv %d", len(listing.Logs), len(listing.CSV))
	}
}

// py: tests/test_files_management.py::test_list_files_metadata_accuracy
func TestListFilesMetadataAccuracy(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	isoTime := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d{6})?$`)
	for _, f := range listFiles(t, ts.AdminClient(t, "test123")).Logs {
		size, isNumber := f.Size.(json.Number)
		modified, isText := f.Modified.(string)
		if !isNumber || strings.ContainsAny(size.String(), ".-eE") || !isText || !isoTime.MatchString(modified) || f.Type != "log" || f.Name == "" {
			t.Errorf("log entry %+v", f)
		}
	}
}

// py: tests/test_files_management.py::test_list_files_ignores_subdirectories
func TestListFilesIgnoresSubdirectories(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// Python made no folders; these are real ones to ignore.
	for _, dir := range []string{ts.LogsDir, ts.CSVDir, ts.QuizzesDir} {
		if err := os.MkdirAll(filepath.Join(dir, "subfolder.log"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	listing := listFiles(t, ts.AdminClient(t, "test123"))
	for _, list := range [][]filesEntry{listing.Logs, listing.CSV, listing.Quizzes} {
		for _, f := range list {
			if f.Name == "subfolder.log" || f.Size == nil || f.Modified == nil || f.Type == nil {
				t.Errorf("entry %+v", f)
			}
		}
	}
	for _, f := range listing.Quizzes {
		if f.Name == "imgs" || f.Name == "attach" {
			t.Errorf("the folder %s is listed as a quiz file", f.Name)
		}
	}
}

// py: tests/test_files_management.py::test_list_files_handles_special_characters
func TestListFilesHandlesSpecialCharacters(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	name := "Математика 5 клас (копія).log"
	if err := os.WriteFile(filepath.Join(ts.LogsDir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range listFiles(t, ts.AdminClient(t, "test123")).Logs {
		if strings.ContainsRune(f.Name, 0) {
			t.Errorf("name with NUL: %q", f.Name)
		}
		found = found || f.Name == name
	}
	if !found {
		t.Errorf("%q is not listed", name)
	}
}

// File Viewing Tests

// py: tests/test_files_management.py::test_view_log_file_success
func TestViewLogFileSuccess(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	logName := listFiles(t, admin).Logs[0].Name
	res := admin.GetJSON(t, "/api/files/logs/view/"+url.PathEscape(logName))
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/plain") || len(res.Body) == 0 {
		t.Errorf("%d %s %q", res.Status, res.Header.Get("Content-Type"), res.Body)
	}
}

// py: tests/test_files_management.py::test_view_csv_file_success
func TestViewCSVFileSuccess(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: fastFlush})
	answerFirstQuestion(t, ts, "testuser")
	admin := ts.AdminClient(t, "test123")
	csvFile := waitForCSV(t, admin, "_0001.csv")
	res := admin.GetJSON(t, "/api/files/csv/view/"+url.PathEscape(csvFile.Name))
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/plain") || len(res.Body) == 0 {
		t.Errorf("%d %s %q", res.Status, res.Header.Get("Content-Type"), res.Body)
	}
}

// py: tests/test_files_management.py::test_view_file_not_found
func TestViewFileNotFound(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.AdminClient(t, "test123").GetJSON(t, "/api/files/logs/view/nonexistent.log")
	if res.Status != 404 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if text, _ := res.JSON(t)["error"].(string); !strings.Contains(strings.ToLower(text), "not found") {
		t.Errorf("error %q", text)
	}
}

// py: tests/test_files_management.py::test_view_empty_file
func TestViewEmptyFile(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// Like the Python test: a file that does not exist gives 404.
	if res := ts.AdminClient(t, "test123").GetJSON(t, "/api/files/logs/view/empty.log"); res.Status != 404 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_view_file_content_type_headers
func TestViewFileContentTypeHeaders(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	res := admin.GetJSON(t, "/api/files/logs/view/"+url.PathEscape(listFiles(t, admin).Logs[0].Name))
	contentType := res.Header.Get("Content-Type")
	if res.Status != 200 || !strings.Contains(contentType, "text/plain") || !strings.Contains(contentType, "utf-8") {
		t.Errorf("%d %q", res.Status, contentType)
	}
}

// File Download Tests

// py: tests/test_files_management.py::test_download_log_file_success
func TestDownloadLogFileSuccess(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	res := admin.GetJSON(t, "/api/files/logs/download/"+url.PathEscape(listFiles(t, admin).Logs[0].Name))
	if res.Status != 200 || len(res.Body) == 0 {
		t.Errorf("%d %q", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_download_csv_file_success
func TestDownloadCSVFileSuccess(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: fastFlush})
	answerFirstQuestion(t, ts, "testuser")
	admin := ts.AdminClient(t, "test123")
	csvFile := waitForCSV(t, admin, "_0001.csv")
	res := admin.GetJSON(t, "/api/files/csv/download/"+url.PathEscape(csvFile.Name))
	if res.Status != 200 || len(res.Body) == 0 {
		t.Errorf("%d %q", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_download_file_not_found
func TestDownloadFileNotFound(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := ts.AdminClient(t, "test123").GetJSON(t, "/api/files/logs/download/nonexistent.log"); res.Status != 404 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_download_headers_correct
func TestDownloadHeadersCorrect(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	logName := listFiles(t, admin).Logs[0].Name
	res := admin.GetJSON(t, "/api/files/logs/download/"+url.PathEscape(logName))
	disposition := res.Header.Get("Content-Disposition")
	if res.Status != 200 || !strings.Contains(disposition, "attachment") || !strings.Contains(disposition, logName) {
		t.Errorf("%d %q", res.Status, disposition)
	}
}

// Security Tests

// py: tests/test_files_management.py::test_path_traversal_prevention_logs
func TestPathTraversalPreventionLogs(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	paths := []string{
		"/api/files/logs/view/../../../etc/passwd", "/api/files/logs/view/logs/../../../sensitive.txt",
		// The same names with the slashes encoded reach the handler itself.
		"/api/files/logs/view/" + url.PathEscape("../../../etc/passwd"),
		"/api/files/logs/view/" + url.PathEscape("logs/../../../sensitive.txt"),
		"/api/files/logs/view/" + url.PathEscape(`..\..\secret.txt`),
	}
	for _, path := range paths {
		res := admin.GetJSON(t, path)
		if res.Status != 400 && res.Status != 404 {
			t.Errorf("%s: %d %s", path, res.Status, res.Body)
		}
		if res.Status == 400 && res.JSON(t)["error"] == nil {
			t.Errorf("%s: no error in %s", path, res.Body)
		}
	}
	for _, name := range []string{"../../../etc/passwd", `..\..\secret.txt`} {
		if res := admin.GetJSON(t, "/api/files/logs/view/"+url.PathEscape(name)); res.Status != 400 {
			t.Errorf("%q: %d, want 400 (Invalid filename)", name, res.Status)
		}
	}
}

// py: tests/test_files_management.py::test_null_byte_injection_prevention
func TestNullByteInjectionPrevention(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	for _, name := range []string{"test.log\x00.txt", "safe.txt\x00../../etc/passwd"} {
		if res := admin.GetJSON(t, "/api/files/logs/view/"+url.PathEscape(name)); res.Status != 400 && res.Status != 404 {
			t.Errorf("%q: %d %s", name, res.Status, res.Body)
		}
	}
}

// py: tests/test_files_management.py::test_special_filename_handling
func TestSpecialFilenameHandling(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	for _, name := range []string{".", "..", "con", "prn", "aux"} {
		if res := admin.GetJSON(t, "/api/files/logs/view/"+name); res.Status != 400 && res.Status != 404 {
			t.Errorf("%q: %d %s", name, res.Status, res.Body)
		}
	}
}

// Integration Tests

// py: tests/test_files_management.py::test_files_page_admin_panel_integration
func TestFilesPageAdminPanelIntegration(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := testutil.WithCookies(ts.AdminCookies(t, "test123")...)
	// The "File Manager" link of the admin page is part of the React admin
	// app (Task 15). Both pages are served, and the files page is the File
	// Manager.
	if res := ts.GetJSON(t, "/admin/", cookies); res.Status != 200 {
		t.Errorf("admin page: %d", res.Status)
	}
	res := ts.GetJSON(t, "/files/", cookies)
	if res.Status != 200 || !strings.Contains(res.Text(), "File Manager") {
		t.Errorf("files page: %d\n%s", res.Status, res.Text())
	}
}

// py: tests/test_files_management.py::test_files_functionality_with_real_server_logs
func TestFilesFunctionalityWithRealServerLogs(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	ts.GetJSON(t, "/")
	logs := listFiles(t, ts.AdminClient(t, "test123")).Logs
	if len(logs) == 0 || logs[0].Name == "" || logs[0].Size == json.Number("0") {
		t.Errorf("logs %+v", logs)
	}
}

// py: tests/test_files_management.py::test_files_functionality_with_real_csv_data
func TestFilesFunctionalityWithRealCSVData(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: fastFlush})
	userID := answerFirstQuestion(t, ts, "integrationuser")
	// The default quiz has one question; Python's test also sent a second.
	ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 2, "selected_answer": 1})
	admin := ts.AdminClient(t, "test123")
	usersCSV := waitForCSV(t, admin, ".users.csv")
	res := admin.GetJSON(t, "/api/files/csv/view/"+url.PathEscape(usersCSV.Name))
	if res.Status != 200 || !strings.Contains(res.Text(), "integrationuser") {
		t.Errorf("%d %q", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_concurrent_file_access
func TestConcurrentFileAccess(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	statuses := make([]int, 3)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() {
			request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files/list", nil)
			response, err := admin.HTTP.Do(request)
			if err != nil {
				statuses[i] = 500
				return
			}
			response.Body.Close()
			statuses[i] = response.StatusCode
		})
	}
	wg.Wait()
	for i, status := range statuses {
		if status != 200 {
			t.Errorf("request %d: %d", i, status)
		}
	}
}

// py: tests/test_files_management.py::test_files_during_active_quiz_session
func TestFilesDuringActiveQuizSession(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	userID := answerFirstQuestion(t, ts, "activeuser")
	listFiles(t, admin)
	ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 2, "selected_answer": 1})
	listFiles(t, admin)
}

// Error Handling Tests

// py: tests/test_files_management.py::test_corrupted_file_handling
func TestCorruptedFileHandling(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := ts.AdminClient(t, "test123").GetJSON(t, "/api/files/logs/view/corrupted.log"); res.Status != 404 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_network_interruption_resilience
func TestNetworkInterruptionResilience(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	listFiles(t, ts.AdminClient(t, "test123"))
}

// CSV Table Preview Tests

// csvPageAssets returns the script and stylesheet files the page loads.
func csvPageAssets(t *testing.T, ts *testutil.TestServer, page string) (scripts, styles []string) {
	t.Helper()
	for _, m := range regexp.MustCompile(`<script[^>]*src="(/assets/[^"]+\.js)"`).FindAllStringSubmatch(page, -1) {
		scripts = append(scripts, m[1])
	}
	for _, m := range regexp.MustCompile(`<link[^>]*href="(/assets/[^"]+\.css)"`).FindAllStringSubmatch(page, -1) {
		styles = append(styles, m[1])
	}
	return scripts, styles
}

// csvFileForTheTable makes a CSV file in csv_dir with quoted fields.
func csvFileForTheTable(t *testing.T, ts *testutil.TestServer) string {
	t.Helper()
	content := "user_id,question,selected_answer\r\n111111,\"Q1, with a comma\",\"line one\r\nline two\"\r\n"
	if err := os.WriteFile(filepath.Join(ts.CSVDir, "results_0001.csv"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return "results_0001.csv"
}

// py: tests/test_files_management.py::test_files_page_has_csv_table_container
func TestFilesPageHasCSVTableContainer(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// The pop-up shows the table page /files/csv/<name> in an iframe; both
	// pages must be served.
	name := csvFileForTheTable(t, ts)
	if res := ts.GetJSON(t, "/files/"); res.Status != 200 {
		t.Errorf("files page: %d", res.Status)
	}
	res := ts.GetJSON(t, "/files/csv/"+name+"?embedded=1")
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		t.Errorf("table page in the iframe: %d %s", res.Status, res.Header.Get("Content-Type"))
	}
}

// py: tests/test_files_management.py::test_csv_table_page_has_table_styles
func TestCSVTablePageHasTableStyles(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/files/csv/any.csv")
	if res.Status != 200 {
		t.Fatalf("%d", res.Status)
	}
	// The table styles are in the stylesheet of the built page.
	_, styles := csvPageAssets(t, ts, res.Text())
	if len(styles) == 0 {
		t.Fatalf("the page loads no stylesheet:\n%s", res.Text())
	}
	for _, style := range styles {
		css := ts.GetJSON(t, style)
		if css.Status != 200 || !strings.Contains(css.Header.Get("Content-Type"), "text/css") || len(css.Body) == 0 {
			t.Errorf("%s: %d %s", style, css.Status, css.Header.Get("Content-Type"))
		}
	}
}

// py: tests/test_files_management.py::test_csv_table_page_has_csv_parsing_function
func TestCSVTablePageHasCSVParsingFunction(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/files/csv/any.csv")
	if res.Status != 200 {
		t.Fatalf("%d", res.Status)
	}
	// parseCSV, renderCsvTable and escapeHtml are in the page's script
	// (tested with Vitest in Task 16); the server must serve it.
	scripts, _ := csvPageAssets(t, ts, res.Text())
	if len(scripts) == 0 {
		t.Fatalf("the page loads no script:\n%s", res.Text())
	}
	for _, script := range scripts {
		js := ts.GetJSON(t, script)
		if js.Status != 200 || !strings.Contains(js.Header.Get("Content-Type"), "javascript") {
			t.Errorf("%s: %d %s", script, js.Status, js.Header.Get("Content-Type"))
		}
	}
}

// py: tests/test_files_management.py::test_files_page_viewfile_accepts_viewmode
func TestFilesPageViewFileAcceptsViewMode(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// Both view modes of a CSV file (text and table) read the same view API.
	name := csvFileForTheTable(t, ts)
	res := ts.AdminClient(t, "test123").GetJSON(t, "/api/files/csv/view/"+name)
	want := "user_id,question,selected_answer\n111111,\"Q1, with a comma\",\"line one\nline two\"\n"
	if res.Status != 200 || res.Text() != want {
		t.Errorf("%d %q", res.Status, res.Body)
	}
}

// py: tests/test_files_management.py::test_files_page_csv_text_table_buttons
func TestFilesPageCSVTextTableButtons(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// The page shows Text and Table buttons for the files of the "csv" list;
	// the list must give the CSV file with its name (the page reads it from
	// data-name).
	name := csvFileForTheTable(t, ts)
	listing := listFiles(t, ts.AdminClient(t, "test123"))
	if len(listing.CSV) != 1 || listing.CSV[0].Name != name || listing.CSV[0].Type != "csv" {
		t.Errorf("csv list %+v", listing.CSV)
	}
}

// py: tests/test_files_management.py::test_files_page_csv_specific_buttons
func TestFilesPageCSVSpecificButtons(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// Only CSV files get the table view; the page tells them by their type.
	csvFileForTheTable(t, ts)
	listing := listFiles(t, ts.AdminClient(t, "test123"))
	types := map[any]bool{}
	for _, list := range [][]filesEntry{listing.Logs, listing.CSV, listing.Quizzes} {
		for _, f := range list {
			types[f.Type] = true
		}
	}
	if len(types) != 3 || !types["log"] || !types["csv"] || !types["yaml"] {
		t.Errorf("types %v", types)
	}
}

// tests/test_csv_table_viewer.py

// py: tests/test_csv_table_viewer.py::test_csv_table_page_is_served
func TestCSVTablePageIsServed(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/files/csv/results_0001.csv")
	if res.Status != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("%d %s", res.Status, res.Header.Get("Content-Type"))
	}
	// The filters and the row count are made by the React page (Task 16);
	// the server sends the page with its title and boot data.
	if !strings.Contains(res.Text(), "<title>CSV Table - WebQuiz</title>") {
		t.Errorf("title:\n%s", res.Text())
	}
	pageBoot(t, res.Text())
}

// py: tests/test_csv_table_viewer.py::test_csv_table_page_reads_file_through_admin_api
func TestCSVTablePageReadsFileThroughAdminAPI(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	name := csvFileForTheTable(t, ts)
	if res := ts.GetJSON(t, "/files/csv/"+name); res.Status != 200 {
		t.Fatalf("page: %d", res.Status)
	}
	// The page reads the file through the protected view API, not directly.
	if res := ts.GetJSON(t, "/api/files/csv/view/"+name); res.Status != 401 {
		t.Errorf("without a session: %d", res.Status)
	}
	if res := ts.AdminClient(t, "test123").GetJSON(t, "/api/files/csv/view/"+name); res.Status != 200 || !strings.Contains(res.Text(), "Q1, with a comma") {
		t.Errorf("with a session: %d %q", res.Status, res.Body)
	}
}

// py: tests/test_csv_table_viewer.py::test_csv_table_page_blocked_from_public_ip
func TestCSVTablePageBlockedFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := ts.GetJSON(t, "/files/csv/results_0001.csv", testutil.FromIP("8.8.8.8")); res.Status != 403 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_csv_table_viewer.py::test_files_page_shows_table_in_iframe
func TestFilesPageShowsTableInIframe(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// The iframe of the files page loads /files/csv/<name>?embedded=1.
	if res := ts.GetJSON(t, "/files/"); res.Status != 200 {
		t.Errorf("files page: %d", res.Status)
	}
	if res := ts.GetJSON(t, "/files/csv/results_0001.csv?embedded=1"); res.Status != 200 {
		t.Errorf("embedded table page: %d", res.Status)
	}
}

// py: tests/test_csv_table_viewer.py::test_files_page_has_new_window_link_for_csv
func TestFilesPageHasNewWindowLinkForCSV(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// "↗ New window" opens /files/csv/<name> as its own page, also for a
	// name that needs escaping in the address.
	name := "results 5 клас_0001.csv"
	if err := os.WriteFile(filepath.Join(ts.CSVDir, name), []byte("a,b\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := ts.GetJSON(t, "/files/csv/"+url.PathEscape(name)); res.Status != 200 || !strings.Contains(res.Text(), "webquiz-boot") {
		t.Errorf("table page: %d", res.Status)
	}
}
