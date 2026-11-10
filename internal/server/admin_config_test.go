package server_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// saveConfig sends a full YAML text to PUT /api/admin/config.
func saveConfig(t *testing.T, admin *testutil.Client, content string) *testutil.Response {
	t.Helper()
	return admin.Put(t, "/api/admin/config", map[string]any{"content": content})
}

// savePartial sends a JSON partial update to PUT /api/admin/config.
func savePartial(t *testing.T, admin *testutil.Client, data any) *testutil.Response {
	t.Helper()
	return admin.Put(t, "/api/admin/config", map[string]any{"data": data})
}

// okSave checks a successful save and returns its answer.
func okSave(t *testing.T, res *testutil.Response) map[string]any {
	t.Helper()
	if res.Status != 200 {
		t.Fatalf("status %d: %s", res.Status, res.Text())
	}
	body := res.JSON(t)
	if body["success"] != true {
		t.Fatalf("not a success: %s", res.Text())
	}
	return body
}

// rejectedSave checks a 400 answer and returns it.
func rejectedSave(t *testing.T, res *testutil.Response) map[string]any {
	t.Helper()
	if res.Status != 400 {
		t.Fatalf("status %d, want 400: %s", res.Status, res.Text())
	}
	return res.JSON(t)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// loadYAML reads YAML text with PyYAML's rules, as yaml.safe_load does.
func loadYAML(t *testing.T, text string) map[string]any {
	t.Helper()
	value, err := pyval.LoadYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("not a mapping: %q", text)
	}
	return m
}

func TestConfigSaveFullYAML(t *testing.T) {
	t.Parallel()

	// py: tests/test_config_management.py::test_update_config_with_valid_data
	t.Run("valid data", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		admin := ts.AdminClient(t, "test123")
		// The test sets paths that differ from the test server's, so a restart
		// is rightly required for them.
		body := okSave(t, saveConfig(t, admin, `server:
  host: "0.0.0.0"
  port: 8080

paths:
  quizzes_dir: "quizzes"
  logs_dir: "logs"
  csv_dir: "data"
  static_dir: "static"

admin:
  master_key: "test123"
  trusted_ips:
    - "127.0.0.1"

options:
  flush_interval: 30

registration:
  fields:
    - "Grade"
    - "School"
`))
		message := strings.ToLower(body["message"].(string))
		if !strings.Contains(message, "restart required") && !strings.Contains(message, "saved and applied") {
			t.Errorf("message %q", body["message"])
		}
		if _, ok := body["config_path"]; !ok {
			t.Error("no config_path")
		}
	})

	// py: tests/test_config_management.py::test_update_config_with_empty_content
	t.Run("empty content", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), ""))
	})

	// py: tests/test_config_management.py::test_update_config_with_only_some_sections
	t.Run("only some sections", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "server:\n  port: 9000\n"))
	})

	// py: tests/test_config_management.py::test_config_with_registration_fields
	t.Run("registration fields", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  fields:\n    - \"Grade\"\n    - \"School\"\n    - \"Teacher\"\n"))
	})

	// py: tests/test_config_management.py::test_config_with_quizzes
	t.Run("downloadable quizzes", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), `quizzes:
  - name: "Math Quiz"
    download_path: "https://example.com/math.zip"
    folder: "math_questions/"
  - name: "Science Quiz"
    download_path: "https://example.com/science.zip"
    folder: "science/"
`))
	})

	// py: tests/test_config_management.py::test_config_master_key_can_be_null
	t.Run("master key null", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "admin:\n  master_key: null\n"))
	})
}

