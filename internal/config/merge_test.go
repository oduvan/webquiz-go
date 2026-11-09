package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// teacherConfig is a webquiz.yaml as a teacher left it with WebQuiz 1.19:
// the example file with registration switched on, a language line and
// Ukrainian text, comments and blank lines everywhere.
const teacherConfig = `# WebQuiz Server Configuration
# Copy this file to server_config.yaml and customize as needed

server:
  host: "0.0.0.0"  # Listen on all interfaces (use "127.0.0.1" for localhost only)
  port: 8080       # Server port

paths:
  quizzes_dir: "quizzes"  # Directory containing quiz YAML files
  logs_dir: "logs"        # Directory for server log files (auto-created)

admin:
  master_key: a1        # Set to enable admin interface (e.g., "your-secret-key")
                          # Can also use WEBQUIZ_MASTER_KEY environment variable
  # trusted_ips:        # List of IP addresses allowed to access admin interface
  #   - "127.0.0.1"     # Default: only localhost (127.0.0.1)

# Optional: Registration configuration
# Uncomment and configure to collect additional user information
registration:
  approve: false                     # Set to true to require admin approval before users can start
  username_label: "Ім'я користувача" # Label for username field
  fields:                            # Additional registration fields (supports Cyrillic)
    - Клас
    - Школа

# Мова інтерфейсу
language: uk   # uk або en

# Usage examples:
# webquiz --config server_config.yaml
`

// formUpdate is what the config form of the files page sends.
func formUpdate() map[string]any {
	return map[string]any{
		"language":                     "en",
		"extra_answers_with_users_csv": true,
		"registration": map[string]any{
			"approve":         true,
			"username_label":  "Учень",
			"fields":          []any{"Клас"},
			"disabled_fields": []any{"Школа"},
		},
	}
}

// commentAndBlankLines returns the lines that are empty or only a comment.
func commentAndBlankLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

// checkInOrder fails when the lines are not all in text, in this order.
func checkInOrder(t *testing.T, text string, lines []string) {
	t.Helper()
	outLines := strings.Split(text, "\n")
	next := 0
	for _, line := range lines {
		found := false
		for next < len(outLines) {
			next++
			if outLines[next-1] == line {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("line %q is missing (or out of order) in:\n%s", line, text)
		}
	}
}

func mustMerge(t *testing.T, original string, partial map[string]any) string {
	t.Helper()
	out, err := MergeYAML([]byte(original), partial)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func parse(t *testing.T, text string) map[string]any {
	t.Helper()
	v, err := pyval.LoadYAML([]byte(text))
	if err != nil {
		t.Fatalf("the merged config does not read back: %v\n%s", err, text)
	}
	m, _ := v.(map[string]any)
	return m
}

func TestPartialUpdateKeepsComments(t *testing.T) {
	out := mustMerge(t, teacherConfig, formUpdate())

	// Every comment and every blank line is still there, in the same order.
	checkInOrder(t, out, commentAndBlankLines(teacherConfig))
	// Lines the update does not touch are kept as they were, quotes and spacing included.
	for _, line := range []string{
		`  host: "0.0.0.0"  # Listen on all interfaces (use "127.0.0.1" for localhost only)`,
		`  port: 8080       # Server port`,
		`  master_key: a1        # Set to enable admin interface (e.g., "your-secret-key")`,
		`                          # Can also use WEBQUIZ_MASTER_KEY environment variable`,
	} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("line %q changed or is missing:\n%s", line, out)
		}
	}
	// Changed values keep their end-of-line comment at the same column.
	for _, line := range []string{
		`  approve: true                      # Set to true to require admin approval before users can start`,
		`  username_label: "Учень"            # Label for username field`,
		`language: en   # uk або en`,
	} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("line %q is missing:\n%s", line, out)
		}
	}
	// The new section key goes right after the section, before the blank line.
	if !strings.Contains(out, "    - Клас\n  disabled_fields:\n    - Школа\n\n# Мова інтерфейсу\n") {
		t.Errorf("disabled_fields is not right after the registration section:\n%s", out)
	}

	got := parse(t, out)
	if got["language"] != "en" || got["extra_answers_with_users_csv"] != true {
		t.Errorf("language/extra: %v", got)
	}
	want := map[string]any{"approve": true, "username_label": "Учень", "fields": []any{"Клас"}, "disabled_fields": []any{"Школа"}}
	if !reflect.DeepEqual(got["registration"], want) {
		t.Errorf("registration = %#v, want %#v", got["registration"], want)
	}
	if server := got["server"].(map[string]any); server["port"] != 8080 || server["host"] != "0.0.0.0" {
		t.Errorf("server = %v", server)
	}
	if admin := got["admin"].(map[string]any); admin["master_key"] != "a1" {
		t.Errorf("admin = %v", admin)
	}

	// A second save from the form changes the values back and keeps everything else.
	second := mustMerge(t, out, map[string]any{
		"language":                     "uk",
		"extra_answers_with_users_csv": false,
		"registration":                 map[string]any{"approve": false, "username_label": "", "fields": []any{"Клас", "Школа"}, "disabled_fields": []any{}},
	})
	checkInOrder(t, second, commentAndBlankLines(teacherConfig))
	if !strings.Contains(second, "language: uk   # uk або en\n") {
		t.Errorf("language line:\n%s", second)
	}
	got = parse(t, second)
	want = map[string]any{"approve": false, "username_label": "", "fields": []any{"Клас", "Школа"}, "disabled_fields": []any{}}
	if !reflect.DeepEqual(got["registration"], want) {
		t.Errorf("registration = %#v, want %#v", got["registration"], want)
	}
}

