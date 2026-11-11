package macos

import (
	"bytes"
	"encoding/xml"
	"os"
	"testing"
)

// The approved values of WebQuiz.app: one program "webquiz", a menu bar app
// with no Dock icon (LSUIElement), macOS 12 or newer, the version from the
// VERSION file.
func TestInfoPlist(t *testing.T) {
	data, err := os.ReadFile("Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	got := readPlist(t, data)
	want := map[string]string{
		"CFBundleName":                  "WebQuiz",
		"CFBundleDisplayName":           "WebQuiz",
		"CFBundleExecutable":            "webquiz",
		"CFBundleIdentifier":            "com.github.oduvan.webquiz",
		"CFBundleIconFile":              "WebQuiz",
		"CFBundlePackageType":           "APPL",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleShortVersionString":    "__VERSION__",
		"CFBundleVersion":               "__VERSION__",
		"LSMinimumSystemVersion":        "12.0",
		"LSUIElement":                   "true",
		"NSHighResolutionCapable":       "true",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected key %s", key)
		}
	}
}

// readPlist reads the top dictionary of a property list: each key with its
// text, or "true" / "false" for a boolean.
func readPlist(t *testing.T, data []byte) map[string]string {
	t.Helper()
	decoder := xml.NewDecoder(bytes.NewReader(data))
	values := map[string]string{}
	var key, element string
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch tok := token.(type) {
		case xml.StartElement:
			depth++
			element = tok.Name.Local
			if element == "true" || element == "false" {
				values[key] = element
			}
		case xml.EndElement:
			depth--
			element = ""
		case xml.CharData:
			text := string(bytes.TrimSpace(tok))
			switch element {
			case "key":
				key = text
			case "string":
				values[key] = text
			}
		}
	}
	if depth != 0 || len(values) == 0 {
		t.Fatalf("Info.plist is not a complete property list")
	}
	return values
}