func TestConfigSaveRejectsBadYAML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		py      string
		content string
		words   []string // all must be in the lower-case text of the errors
		syntax  bool     // the error is "Invalid YAML syntax", not a validation failure
	}{
		{py: "tests/test_config_management.py::test_reject_invalid_yaml_syntax", content: "server:\n  host: \"unclosed", syntax: true},
		{py: "tests/test_config_management.py::test_reject_invalid_port_out_of_range", content: "server:\n  port: 70000\n", words: []string{"port"}},
		{py: "tests/test_config_management.py::test_reject_invalid_port_type", content: "server:\n  port: \"8080\"\n", words: []string{"port", "integer"}},
		{py: "tests/test_config_management.py::test_reject_server_section_as_list", content: "server:\n  - host: \"0.0.0.0\"\n  - port: 8080\n", words: []string{"server", "dictionary"}},
		{py: "tests/test_config_management.py::test_reject_invalid_trusted_ips_not_list", content: "admin:\n  trusted_ips: \"127.0.0.1\"\n", words: []string{"trusted_ips", "list"}},
		{py: "tests/test_config_management.py::test_reject_invalid_quizzes_missing_fields", content: "quizzes:\n  - name: \"Test Quiz\"\n    download_path: \"https://example.com/quiz.zip\"\n", words: []string{"folder"}},
		{py: "tests/test_config_management.py::test_reject_negative_port", content: "server:\n  port: -1\n", words: []string{"port"}},
	}
	for _, c := range cases {
		t.Run(c.py[strings.LastIndex(c.py, "::")+2:], func(t *testing.T) {
			t.Parallel()
			ts := testutil.StartServer(t, testutil.Options{})
			body := rejectedSave(t, saveConfig(t, ts.AdminClient(t, "test123"), c.content))
			if c.syntax {
				if !strings.Contains(body["error"].(string), "Invalid YAML syntax") {
					t.Errorf("error %v", body["error"])
				}
				return
			}
			if !strings.Contains(strings.ToLower(body["error"].(string)), "validation failed") {
				t.Errorf("error %v", body["error"])
			}
			errorsText := strings.ToLower(jsonText(body["errors"]))
			for _, word := range c.words {
				if !strings.Contains(errorsText, word) {
					t.Errorf("errors %s lack %q", errorsText, word)
				}
			}
		})
	}
}

// py: tests/test_config_management.py::test_config_requires_authentication
func TestConfigSaveNeedsAdminSession(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.Put(t, "/api/admin/config", map[string]any{"content": "server:\n  port: 8080"})
	if res.Status != 401 {
		t.Errorf("status %d: %s", res.Status, res.Text())
	}
}

// py: tests/test_config_management.py::test_config_registration_fields_saved_to_file
func TestConfigRegistrationFieldsSavedToFile(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	body := okSave(t, saveConfig(t, admin, `registration:
  fields:
    - "Grade"
    - "School"
    - "Teacher"
  approve: true
  username_label: "Student Name"
`))
	saved := readFile(t, body["config_path"].(string))
	for _, want := range []string{"fields:", "Grade", "School", "Teacher", "approve: true", "Student Name"} {
		if !strings.Contains(saved, want) {
			t.Errorf("the saved file lacks %q:\n%s", want, saved)
		}
	}
	registration := loadYAML(t, saved)["registration"].(map[string]any)
	if !pyval.Equal(registration["fields"], []any{"Grade", "School", "Teacher"}) || registration["approve"] != true || registration["username_label"] != "Student Name" {
		t.Errorf("registration %v", registration)
	}
}

// py: tests/test_config_management.py::test_config_registration_fields_reload_from_files_page
func TestConfigFieldsReloadFromFilesPage(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	okSave(t, saveConfig(t, admin, "registration:\n  fields:\n    - \"Grade\"\n    - \"School\"\n"))
	page := admin.Do(t, "GET", "/files/", nil)
	if page.Status != 200 {
		t.Fatalf("status %d", page.Status)
	}
	// The boot data replaces CONFIG_CONTENT of the old page.
	content, _ := pageBoot(t, page.Text())["config_content"].(string)
	for _, want := range []string{"fields:", "Grade", "School"} {
		if !strings.Contains(content, want) {
			t.Errorf("config_content lacks %q: %q", want, content)
		}
	}
}

// py: tests/test_config_management.py::test_files_page_includes_config_data
func TestFilesPageIncludesConfigData(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	okSave(t, saveConfig(t, admin, "registration:\n  approve: true\n  fields:\n    - Grade\n"))
	page := admin.Do(t, "GET", "/files/", nil)
	if page.Status != 200 {
		t.Fatalf("status %d", page.Status)
	}
	// The boot data replaces CONFIG_DATA of the old page.
	data, _ := pageBoot(t, page.Text())["config_data"].(map[string]any)
	registration, _ := data["registration"].(map[string]any)
	if registration == nil || registration["approve"] != true || jsonText(registration["fields"]) != `["Grade"]` {
		t.Errorf("config_data %v", data)
	}
}

