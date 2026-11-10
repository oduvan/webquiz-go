package config

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

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
	// The comment of a key that gets a whole new value stays on the key line.
	out = mustMerge(t, "registration:   # off for now\nlanguage: uk\n", map[string]any{"registration": map[string]any{"approve": true}})
	if out != "registration:   # off for now\n  approve: true\nlanguage: uk\n" {
		t.Errorf("comment of an empty key:\n%q", out)
	}
	out = mustMerge(t, "registration: null  # off\nlanguage: uk\n", map[string]any{"registration": map[string]any{"approve": true}})
	if out != "registration:       # off\n  approve: true\nlanguage: uk\n" {
		t.Errorf("comment after null:\n%q", out)
	}
	out = mustMerge(t, "a:\n  fields:   # the list\n    - x\n", map[string]any{"a": map[string]any{"fields": []any{"y"}}})
	if out != "a:\n  fields:   # the list\n    - \"y\"\n" {
		t.Errorf("comment of a list key:\n%q", out)
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
		if err := CheckMerge([]byte(c.Original), data, out); err != nil {
			t.Errorf("MergeYAML(%q, %s): the save's check refuses the result: %v\n%s", c.Original, c.Data, err, out)
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

// lastSectionConfig has the shape of the config that the classroom harness
// saved from the form: registration is the last section and has only some
// of the form's keys, and the file has no language line.
const lastSectionConfig = `# WebQuiz config

server:
  port: 8199   # the port

admin:
  master_key: harness
  trusted_ips: []

# Registration form
registration:
  # extra fields of the form
  fields:
    - Клас
    - Школа
`

// The form adds keys to the last section and new top-level keys at the same
// time. The new section keys must stay inside the section: before the fix,
// "language: en" went between the section and its new keys, and the text did
// not read back ("mapping values are not allowed in this context").
func TestMergeYAMLLastSectionGetsNewKeys(t *testing.T) {
	update := map[string]any{
		"language":                     "en",
		"extra_answers_with_users_csv": false,
		"registration": map[string]any{
			"approve":         false,
			"username_label":  "",
			"fields":          []any{"Клас"},
			"disabled_fields": []any{"Школа"},
		},
	}
	want := map[string]any{
		"server":                       map[string]any{"port": 8199},
		"admin":                        map[string]any{"master_key": "harness", "trusted_ips": []any{}},
		"language":                     "en",
		"extra_answers_with_users_csv": false,
		"registration": map[string]any{
			"approve":         false,
			"username_label":  "",
			"fields":          []any{"Клас"},
			"disabled_fields": []any{"Школа"},
		},
	}
	for _, c := range []struct{ name, original string }{
		{"as the harness had it", lastSectionConfig},
		{"no final line end", strings.TrimSuffix(lastSectionConfig, "\n")},
		{"Windows line ends", strings.ReplaceAll(lastSectionConfig, "\n", "\r\n")},
		{"a byte order mark (Notepad)", "\xef\xbb\xbf" + lastSectionConfig},
		{"document markers", "---\n" + lastSectionConfig + "...\n"},
		{"document markers with comments", "--- # start\n" + lastSectionConfig + "... # the end\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := mustMerge(t, c.original, update)
			checkReadsAs(t, out, want)
			checkInOrder(t, out, commentAndBlankLines(c.original))
			// The new keys of the section come right after its last line,
			// and the new top-level keys after them.
			tail := "    - Клас\n  approve: false\n  disabled_fields:\n    - Школа\n  username_label: \"\"\nextra_answers_with_users_csv: false\nlanguage: en\n"
			if text := strings.ReplaceAll(out, "\r\n", "\n"); !strings.Contains(text, tail) {
				t.Errorf("the new keys are not where they belong; want %q in:\n%s", tail, out)
			}
		})
	}
}

// matrixLine is one line of a generated config file. key tells what the
// line belongs to: "" for a comment or a blank line, "name" for a top-level
// key line, "name.child" for the lines of a key inside a section.
type matrixLine struct{ text, key string }

// matrixSection is one top-level key of a generated config file.
type matrixSection struct {
	name  string
	lines []matrixLine
	value any
	block bool // a block mapping: an update's keys are merged into it
}

// maybeComment adds an end-of-line comment when inside is true.
func maybeComment(text string, inside bool, comment string) string {
	if inside {
		return text + "   # " + comment
	}
	return text
}

func matrixServer(inside bool) matrixSection {
	lines := []matrixLine{{"server:", "server"}}
	if inside {
		lines = append(lines, matrixLine{"  # where the server listens", ""})
	}
	lines = append(lines, matrixLine{maybeComment("  port: 8080", inside, "the port"), "server.port"})
	return matrixSection{"server", lines, map[string]any{"port": 8080}, true}
}

func matrixLanguage(value string, inside bool) matrixSection {
	return matrixSection{"language", []matrixLine{{maybeComment("language: "+value, inside, "uk or en"), "language"}}, value, false}
}

func matrixAdmin(inside bool) matrixSection {
	lines := []matrixLine{{"admin:", "admin"}, {"  master_key: secret", "admin.master_key"}}
	if inside {
		lines = append(lines, matrixLine{"  # who may open the admin pages", ""})
	}
	lines = append(lines, matrixLine{"  trusted_ips: []", "admin.trusted_ips"})
	return matrixSection{"admin", lines, map[string]any{"master_key": "secret", "trusted_ips": []any{}}, true}
}

func matrixExtra(inside bool) matrixSection {
	return matrixSection{"extra_answers_with_users_csv",
		[]matrixLine{{maybeComment("extra_answers_with_users_csv: false", inside, "a third CSV file"), "extra_answers_with_users_csv"}}, false, false}
}

func matrixTunnel(inside bool) matrixSection {
	lines := []matrixLine{{"tunnel:", "tunnel"}}
	if inside {
		lines = append(lines, matrixLine{"  # the tunnel server", ""})
	}
	lines = append(lines, matrixLine{"  server: t.example.com", "tunnel.server"})
	return matrixSection{"tunnel", lines, map[string]any{"server": "t.example.com"}, true}
}

// matrixRegistration is the registration section with none of the form's
// keys (null or {}), some of them, or all of them.
func matrixRegistration(keys string, inside bool) matrixSection {
	fields := []matrixLine{
		{maybeComment("  fields:", inside, "one per line"), "registration.fields"},
		{"    - Клас", "registration.fields"},
		{"    - Школа", "registration.fields"},
	}
	if inside {
		fields = append([]matrixLine{{"  # extra fields of the form", ""}}, fields...)
	}
	switch keys {
	case "null":
		return matrixSection{"registration", []matrixLine{{maybeComment("registration:", inside, "off for now"), "registration"}}, nil, false}
	case "empty":
		return matrixSection{"registration", []matrixLine{{maybeComment("registration: {}", inside, "nothing yet"), "registration"}}, map[string]any{}, false}
	case "some":
		lines := append([]matrixLine{{"registration:", "registration"}}, fields...)
		return matrixSection{"registration", lines, map[string]any{"fields": []any{"Клас", "Школа"}}, true}
	}
	lines := []matrixLine{
		{"registration:", "registration"},
		{maybeComment("  approve: false", inside, "need approval?"), "registration.approve"},
		{`  username_label: "Ім'я"`, "registration.username_label"},
	}
	lines = append(lines, fields...)
	lines = append(lines, matrixLine{"  disabled_fields: []", "registration.disabled_fields"})
	value := map[string]any{"approve": false, "username_label": "Ім'я", "fields": []any{"Клас", "Школа"}, "disabled_fields": []any{}}
	return matrixSection{"registration", lines, value, true}
}

// matrixFile is a generated config file and the values it holds.
type matrixFile struct {
	name    string
	lines   []matrixLine
	values  map[string]any
	block   map[string]bool
	newline bool // the text ends with a line end
}

func (f matrixFile) text() string {
	texts := make([]string, len(f.lines))
	for i, line := range f.lines {
		texts[i] = line.text
	}
	text := strings.Join(texts, "\n")
	if f.newline {
		text += "\n"
	}
	return text
}

// matrixFiles makes config files of every combination: registration first,
// last or missing; registration with none, some or all of the form's keys;
// language the same as the update, changed or missing; with or without
// extra_answers_with_users_csv; comments and blank lines nowhere, between
// sections, inside sections, at the end, or everywhere; with or without a
// final line end.
func matrixFiles() []matrixFile {
	var files []matrixFile
	for _, order := range []string{"first", "last", "missing"} {
		registrations := []string{"null", "empty", "some", "all"}
		if order == "missing" {
			registrations = []string{"-"}
		}
		for _, registration := range registrations {
			for _, language := range []string{"same", "changed", "missing"} {
				for _, extra := range []bool{true, false} {
					for _, comments := range []string{"none", "between", "inside", "end", "all"} {
						for _, newline := range []bool{true, false} {
							between := comments == "between" || comments == "all"
							inside := comments == "inside" || comments == "all"
							end := comments == "end" || comments == "all"

							sections := []matrixSection{matrixServer(inside)}
							switch language {
							case "same":
								sections = append(sections, matrixLanguage("en", inside))
							case "changed":
								sections = append(sections, matrixLanguage("uk", inside))
							}
							sections = append(sections, matrixAdmin(inside))
							if extra {
								sections = append(sections, matrixExtra(inside))
							}
							sections = append(sections, matrixTunnel(inside))
							switch order {
							case "first":
								sections = append([]matrixSection{matrixRegistration(registration, inside)}, sections...)
							case "last":
								sections = append(sections, matrixRegistration(registration, inside))
							}

							f := matrixFile{
								name: fmt.Sprintf("registration_%s_%s/language_%s/extra_%v/comments_%s/newline_%v",
									order, registration, language, extra, comments, newline),
								values:  map[string]any{},
								block:   map[string]bool{},
								newline: newline,
							}
							if between {
								f.lines = append(f.lines, matrixLine{"# WebQuiz config", ""}, matrixLine{"", ""})
							}
							for i, section := range sections {
								if between && i > 0 {
									f.lines = append(f.lines, matrixLine{"", ""}, matrixLine{"# " + section.name + " settings", ""})
								}
								f.lines = append(f.lines, section.lines...)
								f.values[section.name] = section.value
								f.block[section.name] = section.block
							}
							if end {
								last := sections[len(sections)-1].name
								f.lines = append(f.lines, matrixLine{"  # more " + last + " settings later", ""},
									matrixLine{"", ""}, matrixLine{"# end of the config", ""})
							}
							files = append(files, f)
						}
					}
				}
			}
		}
	}
	return files
}

// pythonMerge is the merge loop of admin_update_config in 1.19: a dict
// update goes into a dict section key by key, any other value replaces the
// old one.
func pythonMerge(values, update map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		if section, ok := value.(map[string]any); ok {
			copied := map[string]any{}
			for k, v := range section {
				copied[k] = v
			}
			value = copied
		}
		out[key] = value
	}
	for key, value := range update {
		if updates, ok := value.(map[string]any); ok {
			if section, ok := out[key].(map[string]any); ok {
				for k, v := range updates {
					section[k] = v
				}
				continue
			}
		}
		out[key] = value
	}
	return out
}

