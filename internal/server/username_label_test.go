package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// The Python tests looked for "Label:" in the page text. The Go page gets the
// label from the boot data (username_label), so the port checks that value;
// the label text alone is what the page shows before the colon.
func paUsernameLabel(t *testing.T, config map[string]any) (string, string) {
	t.Helper()
	ts := testutil.StartServer(t, testutil.Options{Config: config})
	res := ts.GetJSON(t, "/")
	if res.Status != 200 {
		t.Fatalf("GET /: %d", res.Status)
	}
	label, _ := pageBoot(t, res.Text())["username_label"].(string)
	return label, res.Text()
}

// py: tests/test_username_label.py::test_default_username_label
func TestDefaultUsernameLabel(t *testing.T) {
	t.Parallel()
	if label, _ := paUsernameLabel(t, nil); label != "Ім'я користувача" {
		t.Errorf("label %q", label)
	}
}

// py: tests/test_username_label.py::test_custom_username_label_english
func TestCustomUsernameLabelEnglish(t *testing.T) {
	t.Parallel()
	label, html := paUsernameLabel(t, map[string]any{"registration": map[string]any{"username_label": "Student Name"}})
	if label != "Student Name" || strings.Contains(html, "Ім'я користувача:") {
		t.Errorf("label %q, or the default label is in the page", label)
	}
}

// py: tests/test_username_label.py::test_custom_username_label_ukrainian
func TestCustomUsernameLabelUkrainian(t *testing.T) {
	t.Parallel()
	if label, _ := paUsernameLabel(t, map[string]any{"registration": map[string]any{"username_label": "Логін учня"}}); label != "Логін учня" {
		t.Errorf("label %q", label)
	}
}

// py: tests/test_username_label.py::test_default_username_label_follows_language_en
func TestDefaultUsernameLabelFollowsLanguageEn(t *testing.T) {
	t.Parallel()
	label, html := paUsernameLabel(t, map[string]any{"language": "en"})
	if label != "Username" || strings.Contains(html, "Ім'я користувача:") {
		t.Errorf("label %q, or the Ukrainian label is in the page", label)
	}
}

// py: tests/test_username_label.py::test_custom_username_label_overrides_language
func TestCustomUsernameLabelOverridesLanguage(t *testing.T) {
	t.Parallel()
	label, html := paUsernameLabel(t, map[string]any{"language": "en", "registration": map[string]any{"username_label": "Логін учня"}})
	if label != "Логін учня" || strings.Contains(html, "Username:") {
		t.Errorf("label %q, or the language label is in the page", label)
	}
}
