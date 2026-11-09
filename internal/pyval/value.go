package pyval

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Timestamp is a date, or a date and time, that PyYAML read as a Python date
// object (for example 2024-01-15 written without quotes). Python code did not
// treat it as text, so KindOf gives KindOther. It holds the text as written.
type Timestamp string

// MarshalJSON writes the timestamp as its text.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(t))
}

// Kind is the Python type of a value.
type Kind int

// The kinds of values.
const (
	KindNone  Kind = iota // None (nil)
	KindBool              // bool
	KindInt               // int (not bool)
	KindFloat             // float
	KindStr               // str
	KindList              // list
	KindDict              // dict
	KindOther             // anything else (dates, bytes, Go values)
)

// String gives the Python name of the kind.
func (k Kind) String() string {
	switch k {
	case KindNone:
		return "NoneType"
	case KindBool:
		return "bool"
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindStr:
		return "str"
	case KindList:
		return "list"
	case KindDict:
		return "dict"
	}
	return "other"
}

// KindOf tells which Python type a value has.
func KindOf(v any) Kind {
	switch x := v.(type) {
	case nil:
		return KindNone
	case bool:
		return KindBool
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, *big.Int:
		return KindInt
	case float32, float64:
		return KindFloat
	case json.Number:
		if isJSONInt(string(x)) {
			return KindInt
		}
		return KindFloat
	case string:
		return KindStr
	case []any:
		return KindList
	case map[string]any:
		return KindDict
	case []byte, Timestamp:
		return KindOther
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Slice, reflect.Array:
		return KindList
	case reflect.Map:
		return KindDict
	}
	return KindOther
}

// isJSONInt tells whether a JSON number is an int for Python's json module:
// no fraction and no exponent.
func isJSONInt(s string) bool {
	return !strings.ContainsAny(s, ".eE")
}

// IsInt is isinstance(v, int). It is true for booleans too, as in Python.
func IsInt(v any) bool {
	k := KindOf(v)
	return k == KindInt || k == KindBool
}

// IsIntNotBool is isinstance(v, int) and not isinstance(v, bool).
func IsIntNotBool(v any) bool {
	return KindOf(v) == KindInt
}

// IsNumber is isinstance(v, (int, float)). It is true for booleans too.
func IsNumber(v any) bool {
	switch KindOf(v) {
	case KindBool, KindInt, KindFloat:
		return true
	}
	return false
}

// IsBool is isinstance(v, bool).
func IsBool(v any) bool { return KindOf(v) == KindBool }

// IsStr is isinstance(v, str).
func IsStr(v any) bool { return KindOf(v) == KindStr }

// IsList is isinstance(v, list).
func IsList(v any) bool { return KindOf(v) == KindList }

// IsDict is isinstance(v, dict).
func IsDict(v any) bool { return KindOf(v) == KindDict }

// number holds a Python int or float. An int is kept exactly in i.
type number struct {
	isInt bool
	i     *big.Int
	f     float64
}

// toNumber reads a bool, int or float.
func toNumber(v any) (number, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return number{isInt: true, i: big.NewInt(1)}, true
		}
		return number{isInt: true, i: big.NewInt(0)}, true
	case int:
		return number{isInt: true, i: big.NewInt(int64(x))}, true
	case int8:
		return number{isInt: true, i: big.NewInt(int64(x))}, true
	case int16:
		return number{isInt: true, i: big.NewInt(int64(x))}, true
	case int32:
		return number{isInt: true, i: big.NewInt(int64(x))}, true
	case int64:
		return number{isInt: true, i: big.NewInt(x)}, true
	case uint:
		return number{isInt: true, i: new(big.Int).SetUint64(uint64(x))}, true
	case uint8:
		return number{isInt: true, i: new(big.Int).SetUint64(uint64(x))}, true
	case uint16:
		return number{isInt: true, i: new(big.Int).SetUint64(uint64(x))}, true
	case uint32:
		return number{isInt: true, i: new(big.Int).SetUint64(uint64(x))}, true
	case uint64:
		return number{isInt: true, i: new(big.Int).SetUint64(x)}, true
	case *big.Int:
		if x == nil {
			return number{}, false
		}
		return number{isInt: true, i: x}, true
	case float32:
		return number{f: float64(x)}, true
	case float64:
		return number{f: x}, true
	case json.Number:
		s := string(x)
		if isJSONInt(s) {
			i, ok := new(big.Int).SetString(s, 10)
			if !ok {
				return number{}, false
			}
			return number{isInt: true, i: i}, true
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !isRangeError(err) {
			return number{}, false
		}
		return number{f: f}, true
	}
	return number{}, false
}