// keptLines lists the lines of f that a merge of update must leave as they
// are: comments, blank lines, the keys the update does not name, and the
// first line of a block section that the update's keys go into.
func (f matrixFile) keptLines(update map[string]any) []string {
	var kept []string
	for _, line := range f.lines {
		name, child, isChild := strings.Cut(line.key, ".")
		value, named := update[name]
		updates, isDict := value.(map[string]any)
		keep := line.key == "" || !named
		if named && isDict && f.block[name] {
			if !isChild {
				keep = true
			} else if _, changed := updates[child]; !changed {
				keep = true
			}
		}
		if keep {
			kept = append(kept, line.text)
		}
	}
	if f.newline {
		kept = append(kept, "") // the text after the final line end
	}
	return kept
}

// comments lists the comments of the lines (from "#" to the line end). A
// "#" starts a comment at the line start or after a space, outside quotes.
func comments(lines []string) []string {
	var found []string
	for _, line := range lines {
		var quote byte
		for i := 0; i < len(line); i++ {
			c := line[i]
			if quote != 0 {
				if c == '\\' && quote == '"' {
					i++
				} else if c == quote {
					quote = 0
				}
				continue
			}
			before := strings.TrimRight(line[:i], " \t")
			if (c == '\'' || c == '"') && (before == "" || strings.ContainsAny(before[len(before)-1:], ":[{,-")) {
				quote = c
			}
			if c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
				found = append(found, line[i:])
				break
			}
		}
	}
	return found
}