func TestConfigSavePartialJSON(t *testing.T) {
	t.Parallel()

	// py: tests/test_config_management.py::test_save_config_json_registration
	t.Run("registration", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		admin := ts.AdminClient(t, "test123")
		body := okSave(t, savePartial(t, admin, map[string]any{"registration": map[string]any{
			"approve": true, "fields": []any{"Grade", "School"}, "username_label": "Student",
		}}))
		if _, ok := body["config_content"]; !ok {
			t.Error("no config_content")
		}
		data := body["config_data"].(map[string]any)["registration"].(map[string]any)
		if data["approve"] != true || jsonText(data["fields"]) != `["Grade","School"]` || data["username_label"] != "Student" {
			t.Errorf("config_data registration %v", data)
		}
		registration := loadYAML(t, readFile(t, body["config_path"].(string)))["registration"].(map[string]any)
		if registration["approve"] != true || jsonText(registration["fields"]) != `["Grade","School"]` || registration["username_label"] != "Student" {
			t.Errorf("saved registration %v", registration)
		}
	})

	// py: tests/test_config_management.py::test_save_config_json_language
	t.Run("language", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		admin := ts.AdminClient(t, "test123")
		body := okSave(t, savePartial(t, admin, map[string]any{"language": "en"}))
		if body["config_data"].(map[string]any)["language"] != "en" {
			t.Errorf("config_data %v", body["config_data"])
		}
		if loadYAML(t, readFile(t, body["config_path"].(string)))["language"] != "en" {
			t.Error("language is not in the file")
		}
		// Hot reload: the quiz page now speaks English.
		page := ts.Do(t, "GET", "/", nil)
		if page.Status != 200 || !strings.Contains(page.Text(), "Register to start the quiz") || strings.Contains(page.Text(), "Зареєструйтеся для початку тесту") {
			t.Errorf("the page does not use English (status %d)", page.Status)
		}
	})

	// py: tests/test_config_management.py::test_save_config_json_language_rejects_invalid
	t.Run("language rejects invalid", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		body := rejectedSave(t, savePartial(t, ts.AdminClient(t, "test123"), map[string]any{"language": "fr"}))
		errs, _ := body["errors"].([]any)
		if len(errs) == 0 || !strings.Contains(strings.ToLower(jsonText(errs)), "language") {
			t.Errorf("errors %v", body["errors"])
		}
	})

	// py: tests/test_config_management.py::test_save_config_json_preserves_other_sections
	t.Run("preserves other sections", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		admin := ts.AdminClient(t, "test123")
		okSave(t, saveConfig(t, admin, "server:\n  port: 9090\nregistration:\n  approve: false\n"))
		body := okSave(t, savePartial(t, admin, map[string]any{"registration": map[string]any{"approve": true, "fields": []any{"Group"}}}))
		saved := loadYAML(t, readFile(t, body["config_path"].(string)))
		port, _ := pyval.Int(saved["server"].(map[string]any)["port"])
		registration := saved["registration"].(map[string]any)
		if port != 9090 || registration["approve"] != true || jsonText(registration["fields"]) != `["Group"]` {
			t.Errorf("saved %v", saved)
		}
	})

	// py: tests/test_config_management.py::test_save_config_json_validation_error
	t.Run("validation error", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		body := rejectedSave(t, savePartial(t, ts.AdminClient(t, "test123"), map[string]any{"registration": map[string]any{"fields": "not_a_list"}}))
		if !strings.Contains(strings.ToLower(body["error"].(string)), "validation failed") {
			t.Errorf("error %v", body["error"])
		}
	})

	// py: tests/test_config_management.py::test_save_config_json_empty_data
	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		admin := ts.AdminClient(t, "test123")
		okSave(t, saveConfig(t, admin, "registration:\n  approve: true\n"))
		body := okSave(t, savePartial(t, admin, map[string]any{}))
		registration := loadYAML(t, readFile(t, body["config_path"].(string)))["registration"].(map[string]any)
		if registration["approve"] != true {
			t.Errorf("registration %v", registration)
		}
	})

	// py: tests/test_config_management.py::test_save_config_json_data_must_be_dict
	t.Run("data must be a dictionary", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		body := rejectedSave(t, savePartial(t, ts.AdminClient(t, "test123"), "not_a_dict"))
		if !strings.Contains(strings.ToLower(body["error"].(string)), "dictionary") {
			t.Errorf("error %v", body["error"])
		}
	})
}

