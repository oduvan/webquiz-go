package pyval

import (
	"encoding/json"
	"math"
	"math/big"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// pyyamlCase is one entry of testdata/pyyaml_cases.json, written by
// testdata/gen_pyyaml_cases.py with the PyYAML of WebQuiz 1.19.
type pyyamlCase struct {
	YAML   string `json:"yaml"`
	Result any    `json:"result"`
	Error  string `json:"error"`
}

// typed writes a value the way gen_pyyaml_cases.py writes a Python value,
// so the two can be compared as JSON.
func typed(t *testing.T, v any) any {
	t.Helper()
	switch x := v.(type) {
	case nil:
		return map[string]any{"type": "null"}
	case bool:
		return map[string]any{"type": "bool", "value": x}
	case int:
		return map[string]any{"type": "int", "value": big.NewInt(int64(x)).String()}
	case int64:
		return map[string]any{"type": "int", "value": big.NewInt(x).String()}
	case *big.Int:
		return map[string]any{"type": "int", "value": x.String()}
	case float64:
		return map[string]any{"type": "float", "value": FloatRepr(x)}
	case string:
		return map[string]any{"type": "str", "value": x}
	case Timestamp:
		return map[string]any{"type": "timestamp"}
	case []any:
		items := make([]any, len(x))
		for i, item := range x {
			items[i] = typed(t, item)
		}
		return map[string]any{"type": "list", "items": items}
	case map[string]any:
		items := map[string]any{}
		for key, item := range x {
			items[key] = typed(t, item)
		}
		return map[string]any{"type": "dict", "items": items}
	}
	t.Fatalf("LoadYAML returned an unexpected Go type %T", v)
	return nil
}

// asJSON turns a value into plain JSON data, so DeepEqual compares like with like.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLoadYAMLMatchesPyYAML(t *testing.T) {
	data, err := os.ReadFile("testdata/pyyaml_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []pyyamlCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 100 {
		t.Fatalf("expected the full case list, got %d cases", len(cases))
	}
	for _, c := range cases {
		got, err := LoadYAML([]byte(c.YAML))
		if c.Error != "" {
			if err == nil {
				t.Errorf("LoadYAML(%q) = %#v, PyYAML gave %s", c.YAML, got, c.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("LoadYAML(%q) failed: %v", c.YAML, err)
			continue
		}
		if want, have := asJSON(t, c.Result), asJSON(t, typed(t, got)); !reflect.DeepEqual(want, have) {
			t.Errorf("LoadYAML(%q)\n got %v\nwant %v", c.YAML, have, want)
		}
	}
}

func TestLoadYAMLEmptyInput(t *testing.T) {
	for _, text := range []string{"", "   \n", "# comment\n", "---\n"} {
		got, err := LoadYAML([]byte(text))
		if err != nil || got != nil {
			t.Errorf("LoadYAML(%q) = %#v, %v; want nil, nil", text, got, err)
		}
	}
}

func TestLoadYAMLAliasesShareOneValue(t *testing.T) {
	got, err := LoadYAML([]byte("a: &q {x: 1}\nb: *q\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	a, b := m["a"].(map[string]any), m["b"].(map[string]any)
	a["y"] = 2
	if b["y"] != 2 {
		t.Errorf("an alias should give the same map, as in Python; got %#v", b)
	}
}

func TestLoadYAMLManyAliasesStayFast(t *testing.T) {
	// "Billion laughs": every level doubles the size if aliases are copied.
	var text strings.Builder
	text.WriteString("a0: &a0 [x, x]\n")
	for i := 1; i < 40; i++ {
		text.WriteString("a" + itoa(i) + ": &a" + itoa(i) + " [*a" + itoa(i-1) + ", *a" + itoa(i-1) + "]\n")
	}
	if _, err := LoadYAML([]byte(text.String())); err != nil {
		t.Fatal(err)
	}
}

func TestLoadYAMLRecursiveAliasFails(t *testing.T) {
	if _, err := LoadYAML([]byte("a: &a [1, *a]\n")); err == nil {
		t.Error("an alias inside its own anchor should be an error")
	}
}

func TestLoadYAMLGivesErrorLine(t *testing.T) {
	_, err := LoadYAML([]byte("a: 1\nb: !!int abc\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name line 2, got %v", err)
	}
}

func TestLoadYAMLNumbersKeepPythonTypes(t *testing.T) {
	got, err := LoadYAML([]byte("i: 5\nf: 5.0\nb: true\nbig: 123456789012345678901234567890\ninf: .inf\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if _, ok := m["i"].(int); !ok {
		t.Errorf("5 should be an int, got %T", m["i"])
	}
	if _, ok := m["f"].(float64); !ok {
		t.Errorf("5.0 should be a float64, got %T", m["f"])
	}
	if _, ok := m["b"].(bool); !ok {
		t.Errorf("true should be a bool, got %T", m["b"])
	}
	if _, ok := m["big"].(*big.Int); !ok {
		t.Errorf("a huge whole number should be a *big.Int, got %T", m["big"])
	}
	if f, ok := m["inf"].(float64); !ok || !math.IsInf(f, 1) {
		t.Errorf(".inf should be +Inf, got %#v", m["inf"])
	}
}

func itoa(i int) string {
	return big.NewInt(int64(i)).String()
}

// sortedKeys is a helper for tests that print maps.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