// checkCommentsInOrder fails when a comment of the original, also one at the
// end of a changed line, is missing from the merged text or out of order.
func checkCommentsInOrder(t *testing.T, merged, original string) {
	t.Helper()
	rest := merged
	for _, comment := range comments(strings.Split(original, "\n")) {
		i := strings.Index(rest, comment)
		if i < 0 {
			t.Fatalf("the comment %q is missing (or out of order) in:\n%s", comment, merged)
		}
		rest = rest[i+len(comment):]
	}
}

// The form matrix: the form's update, and an update that the API can send,
// go into config files of many shapes (matrixFiles). The new text must read
// back as exactly the old values with the update in them, keep every comment,
// blank line and untouched line, and not change when the same update comes
// again.
func TestMergeYAMLFormMatrix(t *testing.T) {
	updates := []struct{ name, data string }{
		{"form", `{"language": "en", "extra_answers_with_users_csv": true,
			"registration": {"approve": true, "username_label": "Учень", "fields": ["Клас"], "disabled_fields": ["Школа"]}}`},
		{"api", `{"language": "en", "admin": {"trusted_ips": ["10.0.0.5"]}, "tunnel": {"socket_name": "room-1"},
			"quizzes": [{"name": "Q", "download_path": "https://e.com/q.zip", "folder": "q/"}]}`},
	}
	files := matrixFiles()
	if len(files) != 540 {
		t.Fatalf("%d files, want 540", len(files))
	}
	for _, u := range updates {
		for _, f := range files {
			t.Run(u.name+"/"+f.name, func(t *testing.T) {
				t.Parallel()
				update := decodeJSON(t, []byte(u.data)).(map[string]any)
				original := f.text()
				out := mustMerge(t, original, update)
				checkReadsAs(t, out, pythonMerge(f.values, update))
				checkInOrder(t, out, f.keptLines(update))
				checkCommentsInOrder(t, out, original)
				if again := mustMerge(t, out, update); again != out {
					t.Fatalf("the same update changed the text again:\n%s\nthen:\n%s", out, again)
				}
				if err := CheckMerge([]byte(original), update, []byte(out)); err != nil {
					t.Fatalf("the save's check refuses a right merge: %v\n%s", err, out)
				}
			})
		}
	}
}