func isRangeError(err error) bool {
	numErr, ok := err.(*strconv.NumError)
	return ok && numErr.Err == strconv.ErrRange
}

// float gives the number as a float64 (rounded for huge ints).
func (n number) float() float64 {
	if !n.isInt {
		return n.f
	}
	f, _ := new(big.Float).SetInt(n.i).Float64()
	return f
}

// compare compares two numbers exactly, like Python does. ok is false when
// a NaN takes part.
func (n number) compare(m number) (result int, ok bool) {
	if n.isInt && m.isInt {
		return n.i.Cmp(m.i), true
	}
	if (!n.isInt && math.IsNaN(n.f)) || (!m.isInt && math.IsNaN(m.f)) {
		return 0, false
	}
	return n.bigFloat().Cmp(m.bigFloat()), true
}

func (n number) bigFloat() *big.Float {
	if n.isInt {
		return new(big.Float).SetInt(n.i)
	}
	return big.NewFloat(n.f) // ±Inf is allowed, NaN was ruled out
}

// Number gives the value of a bool, int or float as float64. For other values
// ok is false.
func Number(v any) (float64, bool) {
	n, ok := toNumber(v)
	if !ok {
		return 0, false
	}
	return n.float(), true
}

// Int gives the value of a bool or int (True is 1). ok is false for other
// values, for floats, and for ints that do not fit in int64.
func Int(v any) (int64, bool) {
	n, ok := toNumber(v)
	if !ok || !n.isInt || !n.i.IsInt64() {
		return 0, false
	}
	return n.i.Int64(), true
}

// Compare compares two numbers (bools, ints, floats) like Python's < and >:
// it gives -1, 0 or +1. ok is false when a value is not a number or is NaN.
func Compare(a, b any) (result int, ok bool) {
	x, okA := toNumber(a)
	y, okB := toNumber(b)
	if !okA || !okB {
		return 0, false
	}
	return x.compare(y)
}

// ToFloat is Python's float(v) for numbers and text. Text may have spaces
// around it, underscores between digits, and the words inf, infinity and nan.
// ok is false where Python raises an error.
func ToFloat(v any) (float64, bool) {
	if s, isStr := v.(string); isStr {
		return parsePythonFloat(s)
	}
	return Number(v)
}

// parsePythonFloat reads text the way Python's float() does.
func parsePythonFloat(s string) (float64, bool) {
	s = Strip(s)
	if s == "" {
		return 0, false
	}
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 {
		return 0, false
	}
	switch strings.ToLower(body) {
	case "nan": // Go's ParseFloat does not take a sign before "nan"
		return math.NaN(), true
	case "inf", "infinity":
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	}
	// Digits, one dot, an exponent; underscores only between two digits.
	var clean strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_':
			if i == 0 || i == len(s)-1 || !isDigit(s[i-1]) || !isDigit(s[i+1]) {
				return 0, false
			}
		case isDigit(c) || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-':
			clean.WriteByte(c)
		default:
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(clean.String(), 64)
	if err != nil && !isRangeError(err) {
		return 0, false
	}
	return f, true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// List gives the items of a list value. Typed Go slices work too.
func List(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case nil, string, []byte:
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	items := make([]any, rv.Len())
	for i := range items {
		items[i] = rv.Index(i).Interface()
	}
	return items, true
}

// Dict gives a dict value as map[string]any. Maps with other key types work
// too: their keys become text with fmt.Sprint.
func Dict(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		return x, true
	case nil:
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map {
		return nil, false
	}
	m := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		m[fmt.Sprint(iter.Key().Interface())] = iter.Value().Interface()
	}
	return m, true
}

// Len is Python's len() for text (counted in characters), lists and dicts.
// ok is false for other values, where Python raises TypeError.
func Len(v any) (int, bool) {
	if s, ok := v.(string); ok {
		return utf8.RuneCountInString(s), true
	}
	if items, ok := List(v); ok {
		return len(items), true
	}
	if m, ok := Dict(v); ok {
		return len(m), true
	}
	return 0, false
}

