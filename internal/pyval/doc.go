// Package pyval gives Go code the Python value rules that WebQuiz 1.19 relied on.
//
// The Python version read quiz files and webquiz.yaml with PyYAML and checked
// the values with isinstance(), truth tests and ==. The Go version must read
// the same files the same way, so this package copies those rules:
//
//   - LoadYAML reads YAML like PyYAML's yaml.safe_load (YAML 1.1): yes, no,
//     on and off are booleans, 1:30 is the number 90, 1e3 is text, a date is
//     a Timestamp. It gives nil, bool, int (or *big.Int when it does not fit),
//     float64, string, Timestamp, []byte (!!binary), []any and map[string]any.
//     Keys that are not text become text the way Python's json.dumps writes
//     them (1 becomes "1", true becomes "true", null becomes "null").
//   - KindOf, IsInt, IsIntNotBool, IsNumber, IsStr, IsList and IsDict answer
//     isinstance() questions. Booleans count as int, as in Python.
//   - Truthy is Python's bool(x), Equal is ==, Str is str(x), Repr is repr(x),
//     FloatRepr is repr() of a float, Strip is str.strip(), Round is
//     round(x, n), ToFloat is float(x), Len is len(x).
//
// Values that came from JSON request bodies decoded with UseNumber hold
// json.Number. A json.Number with a dot or an exponent is a float, any other
// is an int, as in Python's json module. All functions accept them.
package pyval