// Shapes of config files where the merge used to write broken YAML, change
// a value it should not touch, or lose a comment. Each one must read back as
// the old values with the update in them, keep its comments, and not change
// when the same update comes again.
func TestMergeYAMLEdgeShapes(t *testing.T) {
	languageAndApprove := map[string]any{"language": "en", "registration": map[string]any{"approve": true}}
	for _, c := range []struct {
		name     string
		original string
		update   map[string]any
		want     map[string]any
	}{
		{
			name:     "the whole file is indented",
			original: "# top\n  server:\n    port: 8080\n  registration:\n    fields:\n      - A\n",
			update:   languageAndApprove,
			want: map[string]any{"server": map[string]any{"port": 8080}, "language": "en",
				"registration": map[string]any{"fields": []any{"A"}, "approve": true}},
		},
		{
			name:     "a document end marker without a final line end",
			original: "registration:\n  fields:\n    - A\n...",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"fields": []any{"A"}, "approve": true}},
		},
		{
			name:     "a comment after the document end marker",
			original: "registration:\n  fields:\n    - A\n...\n# notes after the document\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"fields": []any{"A"}, "approve": true}},
		},
		{
			name:     "a byte order mark before a changed line with a comment",
			original: "\xef\xbb\xbflanguage: uk   # uk or en\nregistration:\n  approve: false\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"approve": true}},
		},
		{
			name:     "a block scalar that keeps its final blank lines ends the section",
			original: "registration:\n  username_label: |+\n    Name\n\n\nlanguage: uk\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"username_label": "Name\n\n\n", "approve": true}},
		},
		{
			name:     "a block scalar that keeps its final blank lines ends the file",
			original: "registration:\n  username_label: |+\n    Name\n\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"username_label": "Name\n\n", "approve": true}},
		},
		{
			name:     "a block scalar with a blank line after it ends the file",
			original: "registration:\n  username_label: |\n    Name\n\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"username_label": "Name\n", "approve": true}},
		},
		{
			name:     "a block scalar with lines that start with # ends the section",
			original: "registration:\n  username_label: |\n    Name\n    # part of the label\nlanguage: uk\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"username_label": "Name\n# part of the label\n", "approve": true}},
		},
		{
			name:     "a block scalar ends the file without a final line end",
			original: "registration:\n  username_label: |\n    Name\n    Surname",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"username_label": "Name\nSurname", "approve": true}},
		},
		{
			name:     "a folded block scalar that keeps its line ends ends the file without a final line end",
			original: "registration:\n  username_label: >+\n    Name",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"username_label": "Name", "approve": true}},
		},
		{
			name:     "a block scalar with a comment after its header gets a new value",
			original: "registration:\n  username_label: |   # two lines\n    Name\n    Surname\nlanguage: uk\n",
			update:   map[string]any{"registration": map[string]any{"username_label": "Учень"}},
			want:     map[string]any{"language": "uk", "registration": map[string]any{"username_label": "Учень"}},
		},
		{
			name:     "a new multi-line value keeps the comment of the key",
			original: "registration:\n  username_label: Name   # the label\nlanguage: uk\n",
			update:   map[string]any{"registration": map[string]any{"username_label": "Name\nSurname\n"}},
			want:     map[string]any{"language": "uk", "registration": map[string]any{"username_label": "Name\nSurname\n"}},
		},
		{
			// Written as a block scalar ("|"), the new text would take in the
			// comment line after it.
			name:     "a new multi-line value before an indented comment",
			original: "registration:\n  username_label: Name\n    # the label above\nlanguage: uk\n",
			update:   map[string]any{"registration": map[string]any{"username_label": "Name\nSurname"}},
			want:     map[string]any{"language": "uk", "registration": map[string]any{"username_label": "Name\nSurname"}},
		},
		{
			name:     "a new multi-line top-level value before an indented comment",
			original: "language: uk\n  # uk or en\nregistration:\n  approve: false\n",
			update:   map[string]any{"language": "en\nuk\n"},
			want:     map[string]any{"language": "en\nuk\n", "registration": map[string]any{"approve": false}},
		},
		{
			name:     "a flow list with a comment gets a new value",
			original: "registration:\n  fields: [A, B]   # one per line\nlanguage: uk\n",
			update:   map[string]any{"registration": map[string]any{"fields": []any{"X"}}},
			want:     map[string]any{"language": "uk", "registration": map[string]any{"fields": []any{"X"}}},
		},
		{
			name:     "a flow section with a comment gets new keys",
			original: "registration: {approve: false, note: 'a # b'}   # the form\nlanguage: uk\n",
			update:   map[string]any{"registration": map[string]any{"fields": []any{"X"}}},
			want: map[string]any{"language": "uk",
				"registration": map[string]any{"approve": false, "note": "a # b", "fields": []any{"X"}}},
		},
		{
			name:     "the last section ends with a nested section",
			original: "tunnel:\n  server: t.example.com\n  config:\n    username: u\n    base_url: https://t.example.com/t\n",
			update:   map[string]any{"language": "en", "tunnel": map[string]any{"socket_name": "room-1"}},
			want: map[string]any{"language": "en", "tunnel": map[string]any{"server": "t.example.com", "socket_name": "room-1",
				"config": map[string]any{"username": "u", "base_url": "https://t.example.com/t"}}},
		},
		{
			name:     "the last section ends with a list at the key's indentation",
			original: "registration:\n  fields:\n  - A\n  - B\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"fields": []any{"A", "B"}, "approve": true}},
		},
		{
			name:     "the last section is indented by four spaces",
			original: "registration:\n    fields:\n        - A\n",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"fields": []any{"A"}, "approve": true}},
		},
		{
			name:     "a comment at the line start ends the file without a line end",
			original: "registration:\n  fields:\n    - A\n# the end",
			update:   languageAndApprove,
			want:     map[string]any{"language": "en", "registration": map[string]any{"fields": []any{"A"}, "approve": true}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := mustMerge(t, c.original, c.update)
			checkReadsAs(t, out, c.want)
			checkInOrder(t, out, commentAndBlankLines(strings.TrimPrefix(c.original, "\xef\xbb\xbf")))
			checkCommentsInOrder(t, out, c.original)
			if again := mustMerge(t, out, c.update); again != out {
				t.Fatalf("the same update changed the text again:\n%s\nthen:\n%s", out, again)
			}
			if err := CheckMerge([]byte(c.original), c.update, []byte(out)); err != nil {
				t.Fatalf("the save's check refuses a right merge: %v\n%s", err, out)
			}
		})
	}
}

