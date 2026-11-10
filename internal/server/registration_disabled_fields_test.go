package server_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
	"gopkg.in/yaml.v3"
)

// paSkipWithoutConfigSave skips a test while PUT /api/admin/config is still the
// 501 stub (Task 9 builds it). The test runs once that task is merged.
func paSkipWithoutConfigSave(t *testing.T, res *testutil.Response) {
	t.Helper()
	if res.Status == 501 {
		t.Skip("PUT /api/admin/config is not built yet (Task 9)")
	}
}

func paDisabledFieldsServer(t *testing.T, registration map[string]any) *testutil.TestServer {
	t.Helper()
	return testutil.StartServer(t, testutil.Options{Config: map[string]any{"registration": registration}})
}

// py: tests/test_registration_disabled_fields.py::test_disabled_field_not_required_at_register
func TestDisabledFieldNotRequiredAtRegister(t *testing.T) {
	t.Parallel()
	ts := paDisabledFieldsServer(t, map[string]any{"fields": []any{"Grade"}, "disabled_fields": []any{"School"}})
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "student_a", "grade": "10"})
	if res.Status != 200 || res.JSON(t)["user_id"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_disabled_fields.py::test_disabled_field_becomes_required_when_enabled
func TestDisabledFieldBecomesRequiredWhenEnabled(t *testing.T) {
	t.Parallel()
	ts := paDisabledFieldsServer(t, map[string]any{"fields": []any{}, "disabled_fields": []any{"Grade"}})
	if res := ts.PostJSON(t, "/api/register", map[string]any{"username": "stu1"}); res.Status != 200 {
		t.Fatalf("without grade: %d %s", res.Status, res.Body)
	}
	cookies := ts.AdminCookies(t, "test123")
	res := ts.Put(t, "/api/admin/config", map[string]any{"data": map[string]any{
		"registration": map[string]any{"fields": []any{"Grade"}, "disabled_fields": []any{}},
	}}, testutil.WithCookies(cookies...))
	paSkipWithoutConfigSave(t, res)
	if res.Status != 200 {
		t.Fatalf("config save: %d %s", res.Status, res.Body)
	}
	// The quiz restarts on config save and clears the users, so the name is free.
	if res := ts.PostJSON(t, "/api/register", map[string]any{"username": "stu2"}); res.Status != 400 {
		t.Errorf("grade is required now: %d %s", res.Status, res.Body)
	}
	if res := ts.PostJSON(t, "/api/register", map[string]any{"username": "stu2", "grade": "9"}); res.Status != 200 {
		t.Errorf("with grade: %d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_disabled_fields.py::test_partial_save_persists_disabled_fields
func TestPartialSavePersistsDisabledFields(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := ts.AdminCookies(t, "test123")
	res := ts.Put(t, "/api/admin/config", map[string]any{"data": map[string]any{
		"registration": map[string]any{"fields": []any{"Grade"}, "disabled_fields": []any{"School", "Teacher"}},
	}}, testutil.WithCookies(cookies...))
	paSkipWithoutConfigSave(t, res)
	if res.Status != 200 {
		t.Fatalf("config save: %d %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	registration := data["config_data"].(map[string]any)["registration"].(map[string]any)
	if jsonText(registration["fields"]) != `["Grade"]` || jsonText(registration["disabled_fields"]) != `["School","Teacher"]` {
		t.Errorf("response config_data: %v", registration)
	}
	text, err := os.ReadFile(data["config_path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Registration struct {
			Fields         []string `yaml:"fields"`
			DisabledFields []string `yaml:"disabled_fields"`
		} `yaml:"registration"`
	}
	if err := yaml.Unmarshal(text, &saved); err != nil {
		t.Fatal(err)
	}
	if strings.Join(saved.Registration.Fields, ",") != "Grade" || strings.Join(saved.Registration.DisabledFields, ",") != "School,Teacher" {
		t.Errorf("file on disk:\n%s", text)
	}
}

// The Python page had `const CONFIG_DATA = ...;` in a script; the Go page
// carries the same data as config_data in the boot JSON.
// py: tests/test_registration_disabled_fields.py::test_files_page_exposes_disabled_fields
func TestFilesPageExposesDisabledFields(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	content := "registration:\n  fields:\n    - Grade\n  disabled_fields:\n    - School\n    - Teacher\n"
	res := admin.Put(t, "/api/admin/config", map[string]any{"content": content})
	paSkipWithoutConfigSave(t, res)
	if res.Status != 200 {
		t.Fatalf("config save: %d %s", res.Status, res.Body)
	}
	page := admin.GetJSON(t, "/files/")
	if page.Status != 200 {
		t.Fatalf("/files/: %d", page.Status)
	}
	configData, _ := pageBoot(t, page.Text())["config_data"].(map[string]any)
	registration, _ := configData["registration"].(map[string]any)
	if jsonText(registration["fields"]) != `["Grade"]` || jsonText(registration["disabled_fields"]) != `["School","Teacher"]` {
		t.Errorf("config_data: %v", configData)
	}
}

// py: tests/test_registration_disabled_fields.py::test_validation_rejects_non_list_disabled_fields
func TestValidationRejectsNonListDisabledFields(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := ts.AdminCookies(t, "test123")
	res := ts.Put(t, "/api/admin/config", map[string]any{"data": map[string]any{
		"registration": map[string]any{"disabled_fields": "not_a_list"},
	}}, testutil.WithCookies(cookies...))
	paSkipWithoutConfigSave(t, res)
	data := res.JSON(t)
	text, _ := data["error"].(string)
	errors, _ := data["errors"].([]any)
	found := false
	for _, item := range errors {
		if s, _ := item.(string); strings.Contains(s, "disabled_fields") {
			found = true
		}
	}
	if res.Status != 400 || !strings.Contains(strings.ToLower(text), "validation failed") || !found {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_disabled_fields.py::test_validation_rejects_non_string_disabled_fields
func TestValidationRejectsNonStringDisabledFields(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := ts.AdminCookies(t, "test123")
	res := ts.Put(t, "/api/admin/config", map[string]any{"data": map[string]any{
		"registration": map[string]any{"disabled_fields": []any{"ok", 42}},
	}}, testutil.WithCookies(cookies...))
	paSkipWithoutConfigSave(t, res)
	text, _ := res.JSON(t)["error"].(string)
	if res.Status != 400 || !strings.Contains(strings.ToLower(text), "validation failed") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// The Python page was checked for a data-field-name="school" row; the Go
// page has no such HTML, so the boot data stands in: registration_fields
// lists Grade and not School, and "school" is nowhere in the page.
// py: tests/test_registration_disabled_fields.py::test_disabled_field_omitted_from_registration_html
func TestDisabledFieldOmittedFromRegistrationHTML(t *testing.T) {
	t.Parallel()
	ts := paDisabledFieldsServer(t, map[string]any{"fields": []any{"Grade"}, "disabled_fields": []any{"School"}})
	res := ts.GetJSON(t, "/")
	if res.Status != 200 {
		t.Fatalf("%d", res.Status)
	}
	html := res.Text()
	fields, _ := pageBoot(t, html)["registration_fields"].([]any)
	if len(fields) != 1 || !strings.Contains(jsonText(fields[0]), `"Grade"`) {
		t.Errorf("registration_fields: %v", fields)
	}
	if regexp.MustCompile(`(?i)school`).MatchString(html) {
		t.Errorf("the disabled field appears in the page")
	}
}