// Truthy is Python's bool(v).
func Truthy(v any) bool {
	switch KindOf(v) {
	case KindNone:
		return false
	case KindBool:
		return v.(bool)
	case KindInt, KindFloat:
		n, ok := toNumber(v)
		if !ok {
			return true
		}
		if n.isInt {
			return n.i.Sign() != 0
		}
		return n.f != 0 // NaN is true in Python
	case KindStr, KindList, KindDict:
		n, _ := Len(v)
		return n > 0
	}
	if b, ok := v.([]byte); ok {
		return len(b) > 0
	}
	return true
}

// Equal is Python's a == b for the values this package knows. Numbers are
// compared by value (True == 1 == 1.0), lists item by item, dicts key by key.
func Equal(a, b any) bool {
	ka, kb := KindOf(a), KindOf(b)
	if isNumberKind(ka) && isNumberKind(kb) {
		x, okA := toNumber(a)
		y, okB := toNumber(b)
		if !okA || !okB {
			return false
		}
		result, ok := x.compare(y)
		return ok && result == 0
	}
	if ka != kb {
		return false
	}
	switch ka {
	case KindNone:
		return true
	case KindStr:
		return a.(string) == b.(string)
	case KindList:
		la, _ := List(a)
		lb, _ := List(b)
		if len(la) != len(lb) {
			return false
		}
		for i := range la {
			if !Equal(la[i], lb[i]) {
				return false
			}
		}
		return true
	case KindDict:
		da, _ := Dict(a)
		db, _ := Dict(b)
		if len(da) != len(db) {
			return false
		}
		for key, va := range da {
			vb, ok := db[key]
			if !ok || !Equal(va, vb) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

func isNumberKind(k Kind) bool {
	return k == KindBool || k == KindInt || k == KindFloat
}

// Str is Python's str(v).
func Str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case Timestamp:
		return string(x)
	case []byte:
		return Repr(v)
	}
	switch KindOf(v) {
	case KindNone:
		return "None"
	case KindBool:
		if v.(bool) {
			return "True"
		}
		return "False"
	case KindInt, KindFloat:
		n, ok := toNumber(v)
		if !ok {
			return fmt.Sprint(v)
		}
		if n.isInt {
			return n.i.String()
		}
		return FloatRepr(n.f)
	case KindList:
		items, _ := List(v)
		parts := make([]string, len(items))
		for i, item := range items {
			parts[i] = Repr(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case KindDict:
		// Python keeps insertion order; a Go map has none, so keys are sorted.
		m, _ := Dict(v)
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, key := range keys {
			parts[i] = Repr(key) + ": " + Repr(m[key])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprint(v)
}

// Repr is Python's repr(v). It differs from Str only for text, which gets
// quotes and escapes.
func Repr(v any) string {
	switch x := v.(type) {
	case string:
		return reprString(x)
	case []byte:
		return "b" + reprString(string(x))
	}
	return Str(v)
}

// reprString quotes text the way Python's repr() does.
func reprString(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == ' ' || unicode.IsPrint(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// FloatRepr is Python's repr() of a float: the shortest text that reads back
// as the same number, with ".0" for whole numbers and an exponent below 1e-4
// and from 1e16 on.
func FloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	sign := ""
	if math.Signbit(f) {
		sign = "-"
		f = -f
	}
	text := strconv.FormatFloat(f, 'e', -1, 64) // like "1.5e+16"
	mantissa, expText, _ := strings.Cut(text, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mantissa, ".", "", 1)
	if exp < -4 || exp >= 16 {
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		expSign := "+"
		if exp < 0 {
			expSign = "-"
			exp = -exp
		}
		return fmt.Sprintf("%s%se%s%02d", sign, out, expSign, exp)
	}
	if exp < 0 {
		return sign + "0." + strings.Repeat("0", -exp-1) + digits
	}
	if len(digits) <= exp+1 {
		return sign + digits + strings.Repeat("0", exp+1-len(digits)) + ".0"
	}
	return sign + digits[:exp+1] + "." + digits[exp+1:]
}

// IsSpace tells whether Python's str.isspace() is true for the character.
// It is unicode.IsSpace plus the separators \x1c to \x1f.
func IsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// Strip is Python's str.strip() without arguments.
func Strip(s string) string {
	return strings.TrimFunc(s, IsSpace)
}

// Round is Python's round(x, ndigits) for a float. It rounds the exact binary
// value of x and sends halves to the even digit, as Python does.
func Round(x float64, ndigits int) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', ndigits, 64), 64)
	if err != nil {
		return x
	}
	return rounded
}