// py: tests/test_config_management.py::test_save_config_json_preserves_comments_and_blank_lines
func TestConfigSavePartialKeepsCommentsAndBlankLines(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	// A no-op save tells the path the server uses.
	configPath := okSave(t, savePartial(t, admin, map[string]any{}))["config_path"].(string)

	// A hand-formatted file with comments and blank lines between sections.
	port := ts.Port
	commented := "# Top-of-file comment\n\nserver:\n  # port the server listens on\n  port: " + itoa(port) + "\n\n" +
		"paths:\n  quizzes_dir: " + ts.QuizzesDir + "\n  logs_dir: " + ts.LogsDir + "\n  csv_dir: " + ts.CSVDir + "\n  static_dir: " + ts.StaticDir + "\n\n" +
		"admin:\n  master_key: test123  # do not share\n  trusted_ips: []\n\n" +
		"# Student registration settings\nregistration:\n  approve: false\n"
	if err := os.WriteFile(configPath, []byte(commented), 0o644); err != nil {
		t.Fatal(err)
	}
	okSave(t, savePartial(t, admin, map[string]any{"registration": map[string]any{"approve": true}}))

	saved := readFile(t, configPath)
	for _, want := range []string{
		"# Top-of-file comment", "# port the server listens on", "# do not share", "# Student registration settings",
		"\n\nserver:", "\n\npaths:", "\n\nadmin:", "\n\n# Student registration settings\nregistration:",
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("the saved file lacks %q:\n%s", want, saved)
		}
	}
	parsed := loadYAML(t, saved)
	savedPort, _ := pyval.Int(parsed["server"].(map[string]any)["port"])
	if parsed["registration"].(map[string]any)["approve"] != true || int(savedPort) != port || parsed["admin"].(map[string]any)["master_key"] != "test123" {
		t.Errorf("parsed %v", parsed)
	}
}