// Checker code often ends with a comment line. Inside a block scalar such a
// line is part of the text: a new value must replace it with the rest of the
// old text, and keys after the code must not cut it.
func TestMergeYAMLBlockScalarLinesThatLookLikeComments(t *testing.T) {
	const original = "checker_templates:\n  - name: Int\n    code: |\n      assert int(user_answer) == 5\n      # the end of the check\n" +
		"registration:\n  username_label: |\n    Name\n    # part of the label\n"
	update := map[string]any{
		"language":          "en",
		"checker_templates": []any{map[string]any{"name": "New", "code": "assert True\nassert 1"}},
		"registration":      map[string]any{"approve": true},
	}
	out := mustMerge(t, original, update)
	checkReadsAs(t, out, map[string]any{
		"language":          "en",
		"checker_templates": []any{map[string]any{"name": "New", "code": "assert True\nassert 1"}},
		"registration":      map[string]any{"username_label": "Name\n# part of the label\n", "approve": true},
	})
	if strings.Contains(out, "the end of the check") {
		t.Errorf("a line of the old code is left:\n%s", out)
	}
	if err := CheckMerge([]byte(original), update, []byte(out)); err != nil {
		t.Errorf("the save's check refuses a right merge: %v\n%s", err, out)
	}
}

// CheckMerge is the last guard before a config form save writes the file:
// text that does not read back, or that reads back as other values than the
// old ones with the update in them, is refused with the reason.
func TestCheckMergeRefusesWrongText(t *testing.T) {
	original := []byte("# form\nregistration:\n  fields:\n    - Клас\n")
	update := map[string]any{"language": "en", "registration": map[string]any{"approve": false}}
	for _, c := range []struct{ name, merged, reason string }{
		{"text that does not read back (the merge before the fix)",
			"# form\nregistration:\n  fields:\n    - Клас\nlanguage: en\n  approve: false\n", "does not read back"},
		{"a new value is missing", "# form\nregistration:\n  fields:\n    - Клас\n  approve: false\n", "language would be missing"},
		{"a new value is wrong", "# form\nregistration:\n  fields:\n    - Клас\n  approve: false\nlanguage: uk\n", "language would read back as 'uk', not 'en'"},
		{"a value has another type", "# form\nregistration:\n  fields:\n    - Клас\n  approve: 0\nlanguage: en\n", "registration.approve would read back as 0, not False"},
		{"a value the update does not name changed", "# form\nregistration:\n  fields:\n    - Школа\n  approve: false\nlanguage: en\n", "registration.fields"},
		{"a section key went to the top level", "# form\nregistration:\n  fields:\n    - Клас\napprove: false\nlanguage: en\n", "approve would be added"},
		{"a key that nobody sent", "# form\nregistration:\n  fields:\n    - Клас\n  approve: false\nlanguage: en\nserver: null\n", "server would be added"},
		{"not a mapping", "- a\n", "not a mapping"},
	} {
		err := CheckMerge(original, update, []byte(c.merged))
		if err == nil || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s: error %v, want one with %q", c.name, err, c.reason)
		}
	}
	if err := CheckMerge(original, update, []byte(mustMerge(t, string(original), update))); err != nil {
		t.Errorf("the right text is refused: %v", err)
	}
}

