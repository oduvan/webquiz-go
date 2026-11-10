package config

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// configCases is testdata/config_cases.json, written by
// testdata/gen_config_cases.py with WebQuiz 1.19.
type configCases struct {
	Validate []struct {
		Data   json.RawMessage `json:"data"`
		Valid  bool            `json:"valid"`
		Errors []string        `json:"errors"`
	} `json:"validate"`
	Merge []struct {
		Original     string          `json:"original"`
		Data         json.RawMessage `json:"data"`
		RuamelOutput string          `json:"ruamel_output"`
		Parsed       json.RawMessage `json:"parsed"`
	} `json:"merge"`
}

func readConfigCases(t *testing.T) configCases {
	t.Helper()
	raw, err := os.ReadFile("testdata/config_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases configCases
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Validate) < 50 || len(cases.Merge) < 20 {
		t.Fatalf("expected the full case lists, got %d and %d", len(cases.Validate), len(cases.Merge))
	}
	return cases
}

// decodeJSON reads JSON like the server reads request bodies: with UseNumber.
func decodeJSON(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var v any
	if err := decoder.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidateMatchesPython(t *testing.T) {
	for _, c := range readConfigCases(t).Validate {
		got := Validate(decodeJSON(t, c.Data))
		want := c.Errors
		if len(want) == 0 {
			want = nil
		}
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Validate(%s)\n got %q\nwant %q", c.Data, got, want)
		}
		if (len(got) == 0) != c.Valid {
			t.Errorf("Validate(%s): valid should be %v", c.Data, c.Valid)
		}
	}
}

func TestValidateYAMLValues(t *testing.T) {
	data, err := pyval.LoadYAML([]byte("registration:\n  approve: yes\n  fields:\n  - Клас\n  - on\nserver:\n  port: 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	// "on" is a boolean for PyYAML, so the list has a value that is not text.
	want := []string{"'registration.fields' must contain only strings"}
	if got := Validate(data); !reflect.DeepEqual(got, want) {
		t.Errorf("Validate = %q, want %q", got, want)
	}
	if got := Validate(nil); len(got) != 0 {
		t.Errorf("an empty config is valid, got %q", got)
	}
}
