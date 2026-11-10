package pyval

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"
)

func TestKindOf(t *testing.T) {
	cases := []struct {
		value any
		want  Kind
	}{
		{nil, KindNone},
		{true, KindBool},
		{0, KindInt},
		{int64(7), KindInt},
		{big.NewInt(5), KindInt},
		{json.Number("12"), KindInt},
		{json.Number("-0"), KindInt},
		{json.Number("1.0"), KindFloat},
		{json.Number("1e2"), KindFloat},
		{json.Number("1E2"), KindFloat},
		{1.5, KindFloat},
		{"x", KindStr},
		{[]any{1}, KindList},
		{[]string{"a"}, KindList},
		{map[string]any{}, KindDict},
		{Timestamp("2024-01-15"), KindOther},
		{[]byte("x"), KindOther},
	}
	for _, c := range cases {
		if got := KindOf(c.value); got != c.want {
			t.Errorf("KindOf(%#v) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestIsInt(t *testing.T) {
	// isinstance(x, int) is true for booleans too
	for _, v := range []any{0, -3, true, false, json.Number("4"), big.NewInt(1)} {
		if !IsInt(v) {
			t.Errorf("IsInt(%#v) = false", v)
		}
	}
	for _, v := range []any{1.0, json.Number("1.0"), "1", nil, []any{}} {
		if IsInt(v) {
			t.Errorf("IsInt(%#v) = true", v)
		}
	}
	if IsIntNotBool(true) || !IsIntNotBool(3) || IsIntNotBool(3.0) {
		t.Error("IsIntNotBool should accept whole numbers only, not booleans")
	}
	if !IsNumber(true) || !IsNumber(1.5) || !IsNumber(json.Number("2")) || IsNumber("2") || IsNumber(nil) {
		t.Error("IsNumber should be isinstance(x, (int, float))")
	}
}

func TestTruthy(t *testing.T) {
	truthy := []any{true, 1, -1, 0.5, math.NaN(), json.Number("2"), json.Number("0.1"), "a", " ", []any{nil}, map[string]any{"a": nil}, Timestamp("2024-01-15"), big.NewInt(3)}
	falsy := []any{nil, false, 0, 0.0, json.Number("0"), json.Number("0.0"), "", []any{}, map[string]any{}, big.NewInt(0)}
	for _, v := range truthy {
		if !Truthy(v) {
			t.Errorf("Truthy(%#v) = false", v)
		}
	}
	for _, v := range falsy {
		if Truthy(v) {
			t.Errorf("Truthy(%#v) = true", v)
		}
	}
}

func TestEqual(t *testing.T) {
	equal := [][2]any{
		{1, 1},
		{1, 1.0},
		{true, 1},
		{false, 0.0},
		{json.Number("2"), 2},
		{json.Number("2.0"), 2},
		{json.Number("2"), json.Number("2.0")},
		{big.NewInt(5), 5.0},
		{"a", "a"},
		{nil, nil},
		{[]any{1, "x"}, []any{1.0, "x"}},
		{[]any{json.Number("0"), json.Number("2")}, []int{0, 2}},
		{map[string]any{"a": 1}, map[string]any{"a": true}},
	}
	for _, pair := range equal {
		if !Equal(pair[0], pair[1]) || !Equal(pair[1], pair[0]) {
			t.Errorf("Equal(%#v, %#v) = false", pair[0], pair[1])
		}
	}
	notEqual := [][2]any{
		{1, "1"},
		{1, 2},
		{nil, 0},
		{nil, ""},
		{math.NaN(), math.NaN()},
		{[]any{1}, []any{1, 2}},
		{[]any{1}, 1},
		{map[string]any{"a": 1}, map[string]any{"b": 1}},
		{"1.0", 1.0},
		// 2**53 + 1 cannot be a float, so it is not equal to the nearest float
		{json.Number("9007199254740993"), 9007199254740992.0},
	}
	for _, pair := range notEqual {
		if Equal(pair[0], pair[1]) || Equal(pair[1], pair[0]) {
			t.Errorf("Equal(%#v, %#v) = true", pair[0], pair[1])
		}
	}
}

func TestFloatRepr(t *testing.T) {
	// Expected values printed by Python 3: repr(x)
	cases := map[float64]string{
		1e16:                   "1e+16",
		1e15:                   "1000000000000000.0",
		1.5e-5:                 "1.5e-05",
		0.0001:                 "0.0001",
		0.00001:                "1e-05",
		123456789.123:          "123456789.123",
		2.5:                    "2.5",
		1.0 / 3:                "0.3333333333333333",
		1e22:                   "1e+22",
		1.7976931348623157e308: "1.7976931348623157e+308",
		5e-324:                 "5e-324",
		100:                    "100.0",
		1e-7:                   "1e-07",
		12345678901234567.0:    "1.2345678901234568e+16",
		0:                      "0.0",
		-1.5:                   "-1.5",
		math.Inf(1):            "inf",
		math.Inf(-1):           "-inf",
	}
	for value, want := range cases {
		if got := FloatRepr(value); got != want {
			t.Errorf("FloatRepr(%v) = %q, want %q", value, got, want)
		}
	}
	a, b := 0.1, 0.2 // added at run time, as in Python
	if got := FloatRepr(a + b); got != "0.30000000000000004" {
		t.Errorf("FloatRepr(0.1 + 0.2) = %q", got)
	}
	if got := FloatRepr(math.Copysign(0, -1)); got != "-0.0" {
		t.Errorf("FloatRepr(-0.0) = %q", got)
	}
	if got := FloatRepr(math.NaN()); got != "nan" {
		t.Errorf("FloatRepr(NaN) = %q", got)
	}
}

func TestStrAndRepr(t *testing.T) {
	// Expected values printed by Python 3: str(x)
	cases := []struct {
		value any
		want  string
	}{
		{nil, "None"},
		{true, "True"},
		{false, "False"},
		{5, "5"},
		{json.Number("-0"), "0"},
		{json.Number("7"), "7"},
		{json.Number("1.0"), "1.0"},
		{json.Number("1e2"), "100.0"},
		{2.5, "2.5"},
		{"text", "text"},
		{[]any{1, "a", nil, true, 1.5}, "[1, 'a', None, True, 1.5]"},
		{map[string]any{"a": 1, "b": []any{nil}}, "{'a': 1, 'b': [None]}"},
		{[]any{}, "[]"},
		{map[string]any{}, "{}"},
		{big.NewInt(12), "12"},
	}
	for _, c := range cases {
		if got := Str(c.value); got != c.want {
			t.Errorf("Str(%#v) = %q, want %q", c.value, got, c.want)
		}
	}
	reprs := map[string]string{
		"it's":         `"it's"`,
		`a"b`:          `'a"b'`,
		`a'b"c`:        `'a\'b"c'`,
		"Клас":         "'Клас'",
		"tab\there\n":  `'tab\there\n'`,
		"\x00\x7f":     `'\x00\x7f'`,
		"\xc2\xa0":     `'\xa0'`,
		"\xe2\x80\xa8": "'\x5cu2028'",
		`back\slash`:   `'back\\slash'`,
		"emoji 😀":      "'emoji 😀'",
	}
	for value, want := range reprs {
		if got := Repr(value); got != want {
			t.Errorf("Repr(%q) = %s, want %s", value, got, want)
		}
	}
}

func TestStrip(t *testing.T) {
	// Python's str.strip() removes these, Go's strings.TrimSpace keeps \x1c-\x1f
	if got := Strip("\x1c\t a b  　\x1f\n"); got != "a b" {
		t.Errorf("Strip = %q", got)
	}
	if got := Strip("​ x"); got != "​ x" {
		t.Errorf("a zero width space is not white space in Python, got %q", got)
	}
}

func TestRound(t *testing.T) {
	// Python's round(x, 2) rounds the exact binary value, halves to even
	cases := map[float64]float64{
		0.125:  0.12,
		0.375:  0.38,
		2.675:  2.67,
		1.005:  1.0,
		0.5:    0.5,
		1.25:   1.25,
		-0.125: -0.12,
	}
	for value, want := range cases {
		if got := Round(value, 2); got != want {
			t.Errorf("Round(%v, 2) = %v, want %v", value, got, want)
		}
	}
	if got := Round(1e300, 2); got != 1e300 {
		t.Errorf("Round(1e300, 2) = %v", got)
	}
	if !math.IsInf(Round(math.Inf(1), 2), 1) || !math.IsNaN(Round(math.NaN(), 2)) {
		t.Error("Round should keep inf and nan")
	}
}

func TestToFloat(t *testing.T) {
	good := map[any]float64{
		"1.5":     1.5,
		" 2 \n":   2,
		"1_000.5": 1000.5,
		"1e3":     1000,
		"-.5":     -0.5,
		"+7":      7,
		"1e-400":  0,
		3:         3,
		true:      1,
		2.25:      2.25,
	}
	for value, want := range good {
		got, ok := ToFloat(value)
		if !ok || got != want {
			t.Errorf("ToFloat(%#v) = %v, %v; want %v", value, got, ok, want)
		}
	}
	if f, ok := ToFloat("inf"); !ok || !math.IsInf(f, 1) {
		t.Errorf("ToFloat(inf) = %v, %v", f, ok)
	}
	if f, ok := ToFloat("-Infinity"); !ok || !math.IsInf(f, -1) {
		t.Errorf("ToFloat(-Infinity) = %v, %v", f, ok)
	}
	for _, text := range []string{"nan", "-nan", "+NaN"} {
		if f, ok := ToFloat(text); !ok || !math.IsNaN(f) {
			t.Errorf("ToFloat(%q) = %v, %v", text, f, ok)
		}
	}
	if f, ok := ToFloat("1e400"); !ok || !math.IsInf(f, 1) {
		t.Errorf("ToFloat(1e400) = %v, %v", f, ok)
	}
	for _, bad := range []any{"", "abc", "0x10", "1__0", "_1", "1_", "1.5.2", nil, []any{}, "0x1p4"} {
		if f, ok := ToFloat(bad); ok {
			t.Errorf("ToFloat(%#v) = %v, should fail like Python float()", bad, f)
		}
	}
}

func TestLenAndConversions(t *testing.T) {
	if n, ok := Len("Клас"); !ok || n != 4 {
		t.Errorf("Len counts characters like Python, got %d", n)
	}
	if n, ok := Len([]any{1, 2}); !ok || n != 2 {
		t.Errorf("Len(list) = %d", n)
	}
	if n, ok := Len(map[string]any{"a": 1}); !ok || n != 1 {
		t.Errorf("Len(dict) = %d", n)
	}
	if _, ok := Len(5); ok {
		t.Error("an int has no len()")
	}
	if i, ok := Int(json.Number("42")); !ok || i != 42 {
		t.Errorf("Int(42) = %d, %v", i, ok)
	}
	if i, ok := Int(true); !ok || i != 1 {
		t.Errorf("Int(true) = %d, %v", i, ok)
	}
	if _, ok := Int(1.0); ok {
		t.Error("Int(1.0) should fail: a float is not an int")
	}
	if f, ok := Number(json.Number("2.5")); !ok || f != 2.5 {
		t.Errorf("Number(2.5) = %v, %v", f, ok)
	}
	if items, ok := List([]string{"a", "b"}); !ok || len(items) != 2 || items[1] != "b" {
		t.Errorf("List([]string) = %#v, %v", items, ok)
	}
	if _, ok := List("ab"); ok {
		t.Error("a string is not a list")
	}
	if m, ok := Dict(map[string]any{"a": 1}); !ok || m["a"] != 1 {
		t.Errorf("Dict = %#v, %v", m, ok)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b any
		want int
	}{
		{1, 2, -1},
		{2.5, 2, 1},
		{true, 1, 0},
		{json.Number("3"), 3.0, 0},
		{json.Number("-1"), 0, -1},
		{big.NewInt(10), json.Number("9.5"), 1},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if !ok || got != c.want {
			t.Errorf("Compare(%#v, %#v) = %d, %v; want %d", c.a, c.b, got, ok, c.want)
		}
	}
	for _, pair := range [][2]any{{1, "1"}, {nil, 0}, {math.NaN(), 1}, {[]any{}, 1}} {
		if _, ok := Compare(pair[0], pair[1]); ok {
			t.Errorf("Compare(%#v, %#v) should fail", pair[0], pair[1])
		}
	}
}

func TestTypeChecks(t *testing.T) {
	if !IsBool(false) || IsBool(0) {
		t.Error("IsBool")
	}
	if !IsStr("") || IsStr(Timestamp("2024-01-15")) {
		t.Error("IsStr: a date is not text")
	}
	if !IsList([]any{}) || !IsList([]int{1}) || IsList("ab") || IsList([]byte("x")) {
		t.Error("IsList")
	}
	if !IsDict(map[string]any{}) || !IsDict(map[any]any{1: 2}) || IsDict([]any{}) {
		t.Error("IsDict")
	}
	if m, ok := Dict(map[any]any{1: "a"}); !ok || m["1"] != "a" {
		t.Errorf("Dict(map[any]any) = %#v, %v", m, ok)
	}
	if _, ok := Dict(nil); ok {
		t.Error("Dict(nil) should fail")
	}
	if KindStr.String() != "str" || KindNone.String() != "NoneType" || KindDict.String() != "dict" {
		t.Error("Kind names should be the Python type names")
	}
	data, err := json.Marshal(map[string]any{"d": Timestamp("2024-01-15")})
	if err != nil || string(data) != `{"d":"2024-01-15"}` {
		t.Errorf("Timestamp JSON = %s, %v", data, err)
	}
	if Str(Timestamp("2024-01-15")) != "2024-01-15" || !Truthy(Timestamp("x")) {
		t.Error("Timestamp text and truth")
	}
	if Repr([]byte("ab")) != "b'ab'" {
		t.Errorf("Repr(bytes) = %s", Repr([]byte("ab")))
	}
	for _, v := range []any{int8(1), int16(1), int32(1), uint(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(1)} {
		if f, ok := Number(v); !ok || f != 1 {
			t.Errorf("Number(%T) = %v, %v", v, f, ok)
		}
	}
}

func TestLoadYAMLTaggedValues(t *testing.T) {
	got, err := LoadYAML([]byte("b: !!binary aGVsbG8=\nt: !!timestamp 2024-01-15\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if b, ok := m["b"].([]byte); !ok || string(b) != "hello" {
		t.Errorf("!!binary = %#v", m["b"])
	}
	if m["t"] != Timestamp("2024-01-15") {
		t.Errorf("!!timestamp = %#v", m["t"])
	}
	if b, err := LoadYAML([]byte("!!binary '***'")); err != nil || len(b.([]byte)) != 0 {
		t.Errorf("!!binary skips characters outside base64, as Python: %#v, %v", b, err)
	}
	for _, bad := range []string{"!!binary 'aGVsbG8'", "!!timestamp nope", "!!float abc", "!!set {a: 1}", "!!str {a: 1}", "!!map [1]", "a: !!omap [{a: 1}]"} {
		if v, err := LoadYAML([]byte(bad)); err == nil {
			t.Errorf("LoadYAML(%q) = %#v, want an error", bad, v)
		}
	}
}