func TestPartialUpdateOfTheExampleFile(t *testing.T) {
	// A fresh 1.19 install has the example file, where registration is a comment.
	out := mustMerge(t, ExampleConfig, formUpdate())
	if !strings.HasPrefix(out, ExampleConfig) {
		t.Errorf("the example text should stay as it is, with the new keys after it:\n%s", out)
	}
	got := parse(t, out)
	if got["language"] != "en" || got["registration"].(map[string]any)["approve"] != true {
		t.Errorf("merged values: %v", got)
	}
}

func TestPartialUpdateKeepsQuoteStyleAndLineEnds(t *testing.T) {
	out := mustMerge(t, "language: 'uk'\nextra_answers_with_users_csv: false\n", map[string]any{"language": "en"})
	if out != "language: 'en'\nextra_answers_with_users_csv: false\n" {
		t.Errorf("single quotes should stay, like ruamel's preserve_quotes:\n%q", out)
	}
	out = mustMerge(t, "language: uk\r\nregistration:\r\n  approve: false\r\n", map[string]any{"registration": map[string]any{"approve": true}})
	if out != "language: uk\r\nregistration:\r\n  approve: true\r\n" {
		t.Errorf("Windows line ends should stay:\n%q", out)
	}
	out = mustMerge(t, "language: uk", map[string]any{"server": map[string]any{"port": json.Number("9000")}})
	if out != "language: uk\nserver:\n  port: 9000\n" {
		t.Errorf("a file without a final line end:\n%q", out)
	}
	// A file with only comments keeps them (ruamel dropped them; nothing else differs).
	out = mustMerge(t, "# my notes\n\n", map[string]any{"language": "en"})
	if out != "# my notes\n\nlanguage: en\n" {
		t.Errorf("comments of an empty config:\n%q", out)
	}
}

func TestPartialUpdateWritesPythonReadableValues(t *testing.T) {
	out := mustMerge(t, "", map[string]any{"registration": map[string]any{
		"fields": []any{"yes", "on", "1:30", "1.0", "true"},
		"weight": json.Number("1.0"),
		"count":  json.Number("3"),
		"big":    json.Number("1e3"),
	}})
	got := parse(t, out)["registration"].(map[string]any)
	if !reflect.DeepEqual(got["fields"], []any{"yes", "on", "1:30", "1.0", "true"}) {
		t.Errorf("text that PyYAML would read as another type must be quoted:\n%s", out)
	}
	if f, ok := got["weight"].(float64); !ok || f != 1 {
		t.Errorf("1.0 must stay a float: %#v\n%s", got["weight"], out)
	}
	if got["count"] != 3 {
		t.Errorf("3 must stay an int: %#v", got["count"])
	}
	if f, ok := got["big"].(float64); !ok || f != 1000 {
		t.Errorf("1e3 from JSON is the float 1000.0: %#v\n%s", got["big"], out)
	}
}

// mergeKnownDifferences lists fixture inputs where Go reads differently on purpose.
var mergeKnownDifferences = map[string]string{
	// ruamel refuses repeated keys, so Python replaced the whole file with
	// the update and lost the rest of the config. Go keeps the file and, like
	// PyYAML, uses the last value of the repeated key.
	"registration:\n  approve: false\nregistration:\n  approve: true\n": "repeated keys",
}

func TestMergeYAMLMatchesPython(t *testing.T) {
	for _, c := range readConfigCases(t).Merge {
		data, ok := decodeJSON(t, c.Data).(map[string]any)
		if !ok {
			t.Fatalf("bad case %s", c.Data)
		}
		out, err := MergeYAML([]byte(c.Original), data)
		if err != nil {
			t.Errorf("MergeYAML(%q, %s): %v", c.Original, c.Data, err)
			continue
		}
		got, err := pyval.LoadYAML(out)
		if err != nil {
			t.Errorf("MergeYAML(%q, %s) wrote YAML that does not read back: %v\n%s", c.Original, c.Data, err, out)
			continue
		}
		if reason, ok := mergeKnownDifferences[c.Original]; ok {
			t.Logf("known difference (%s): Go wrote\n%s", reason, out)
			continue
		}
		want := decodeJSON(t, c.Parsed)
		if !pyval.Equal(got, want) {
			t.Errorf("MergeYAML(%q, %s)\nGo wrote:\n%s\nreads as %v\nruamel wrote:\n%s\nreads as %v", c.Original, c.Data, out, got, c.RuamelOutput, want)
		}
	}
}

func TestMergeYAMLRepeatedKeysKeepTheFile(t *testing.T) {
	out := mustMerge(t, "registration:\n  approve: false\nregistration:\n  approve: true\n", map[string]any{
		"registration": map[string]any{"approve": false},
		"language":     "en",
	})
	got := parse(t, out)
	if got["language"] != "en" || got["registration"].(map[string]any)["approve"] != false {
		t.Errorf("the last repeated key is the one that counts:\n%s", out)
	}
}

func TestMergeYAMLFlowSection(t *testing.T) {
	out := mustMerge(t, "# top\nserver: {port: 8080}  # flow\nlanguage: uk\n", map[string]any{"server": map[string]any{"host": "1.2.3.4"}})
	if !strings.HasPrefix(out, "# top\n") || !strings.Contains(out, "language: uk\n") {
		t.Errorf("text around the section should stay:\n%s", out)
	}
	server := parse(t, out)["server"].(map[string]any)
	if server["port"] != 8080 || server["host"] != "1.2.3.4" {
		t.Errorf("server = %v\n%s", server, out)
	}
}