// CheckMerge accepts what the merge rightly writes, also in files with YAML
// features that a config rarely has. It merges like 1.19: a section shared
// through an alias changes for every key that uses it.
func TestCheckMergeAcceptsRightText(t *testing.T) {
	for _, c := range []struct {
		name, original string
		update         map[string]any
	}{
		{"NaN in a section the update does not touch", "server:\n  ratio: .nan\nlanguage: uk\n",
			map[string]any{"language": "en"}},
		{"a section shared through an alias", "registration: &reg\n  approve: false\nother: *reg\n",
			map[string]any{"registration": map[string]any{"approve": true}}},
		{"a merge key in the section", "base: &b\n  approve: false\nregistration:\n  <<: *b\n  fields: [A]\n",
			map[string]any{"registration": map[string]any{"approve": true}}},
		{"repeated keys: the last one counts", "registration:\n  approve: false\nregistration:\n  approve: true\n",
			map[string]any{"registration": map[string]any{"approve": false}, "language": "en"}},
		{"a file that is not a mapping is replaced", "- a\n- b\n", map[string]any{"language": "en"}},
		{"broken text is replaced", "a: [1, 2\n", map[string]any{"language": "en"}},
		{"an empty file", "", map[string]any{"language": "en"}},
		{"a flow document", "{language: uk, registration: {approve: false}}\n",
			map[string]any{"registration": map[string]any{"fields": []any{"A"}}}},
		{"numbers keep their type", "server:\n  port: 8080\n",
			map[string]any{"server": map[string]any{"port": json.Number("9000"), "ratio": json.Number("1.0")}}},
	} {
		out := mustMerge(t, c.original, c.update)
		if err := CheckMerge([]byte(c.original), c.update, []byte(out)); err != nil {
			t.Errorf("%s: %v\n%s", c.name, err, out)
		}
	}
}

