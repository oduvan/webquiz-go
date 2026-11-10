package checker

import (
	"encoding/json"
	"os"
	"testing"
)

// pythonResults is testdata/python_results.json, written by
// testdata/gen_python_results.py from the checker of WebQuiz 1.19.
type pythonResults struct {
	WebQuizVersion string `json:"webquiz_version"`
	PythonVersion  string `json:"python_version"`
	Cases          []struct {
		Name          string  `json:"name"`
		Checker       string  `json:"checker"`
		Answer        string  `json:"answer"`
		Correct       bool    `json:"correct"`
		Error         *string `json:"error"`
		ErrorType     *string `json:"error_type"`
		GoUnsupported string  `json:"go_unsupported"`
	} `json:"cases"`
}

// messagesMustMatch are the Python errors whose text the Go checker gives
// too: assert messages, unknown names, and the ValueError texts of int(),
// float(), the helpers and math.
var messagesMustMatch = map[string]bool{
	"AssertionError": true,
	"NameError":      true,
	"ValueError":     true,
}

func TestMatchesPythonResults(t *testing.T) {
	data, err := os.ReadFile("testdata/python_results.json")
	if err != nil {
		t.Fatal(err)
	}
	var results pythonResults
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	if results.WebQuizVersion != "1.19" || len(results.Cases) < 60 {
		t.Fatalf("unexpected fixture: version %q, %d cases", results.WebQuizVersion, len(results.Cases))
	}
	for _, c := range results.Cases {
		got := Check(c.Checker, c.Answer)
		if c.GoUnsupported != "" {
			// Python features that Starlark cannot run: the Go checker
			// must report an error, not crash and not accept the answer.
			if got.Correct || got.Error == "" {
				t.Errorf("%s (%q): Go gives %+v, want an error because %s", c.Name, c.Answer, got, c.GoUnsupported)
			}
			continue
		}
		if got.Correct != c.Correct {
			t.Errorf("%s (%q): Go correct=%v (%q), Python correct=%v (%q)", c.Name, c.Answer, got.Correct, got.Error, c.Correct, deref(c.Error))
			continue
		}
		if !c.Correct && c.ErrorType != nil && messagesMustMatch[*c.ErrorType] && got.Error != deref(c.Error) {
			t.Errorf("%s (%q): Go error %q, Python %s %q", c.Name, c.Answer, got.Error, *c.ErrorType, deref(c.Error))
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
