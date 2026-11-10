package server

import (
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// Every text that dumpYAML writes must load back as the same text with
// pyval.LoadYAML (PyYAML rules), as a value, in a list and as a key.
func TestDumpYAMLRoundTripsEdgeStrings(t *testing.T) {
	t.Parallel()
	texts := []string{
		"=", "<<", "\nabc", "\n", "\n\n", "abc\n", "abc\n\n", " ", "   ", "\t", " lead", "trail ", "\tlead", "trail\t",
		"yes", "no", "on", "off", "Yes", "NO", "null", "Null", "~", "true", "False", "y", "n",
		"1", "1.0", "0x1f", "0o17", "017", "1e3", "1_000", ".inf", "-.inf", ".nan", "+1", "-1", "1:30", "12:30", "190:20:30",
		"2024-01-15", "2024-01-15 10:20:30", "2024-01-15T10:20:30Z",
		"a\r\nb", "a\rb", "a\r\n", "Привіт\nсвіт\nдруга лінія", "Скільки буде 2 + 2?", "line1\n  indented\nline3",
		"#", "# comment", "a #b", "a: b", "a:b", ": x", "- x", "-", "- ", "? x", "!", "!tag x", "&a", "*a", "|", ">", "| x", "> x",
		"%", "%YAML 1.1", "@", "@x", "`", "`x", "[a]", "{a}", "[", "]", "{", "}", ",", "a, b", "'", "\"", "'a'", "\"a\"",
		"---", "...", "--- x", "a\x00b"[:1], "tab\there", "bell\a", "é", "日本語", "😀", " ", " ", "\u0085",
		"", "null ", " null", "a\n\nb", "\n a", " \na", "a \nb", "a\n b ",
		"x == 'y'\nassert answer == 1\n", "def f():\n    return 1\n\nf()",
	}
	for _, text := range texts {
		for name, value := range map[string]any{
			"value": map[string]any{"k": text},
			"list":  []any{text, "other"},
			"key":   map[string]any{text: "v"},
			"deep":  map[string]any{"questions": []any{map[string]any{"options": []any{text, "b"}}}},
		} {
			out, err := dumpYAML(value)
			if err != nil {
				t.Errorf("%q (%s): %v", text, name, err)
				continue
			}
			back, err := pyval.LoadYAML(out)
			if err != nil || !yamlSame(value, back) {
				t.Errorf("%q (%s) written as\n%s\nloads as %#v (%v)", text, name, out, back, err)
			}
		}
	}
}

// The check also holds one text at a time, so a bad text does not force
// quotes on the others.
func TestDumpYAMLKeepsPlainTextPlain(t *testing.T) {
	t.Parallel()
	out, err := dumpYAML(map[string]any{"title": "Hello", "bad": "="})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "bad: \"=\"\ntitle: Hello\n" {
		t.Errorf("%q", out)
	}
}