// The classroom harness found this: the config form saved to a file whose
// last section is registration and that has no language line. The merge put
// "language: en" between the section and its new keys, and the save failed
// with "Invalid YAML syntax". Now the save works and keeps the comments.
func TestConfigFormSaveWhenRegistrationIsTheLastSection(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	text := "# WebQuiz config\n\nserver:\n  port: " + itoa(ts.Port) + "   # the port\n\n" +
		"paths:\n  quizzes_dir: " + ts.QuizzesDir + "\n  logs_dir: " + ts.LogsDir + "\n  csv_dir: " + ts.CSVDir + "\n  static_dir: " + ts.StaticDir + "\n\n" +
		"admin:\n  master_key: test123\n  trusted_ips: []\n\n" +
		"# Registration form\nregistration:\n  fields:\n    - Клас\n    - Школа\n"
	if err := os.WriteFile(ts.ConfigPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	body := okSave(t, savePartial(t, admin, map[string]any{
		"language":                     "en",
		"extra_answers_with_users_csv": false,
		"registration": map[string]any{
			"approve": false, "username_label": "", "fields": []any{"Клас"}, "disabled_fields": []any{"Школа"},
		},
	}))
	const wantRegistration = `{"approve":false,"disabled_fields":["Школа"],"fields":["Клас"],"username_label":""}`

	// The file reads back with the new values and keeps its comments.
	saved := readFile(t, ts.ConfigPath)
	parsed := loadYAML(t, saved)
	if parsed["language"] != "en" || parsed["extra_answers_with_users_csv"] != false || jsonText(parsed["registration"]) != wantRegistration {
		t.Errorf("saved values %v:\n%s", parsed, saved)
	}
	for _, want := range []string{"# WebQuiz config\n\nserver:", "port: " + itoa(ts.Port) + "   # the port\n", "\n\n# Registration form\nregistration:\n"} {
		if !strings.Contains(saved, want) {
			t.Errorf("the saved file lacks %q:\n%s", want, saved)
		}
	}

	// The answer and the files page show the new values.
	if body["config_content"] != saved {
		t.Errorf("config_content %q, want the saved file", body["config_content"])
	}
	data := body["config_data"].(map[string]any)
	if data["language"] != "en" || data["extra_answers_with_users_csv"] != false || jsonText(data["registration"]) != wantRegistration {
		t.Errorf("config_data %v", data)
	}
	page := admin.Do(t, "GET", "/files/", nil)
	if page.Status != 200 {
		t.Fatalf("files page: status %d", page.Status)
	}
	data, _ = pageBoot(t, page.Text())["config_data"].(map[string]any)
	if data["language"] != "en" || jsonText(data["registration"]) != wantRegistration {
		t.Errorf("config_data of the files page %v", data)
	}

	// The new settings run: the quiz page speaks English.
	if quiz := ts.Do(t, "GET", "/", nil); !strings.Contains(quiz.Text(), "Register to start the quiz") {
		t.Errorf("the quiz page does not use English (status %d)", quiz.Status)
	}
}

// py: tests/test_config_management.py::test_save_config_yaml_returns_both_formats
func TestConfigSaveYAMLReturnsBothFormats(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  approve: true\n  fields:\n    - Grade\n"))
	if _, ok := body["config_content"]; !ok {
		t.Error("no config_content")
	}
	registration := body["config_data"].(map[string]any)["registration"].(map[string]any)
	if registration["approve"] != true || jsonText(registration["fields"]) != `["Grade"]` {
		t.Errorf("registration %v", registration)
	}
}

func itoa(n int) string { return pyval.Str(n) }

// The cases of test_config_hot_reload.py.

// py: tests/test_config_hot_reload.py::test_config_hot_reload_registration_fields
func TestHotReloadRegistrationFields(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  fields:\n    - \"Grade\"\n    - \"School\"\n"))
	if !strings.Contains(body["message"].(string), "saved and applied") {
		t.Errorf("message %v", body["message"])
	}
	page := ts.Do(t, "GET", "/", nil)
	if page.Status != 200 || !strings.Contains(page.Text(), "Grade") || !strings.Contains(page.Text(), "School") {
		t.Errorf("the quiz page lacks the new fields (status %d)", page.Status)
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_registration_approve
func TestHotReloadRegistrationApprove(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"registration": map[string]any{"approve": false}}})
	first := ts.PostJSON(t, "/api/register", map[string]any{"username": "User1"})
	if first.Status != 200 || first.JSON(t)["approved"] != true {
		t.Fatalf("first registration: %d %s", first.Status, first.Text())
	}
	okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  approve: true\n"))
	second := ts.PostJSON(t, "/api/register", map[string]any{"username": "User2"})
	reply := second.JSON(t)
	if second.Status != 200 || reply["approved"] != false || reply["requires_approval"] != true {
		t.Errorf("second registration: %d %s", second.Status, second.Text())
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_username_label
func TestHotReloadUsernameLabel(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if page := ts.Do(t, "GET", "/", nil); !strings.Contains(page.Text(), "Ім'я користувача") && !strings.Contains(strings.ToLower(page.Text()), "username") {
		t.Error("the default label is missing")
	}
	okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  username_label: \"Student Name\"\n"))
	page := ts.Do(t, "GET", "/", nil)
	if page.Status != 200 || !strings.Contains(page.Text(), "Student Name") {
		t.Errorf("the new label is missing (status %d)", page.Status)
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_trusted_ips
func TestHotReloadTrustedIPs(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "admin:\n  master_key: \"test123\"\n  trusted_ips:\n    - \"192.168.1.100\"\n    - \"10.0.0.1\"\n"))
	// A trusted address needs no key; the old test only checked the save.
	if res := ts.Do(t, "POST", "/api/admin/auth", map[string]any{"master_key": "wrong"}, testutil.FromIP("192.168.1.100")); res.Status != 200 {
		t.Errorf("trusted 192.168.1.100 gets %d: %s", res.Status, res.Text())
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_resets_quiz_state
func TestHotReloadResetsQuizState(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	userID := ts.PostJSON(t, "/api/register", map[string]any{"username": "TestUser"}).JSON(t)["user_id"].(string)
	if before := ts.GetJSON(t, "/api/verify-user/"+userID); before.Status != 200 || before.JSON(t)["valid"] != true {
		t.Fatalf("before: %d %s", before.Status, before.Text())
	}
	okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  approve: false\n"))
	after := ts.GetJSON(t, "/api/verify-user/"+userID)
	reply := after.JSON(t)
	if after.Status != 200 || reply["valid"] != false || !strings.Contains(strings.ToLower(reply["message"].(string)), "not found") {
		t.Errorf("after: %d %s", after.Status, after.Text())
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_notifies_websocket_clients
func TestHotReloadNotifiesLiveStatsClients(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // the initial state
	okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  approve: false\n"))
	for {
		if live.next()["type"] == "quiz_switched" {
			return
		}
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_without_active_quiz
func TestHotReloadWithoutActiveQuiz(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{}})
	// With no quiz file the server makes default.yaml, as Python did, so a
	// quiz runs anyway; the old test only checked that the save works.
	okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "registration:\n  fields:\n    - \"Grade\"\n"))
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_preserves_master_key
func TestHotReloadKeepsMasterKey(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "admin:\n  master_key: \"new_master_key_12345\"\n  trusted_ips: []\n"))
	if res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"}); res.Status != 200 {
		t.Errorf("the old key gets %d", res.Status)
	}
	if res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "new_master_key_12345"}); res.Status != 401 {
		t.Errorf("the new key gets %d", res.Status)
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_quizzes_list
func TestHotReloadQuizzesList(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	body := okSave(t, saveConfig(t, admin, `quizzes:
  - name: "Math Quiz"
    download_path: "https://example.com/math.zip"
    folder: "math/"
  - name: "Science Quiz"
    download_path: "https://example.com/science.zip"
    folder: "science/"
`))
	if !strings.Contains(body["message"].(string), "saved and applied") {
		t.Errorf("message %v", body["message"])
	}
	page := admin.Do(t, "GET", "/admin/", nil)
	if page.Status != 200 || !strings.Contains(page.Text(), "Math Quiz") || !strings.Contains(page.Text(), "Science Quiz") {
		t.Errorf("the admin page lacks the quizzes (status %d)", page.Status)
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_tunnel_config
func TestHotReloadTunnelConfig(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "tunnel:\n  server: \"tunnel.example.com\"\n  socket_name: \"myquiz\"\n"))
	if !strings.Contains(body["message"].(string), "saved and applied") {
		t.Errorf("message %v", body["message"])
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_restart_required_message
func TestRestartRequiredForPort(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "server:\n  port: 9999\n"))
	message := body["message"].(string)
	required, _ := body["restart_required"].([]any)
	if !strings.Contains(strings.ToLower(message), "restart required") || !strings.Contains(message, "server.port") || !slices.Contains(required, any("server.port")) {
		t.Errorf("message %q, restart_required %v", message, required)
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_restart_required_paths
func TestRestartRequiredForPaths(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "paths:\n  quizzes_dir: \"new_quizzes\"\n  csv_dir: \"new_data\"\n"))
	message := body["message"].(string)
	if !strings.Contains(strings.ToLower(message), "restart required") || !strings.Contains(message, "paths.quizzes_dir") || !strings.Contains(message, "paths.csv_dir") {
		t.Errorf("message %q", message)
	}
}

// py: tests/test_config_hot_reload.py::test_config_hot_reload_restart_required_master_key
func TestRestartRequiredForMasterKey(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := okSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "admin:\n  master_key: \"new_key_123\"\n  trusted_ips: []\n"))
	message := body["message"].(string)
	if !strings.Contains(strings.ToLower(message), "restart required") || !strings.Contains(message, "admin.master_key") {
		t.Errorf("message %q", message)
	}
}