// One shape is beyond the line editor: a new value for a key whose old value
// has an anchor that an alias elsewhere uses. 1.19 kept the old value for the
// alias; the editor drops the anchor, so the alias points nowhere. The save's
// check refuses this text, and the file stays as it was. The config form
// never sends such an update: its registration is a mapping, which goes into
// the anchored section and keeps the anchor.
func TestCheckMergeRefusesALostAnchor(t *testing.T) {
	const original = "registration: &reg\n  approve: false\nother: *reg\n"
	update := map[string]any{"registration": "replaced"}
	err := CheckMerge([]byte(original), update, []byte(mustMerge(t, original, update)))
	if err == nil || !strings.Contains(err.Error(), "does not read back") {
		t.Errorf("error %v", err)
	}
	form := map[string]any{"registration": map[string]any{"approve": true}}
	out := mustMerge(t, original, form)
	checkReadsAs(t, out, map[string]any{"registration": map[string]any{"approve": true}, "other": map[string]any{"approve": true}})
	if err := CheckMerge([]byte(original), form, []byte(out)); err != nil {
		t.Errorf("the form's update is refused: %v", err)
	}
}

// When the old file cannot be read with PyYAML's rules (an unknown tag),
// there is nothing to compare with. New text that does not read back either
// is left to the config save's own check, which answers "Invalid YAML
// syntax" as 1.19 did; new text that reads back must hold the update.
func TestCheckMergeWhenTheOldFileCannotBeRead(t *testing.T) {
	original := []byte("language: !strange uk\nregistration:\n  approve: false\n")
	update := map[string]any{"registration": map[string]any{"approve": true}}
	if err := CheckMerge(original, update, []byte(mustMerge(t, string(original), update))); err != nil {
		t.Errorf("the strange tag is still there: %v", err)
	}
	update = map[string]any{"language": "en", "registration": map[string]any{"approve": true}}
	if err := CheckMerge(original, update, []byte(mustMerge(t, string(original), update))); err != nil {
		t.Errorf("the update replaced the strange tag: %v", err)
	}
	err := CheckMerge(original, update, []byte("language: en\nregistration:\n  approve: false\n"))
	if err == nil || !strings.Contains(err.Error(), "registration.approve") {
		t.Errorf("a wrong value must be refused: %v", err)
	}
}

// checkReadsAs fails when text does not read back, with PyYAML's rules and
// with yaml.v3, as exactly the values of want.
func checkReadsAs(t *testing.T, text string, want map[string]any) {
	t.Helper()
	var plain any
	if err := yaml.Unmarshal([]byte(text), &plain); err != nil {
		t.Fatalf("yaml.v3 cannot read the merged config: %v\n%s", err, text)
	}
	got := parse(t, text)
	if !sameValue(got, want) {
		t.Fatalf("the merged config reads as\n%#v\nwant\n%#v\ntext:\n%s", got, want, text)
	}
}

// sameValue wants the same values with the same types all the way down.
func TestSameValue(t *testing.T) {
	nan := math.NaN()
	for _, c := range []struct {
		a, b any
		same bool
	}{
		{"en", "en", true},
		{"en", "uk", false},
		{true, 1, false},
		{1, json.Number("1"), true},
		{1, 1.0, false},
		{json.Number("1.0"), 1.0, true},
		{nan, nan, true},
		{nil, nil, true},
		{nil, "", false},
		{[]any{"Клас"}, []any{"Клас"}, true},
		{[]any{"Клас"}, []any{"Клас", "Школа"}, false},
		{[]any{true}, []any{1}, false},
		{map[string]any{"approve": false}, map[string]any{"approve": false}, true},
		{map[string]any{"approve": false}, map[string]any{"approve": 0}, false},
		{map[string]any{"approve": false}, map[string]any{"fields": false}, false},
		{map[string]any{"a": map[string]any{"b": []any{nan}}}, map[string]any{"a": map[string]any{"b": []any{nan}}}, true},
	} {
		if got := sameValue(c.a, c.b); got != c.same {
			t.Errorf("sameValue(%#v, %#v) = %v, want %v", c.a, c.b, got, c.same)
		}
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