// The language cases of test_language.py. The old tests also called
// switch-quiz with a wrong key (it answered 500 and changed nothing); the
// quiz test_quiz.yaml is the only quiz, so it already runs.

// quizPage returns the quiz page. The texts of the interface are in the boot
// data (translations), so they are in the page text.
func quizPage(t *testing.T, ts *testutil.TestServer) string {
	t.Helper()
	page := ts.Do(t, "GET", "/", nil)
	if page.Status != 200 {
		t.Fatalf("GET / gave %d", page.Status)
	}
	return page.Text()
}

// py: tests/test_language.py::test_default_language_shows_ukrainian
func TestDefaultLanguageShowsUkrainian(t *testing.T) {
	t.Parallel()
	html := quizPage(t, testutil.StartServer(t, testutil.Options{}))
	for _, want := range []string{"Зареєструйтеся для початку тесту", "Зареєструватися", "Відправити відповідь"} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

// py: tests/test_language.py::test_english_language_shows_english
func TestEnglishLanguageShowsEnglish(t *testing.T) {
	t.Parallel()
	html := quizPage(t, testutil.StartServer(t, testutil.Options{Config: map[string]any{"language": "en"}}))
	for _, want := range []string{"Register to start the quiz", "Register", "Submit answer"} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	for _, unwanted := range []string{"Зареєструйтеся", "Відправити відповідь"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("the page has %q", unwanted)
		}
	}
}

// py: tests/test_language.py::test_english_css_feedback_labels
func TestEnglishFeedbackLabels(t *testing.T) {
	t.Parallel()
	html := quizPage(t, testutil.StartServer(t, testutil.Options{Config: map[string]any{"language": "en"}}))
	for _, want := range []string{"Correct answer", "Your answer", "Also correct"} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

func TestLanguageOfRegistrationErrors(t *testing.T) {
	t.Parallel()
	register := func(t *testing.T, ts *testutil.TestServer, username string) (int, string) {
		res := ts.PostJSON(t, "/api/register", map[string]any{"username": username})
		reply, _ := res.JSON(t)["error"].(string)
		return res.Status, reply
	}

	// py: tests/test_language.py::test_english_registration_errors
	t.Run("english empty username", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"language": "en"}})
		if status, text := register(t, ts, ""); status != 400 || text != "Username cannot be empty" {
			t.Errorf("%d %q", status, text)
		}
	})

	// py: tests/test_language.py::test_english_duplicate_username_error
	t.Run("english duplicate username", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"language": "en"}})
		register(t, ts, "TestUser")
		if status, text := register(t, ts, "TestUser"); status != 400 || text != "Username already exists" {
			t.Errorf("%d %q", status, text)
		}
	})

	// py: tests/test_language.py::test_ukrainian_registration_errors
	t.Run("ukrainian empty username", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{})
		if status, text := register(t, ts, ""); status != 400 || text != "Ім'я користувача не може бути порожнім" {
			t.Errorf("%d %q", status, text)
		}
	})

	// py: tests/test_language.py::test_english_submit_answer_user_not_found
	t.Run("english user not found", func(t *testing.T) {
		t.Parallel()
		ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"language": "en"}})
		res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": "999999", "question_id": 0, "selected_answer": 1})
		if res.Status != 404 || res.JSON(t)["error"] != "User not found" {
			t.Errorf("%d %s", res.Status, res.Text())
		}
	})
}

const russianRejection = "російська мова не підтримується, але ти москалику попався"

// py: tests/test_language.py::test_invalid_language_rejected
func TestInvalidLanguageRejected(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := rejectedSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "language: fr\n"))
	if !strings.Contains(strings.ToLower(jsonText(body["errors"])), "language") {
		t.Errorf("body %v", body)
	}
}

// py: tests/test_language.py::test_valid_language_en_accepted
func TestValidLanguageEnAccepted(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := saveConfig(t, ts.AdminClient(t, "test123"), "language: en\n"); res.Status != 200 {
		t.Errorf("status %d: %s", res.Status, res.Text())
	}
}

// py: tests/test_language.py::test_russian_language_rejected_via_yaml_content
func TestRussianRejectedViaYAMLContent(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := rejectedSave(t, saveConfig(t, ts.AdminClient(t, "test123"), "language: ru\n"))
	if !strings.Contains(pyval.Str(body["errors"]), russianRejection) {
		t.Errorf("body %v", body)
	}
}

// py: tests/test_language.py::test_russian_language_rejected_via_wizard_data
func TestRussianRejectedViaWizardData(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	body := rejectedSave(t, savePartial(t, ts.AdminClient(t, "test123"), map[string]any{"language": "ru"}))
	if !strings.Contains(pyval.Str(body["errors"]), russianRejection) {
		t.Errorf("body %v", body)
	}
}

// py: tests/test_language.py::test_russian_language_in_yaml_file_falls_back_to_uk
func TestRussianInYAMLFileFallsBackToUkrainian(t *testing.T) {
	t.Parallel()
	text, err := yaml.Marshal(map[string]any{"language": "ru"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "webquiz.yaml")
	if err := os.WriteFile(path, text, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Python logged the rejection while loading; Load returns it as a warning
	// for the caller to log.
	if cfg.Language != "uk" || !slices.Contains(cfg.LoadWarnings, russianRejection) {
		t.Errorf("language %q, warnings %v", cfg.Language, cfg.LoadWarnings)
	}
}
