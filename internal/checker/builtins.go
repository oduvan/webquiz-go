package checker

// The names a checker can use. Starlark has most Python builtins already;
// this file adds the missing ones (sum, round, pow, map, filter, isinstance,
// format), the math module with a few more functions, the helpers to_int,
// distance and direction_angle, and the internal functions that the
// rewrite step calls (_assert_, _fmt_, _iter_, _attr_). Some builtins are
// replaced to act like Python: str and repr quote like Python, int and float
// read text the Python way, and list, sorted, min and friends accept text.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"

	starlarkmath "go.starlark.net/lib/math"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// maxPowerBits limits the size of a**b and a*b for integers, so that
// 10 ** 10 ** 10 fails at once instead of running for a very long time.
const maxPowerBits = 1 << 20

const tooLargeNumber = "the number is too large"

// maxRepeatBytes limits the size of text * n and list * n, which Starlark
// makes in one step, before the memory watch could stop the checker.
const maxRepeatBytes = MaxMemory / 2

// predeclared holds the names every checker can use. The values never
// change, so all checks share them.
var predeclared = newPredeclared()

func newPredeclared() starlark.StringDict {
	d := starlark.StringDict{
		"_assert_":        builtin("_assert_", assertBuiltin),
		"_fmt_":           builtin("_fmt_", fmtBuiltin),
		"_iter_":          builtin("_iter_", iterBuiltin),
		"_attr_":          builtin("_attr_", attrBuiltin),
		"_mul_":           builtin("_mul_", mulBuiltin),
		"math":            mathModule(),
		"to_int":          helperBuiltin("to_int", toInt),
		"distance":        helperBuiltin("distance", distance),
		"direction_angle": helperBuiltin("direction_angle", directionAngle),
		"sum":             builtin("sum", sumBuiltin),
		"round":           builtin("round", roundBuiltin),
		"pow":             builtin("pow", powBuiltin),
		"map":             builtin("map", mapBuiltin),
		"filter":          builtin("filter", filterBuiltin),
		"isinstance":      builtin("isinstance", isinstanceBuiltin),
		"format":          builtin("format", formatBuiltin),
		"str":             builtin("str", strBuiltin),
		"repr":            builtin("repr", reprBuiltin),
		"int":             builtin("int", intBuiltin),
		"float":           builtin("float", floatBuiltin),
		"type":            builtin("type", typeBuiltin),
		"print":           builtin("print", printBuiltin),
	}
	// These accept text like Python does: list("ab") is ["a", "b"].
	for name, textArgs := range map[string]func(int) bool{
		"list":      firstArg,
		"tuple":     firstArg,
		"set":       firstArg,
		"sorted":    firstArg,
		"reversed":  firstArg,
		"enumerate": firstArg,
		"any":       firstArg,
		"all":       firstArg,
		"zip":       func(int) bool { return true },
		"min":       nil,
		"max":       nil,
	} {
		d[name] = textBuiltin(name, textArgs)
	}
	return d
}

func firstArg(i int) bool { return i == 0 }

func builtin(name string, fn func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)) *starlark.Builtin {
	return starlark.NewBuiltin(name, fn)
}

// isPredeclared tells the Starlark compiler which names come from outside.
// Names that are not defined anywhere count too: Python reports them only
// when the line runs ("name 'x' is not defined"), and so does the checker.
func isPredeclared(name string) bool {
	if _, ok := predeclared[name]; ok || name == "_answer_" {
		return true
	}
	_, universal := starlark.Universe[name]
	return !universal
}

const stateKey = "webquiz.checker"

func stateOf(thread *starlark.Thread) *runState {
	s, _ := thread.Local(stateKey).(*runState)
	return s
}

// step counts work done inside a builtin, like the interpreter counts its
// own steps, and stops the builtin when the check must stop.
func step(thread *starlark.Thread) error {
	thread.Steps++
	if thread.Steps >= MaxSteps {
		return errors.New(tooManySteps)
	}
	if s := stateOf(thread); s != nil && thread.Steps%256 == 0 {
		select {
		case <-s.stopped:
			return errors.New(s.reason)
		default:
		}
	}
	return nil
}

// textChars returns the characters of a text, as Python iterates it.
func textChars(s string) starlark.Tuple {
	chars := make(starlark.Tuple, 0, len(s))
	for _, r := range s {
		chars = append(chars, starlark.String(string(r)))
	}
	return chars
}

// iterate calls fn for every item of v. Text gives its characters.
func iterate(thread *starlark.Thread, v starlark.Value, fn func(starlark.Value) error) error {
	switch v := v.(type) {
	case starlark.String:
		for _, c := range textChars(string(v)) {
			if err := fn(c); err != nil {
				return err
			}
		}
		return nil
	case starlark.Iterable:
		iter := v.Iterate()
		defer iter.Done()
		var x starlark.Value
		for iter.Next(&x) {
			if err := step(thread); err != nil {
				return err
			}
			if err := fn(x); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("'%s' object is not iterable", pyTypeName(v))
}

// items collects the items of v. Lists and tuples are used as they are.
func items(thread *starlark.Thread, v starlark.Value) (starlark.Tuple, error) {
	switch v := v.(type) {
	case starlark.Tuple:
		return v, nil
	case *starlark.List:
		out := make(starlark.Tuple, v.Len())
		for i := range out {
			out[i] = v.Index(i)
		}
		return out, nil
	}
	var out starlark.Tuple
	err := iterate(thread, v, func(x starlark.Value) error {
		out = append(out, x)
		return nil
	})
	return out, err
}

// textBuiltin wraps a Starlark builtin so that its iterable arguments may be
// text. textArg tells which positional arguments are iterables; nil means
// "the only argument, when there is just one" (min and max).
func textBuiltin(name string, textArg func(int) bool) *starlark.Builtin {
	original := starlark.Universe[name].(*starlark.Builtin)
	return builtin(name, func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		converted := make(starlark.Tuple, len(args))
		for i, arg := range args {
			converted[i] = arg
			isIterable := textArg != nil && textArg(i) || textArg == nil && len(args) == 1
			if !isIterable {
				continue
			}
			switch arg.(type) {
			case starlark.String, starlark.Iterable:
				collected, err := items(thread, arg)
				if err != nil {
					return nil, err
				}
				converted[i] = collected
			}
		}
		if name == "enumerate" && len(kwargs) == 1 && kwargs[0][0] == starlark.String("start") && len(converted) == 1 {
			// Python allows enumerate(x, start=1); Starlark only enumerate(x, 1).
			converted, kwargs = append(converted, kwargs[0][1]), nil
		}
		return original.CallInternal(thread, converted, kwargs)
	})
}

// assertBuiltin runs a Python assert: _assert_(condition, message) where
// message is a function, so that it is only built when the check fails.
func assertBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond, message starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &cond, &message); err != nil {
		return nil, err
	}
	if cond.Truth() {
		return starlark.None, nil
	}
	if message == nil {
		return nil, errors.New("")
	}
	text, err := starlark.Call(thread, message, nil, nil)
	if err != nil {
		return nil, err
	}
	return nil, errors.New(pyStr(text))
}

// fmtBuiltin formats one f-string field: _fmt_(value, conversion, spec).
func fmtBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	var conversion, spec string
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 3, &value, &conversion, &spec); err != nil {
		return nil, err
	}
	switch conversion {
	case "r":
		value = starlark.String(pyRepr(value))
	case "s":
		value = starlark.String(pyStr(value))
	case "a":
		value = starlark.String(pyASCII(value))
	}
	text, err := formatValue(value, spec)
	if err != nil {
		return nil, err
	}
	return starlark.String(text), nil
}

// mulBuiltin is a * b, but refuses a text or list that would be too large.
func mulBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &x, &y); err != nil {
		return nil, err
	}
	if repeatTooLarge(x, y) || repeatTooLarge(y, x) {
		return nil, errors.New(tooMuchMemory)
	}
	if xi, ok := x.(starlark.Int); ok {
		if yi, ok := y.(starlark.Int); ok && xi.BigInt().BitLen()+yi.BigInt().BitLen() > maxPowerBits {
			return nil, errors.New(tooLargeNumber)
		}
	}
	return starlark.Binary(syntax.STAR, x, y)
}

// repeatTooLarge tells whether seq * count would be larger than
// maxRepeatBytes. A list item takes about 16 bytes.
func repeatTooLarge(seq, count starlark.Value) bool {
	var size uint64
	switch s := seq.(type) {
	case starlark.String:
		size = uint64(len(s))
	case starlark.Bytes:
		size = uint64(len(s))
	case *starlark.List:
		size = uint64(s.Len()) * 16
	case starlark.Tuple:
		size = uint64(len(s)) * 16
	default:
		return false
	}
	n, ok := count.(starlark.Int)
	if !ok || size == 0 || n.Sign() <= 0 {
		return false
	}
	times, ok := n.Uint64()
	return !ok || times > maxRepeatBytes/size
}

// iterBuiltin returns what a for loop goes over: the characters of a text,
// or the value itself.
func iterBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v); err != nil {
		return nil, err
	}
	if s, ok := v.(starlark.String); ok {
		return textChars(string(s)), nil
	}
	return v, nil
}

// attrBuiltin returns x.name. When the Starlark value has no such method
// but Python has one, it returns this file's version of it.
func attrBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	var name string
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &x, &name); err != nil {
		return nil, err
	}
	if obj, ok := x.(starlark.HasAttrs); ok {
		if v, err := obj.Attr(name); err == nil && v != nil {
			return v, nil
		}
	}
	if method := extraMethods[x.Type()][name]; method != nil {
		return starlark.NewBuiltin(name, method).BindReceiver(x), nil
	}
	return nil, fmt.Errorf("%s has no .%s field or method", x.Type(), name)
}

type builtinFunc = func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)

// extraMethods are Python methods that Starlark values do not have.
var extraMethods = map[string]map[string]builtinFunc{
	"string": {
		"isnumeric": textTest(unicode.IsNumber),
		"isdecimal": textTest(unicode.IsDigit),
		"isascii":   isASCII,
		"casefold":  textChange(func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ß", "ss") }),
		"swapcase":  textChange(swapCase),
		"zfill":     zfill,
		"ljust":     justify('<'),
		"rjust":     justify('>'),
		"center":    justify('^'),
	},
	"list": {
		"sort":    listSort,
		"reverse": listReverse,
		"count":   countItems,
		"copy":    listCopy,
	},
	"tuple": {"count": countItems},
	"dict": {
		"copy": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
				return nil, err
			}
			d := starlark.NewDict(b.Receiver().(*starlark.Dict).Len())
			for _, item := range b.Receiver().(*starlark.Dict).Items() {
				if err := d.SetKey(item[0], item[1]); err != nil {
					return nil, err
				}
			}
			return d, nil
		},
	},
	"set": {
		"copy": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
				return nil, err
			}
			return b.Receiver().(*starlark.Set).Union(starlark.NewSet(0).Iterate())
		},
	},
	"float": {"is_integer": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
			return nil, err
		}
		x := float64(b.Receiver().(starlark.Float))
		return starlark.Bool(!math.IsInf(x, 0) && x == math.Trunc(x)), nil
	}},
	"int": {"is_integer": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		return starlark.True, starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0)
	}},
}

func textTest(ok func(rune) bool) builtinFunc {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
			return nil, err
		}
		s := string(b.Receiver().(starlark.String))
		if s == "" {
			return starlark.False, nil
		}
		for _, r := range s {
			if !ok(r) {
				return starlark.False, nil
			}
		}
		return starlark.True, nil
	}
}

func isASCII(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}
	s := string(b.Receiver().(starlark.String))
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return starlark.False, nil
		}
	}
	return starlark.True, nil
}

func textChange(change func(string) string) builtinFunc {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
			return nil, err
		}
		return starlark.String(change(string(b.Receiver().(starlark.String)))), nil
	}
}

func swapCase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

func zfill(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var width int
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &width); err != nil {
		return nil, err
	}
	s := string(b.Receiver().(starlark.String))
	n := width - utf8.RuneCountInString(s)
	if n <= 0 {
		return starlark.String(s), nil
	}
	sign := ""
	if s != "" && (s[0] == '+' || s[0] == '-') {
		sign, s = s[:1], s[1:]
	}
	return starlark.String(sign + strings.Repeat("0", n) + s), nil
}

func justify(align byte) builtinFunc {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var width int
		fill := " "
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &width, &fill); err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(fill) != 1 {
			return nil, errors.New("The fill character must be exactly one character long")
		}
		s := string(b.Receiver().(starlark.String))
		n := width - utf8.RuneCountInString(s)
		if n <= 0 {
			return starlark.String(s), nil
		}
		left := 0
		switch align {
		case '>':
			left = n
		case '^':
			// Python's str.center puts the odd space on the left when width is odd.
			left = n/2 + (n & width & 1)
		}
		return starlark.String(strings.Repeat(fill, left) + s + strings.Repeat(fill, n-left)), nil
	}
}

func listSort(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, errors.New("sort() takes no positional arguments")
	}
	list := b.Receiver().(*starlark.List)
	sorted, err := starlark.Call(thread, starlark.Universe["sorted"], starlark.Tuple{list}, kwargs)
	if err != nil {
		return nil, err
	}
	for i := 0; i < list.Len(); i++ {
		if err := list.SetIndex(i, sorted.(*starlark.List).Index(i)); err != nil {
			return nil, err
		}
	}
	return starlark.None, nil
}

func listReverse(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}
	list := b.Receiver().(*starlark.List)
	for i, j := 0, list.Len()-1; i < j; i, j = i+1, j-1 {
		a, z := list.Index(i), list.Index(j)
		if err := list.SetIndex(i, z); err != nil {
			return nil, err
		}
		if err := list.SetIndex(j, a); err != nil {
			return nil, err
		}
	}
	return starlark.None, nil
}

func countItems(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	seq := b.Receiver().(starlark.Indexable)
	n := 0
	for i := 0; i < seq.Len(); i++ {
		eq, err := starlark.Equal(seq.Index(i), x)
		if err != nil {
			return nil, err
		}
		if eq {
			n++
		}
	}
	return starlark.MakeInt(n), nil
}

func listCopy(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}
	list := b.Receiver().(*starlark.List)
	elems := make([]starlark.Value, list.Len())
	for i := range elems {
		elems[i] = list.Index(i)
	}
	return starlark.NewList(elems), nil
}

// helperBuiltin makes a builtin from one of the helpers of helpers.go.
func helperBuiltin(name string, fn func(string) (*big.Int, error)) *starlark.Builtin {
	return builtin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var s string
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &s); err != nil {
			return nil, err
		}
		n, err := fn(s)
		if err != nil {
			return nil, err
		}
		return starlark.MakeBigInt(n), nil
	})
}

func printBuiltin(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func strBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value = starlark.String("")
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0, &v); err != nil {
		return nil, err
	}
	return starlark.String(pyStr(v)), nil
}

func reprBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v); err != nil {
		return nil, err
	}
	return starlark.String(pyRepr(v)), nil
}

func formatBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	spec := ""
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v, &spec); err != nil {
		return nil, err
	}
	text, err := formatValue(v, spec)
	if err != nil {
		return nil, err
	}
	return starlark.String(text), nil
}

func intBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value = starlark.MakeInt(0)
	var base starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "x?", &x, "base?", &base); err != nil {
		return nil, err
	}
	s, isText := x.(starlark.String)
	if base != nil {
		if !isText {
			return nil, errors.New("int() can't convert non-string with explicit base")
		}
		return starlark.Call(thread, starlark.Universe["int"], starlark.Tuple{starlark.String(pyStrip(string(s))), base}, nil)
	}
	switch x := x.(type) {
	case starlark.String:
		n, ok := parsePyInt(string(s))
		if !ok {
			return nil, fmt.Errorf("invalid literal for int() with base 10: %s", pyReprString(string(s)))
		}
		return starlark.MakeBigInt(n), nil
	case starlark.Float:
		switch f := float64(x); {
		case math.IsInf(f, 0):
			return nil, errors.New("cannot convert float infinity to integer")
		case math.IsNaN(f):
			return nil, errors.New("cannot convert float NaN to integer")
		}
		return starlark.NumberToInt(x)
	case starlark.Int, starlark.Bool:
		return starlark.Call(thread, starlark.Universe["int"], starlark.Tuple{x}, nil)
	}
	return nil, fmt.Errorf("int() argument must be a string, a bytes-like object or a real number, not '%s'", pyTypeName(x))
}

func floatBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value = starlark.Float(0)
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0, &x); err != nil {
		return nil, err
	}
	switch x := x.(type) {
	case starlark.String:
		f, ok := parsePyFloat(string(x))
		if !ok {
			return nil, fmt.Errorf("could not convert string to float: %s", pyReprString(string(x)))
		}
		return starlark.Float(f), nil
	case starlark.Float, starlark.Int, starlark.Bool:
		return starlark.Call(thread, starlark.Universe["float"], starlark.Tuple{x}, nil)
	}
	return nil, fmt.Errorf("float() argument must be a string or a real number, not '%s'", pyTypeName(x))
}

// typeValues maps Starlark type names to the builtins that stand for the
// Python types, so that type(1) == int works as in Python.
var typeValues = map[string]starlark.Value{}

func init() {
	for starlarkName, name := range map[string]string{
		"int": "int", "float": "float", "string": "str", "bool": "bool",
		"list": "list", "tuple": "tuple", "dict": "dict", "set": "set",
	} {
		if v, ok := predeclared[name]; ok {
			typeValues[starlarkName] = v
		} else {
			typeValues[starlarkName] = starlark.Universe[name]
		}
	}
}

func typeBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	if v, ok := typeValues[x.Type()]; ok {
		return v, nil
	}
	return starlark.String(x.Type()), nil
}

func isinstanceBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, types starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &x, &types); err != nil {
		return nil, err
	}
	options, ok := types.(starlark.Tuple)
	if !ok {
		options = starlark.Tuple{types}
	}
	for _, option := range options {
		fn, ok := option.(*starlark.Builtin)
		if !ok {
			return nil, errors.New("isinstance() arg 2 must be a type, a tuple of types, or a union")
		}
		switch name := fn.Name(); name {
		case "int":
			if _, isBool := x.(starlark.Bool); isBool || x.Type() == "int" {
				return starlark.True, nil
			}
		case "str":
			if x.Type() == "string" {
				return starlark.True, nil
			}
		case "float", "bool", "list", "tuple", "dict", "set", "bytes", "range":
			if x.Type() == name {
				return starlark.True, nil
			}
		default:
			return nil, errors.New("isinstance() arg 2 must be a type, a tuple of types, or a union")
		}
	}
	return starlark.False, nil
}

func mapBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 || len(args) < 2 {
		return nil, errors.New("map() must have at least two arguments.")
	}
	lists := make([]starlark.Tuple, len(args)-1)
	shortest := -1
	for i, arg := range args[1:] {
		list, err := items(thread, arg)
		if err != nil {
			return nil, err
		}
		lists[i] = list
		if shortest < 0 || len(list) < shortest {
			shortest = len(list)
		}
	}
	out := make([]starlark.Value, 0, shortest)
	for i := 0; i < shortest; i++ {
		call := make(starlark.Tuple, len(lists))
		for j, list := range lists {
			call[j] = list[i]
		}
		v, err := starlark.Call(thread, args[0], call, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return starlark.NewList(out), nil
}

func filterBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var fn, seq starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &fn, &seq); err != nil {
		return nil, err
	}
	var out []starlark.Value
	err := iterate(thread, seq, func(x starlark.Value) error {
		keep := x
		if fn != starlark.None {
			v, err := starlark.Call(thread, fn, starlark.Tuple{x}, nil)
			if err != nil {
				return err
			}
			keep = v
		}
		if keep.Truth() {
			out = append(out, x)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return starlark.NewList(out), nil
}

// sumBuiltin is Python's sum(), with the compensated float sum of
// Python 3.12+ so that sum([0.1] * 10) == 1.0.
func sumBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var seq starlark.Value
	var start starlark.Value = starlark.MakeInt(0)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "iterable", &seq, "start?", &start); err != nil {
		return nil, err
	}
	switch start.(type) {
	case starlark.String:
		return nil, errors.New("sum() can't sum strings [use ''.join(seq) instead]")
	case starlark.Bytes:
		return nil, errors.New("sum() can't sum bytes [use b''.join(seq) instead]")
	}
	acc := start
	var compensation float64
	_, inFloat := acc.(starlark.Float)
	err := iterate(thread, seq, func(x starlark.Value) error {
		if bv, ok := x.(starlark.Bool); ok {
			x = starlark.MakeInt(boolInt(bv))
		}
		if bv, ok := acc.(starlark.Bool); ok {
			acc = starlark.MakeInt(boolInt(bv))
		}
		if f, ok := acc.(starlark.Float); ok && inFloat {
			if y, ok := floatOf(x); ok {
				total := float64(f) + y
				if math.Abs(float64(f)) >= math.Abs(y) {
					compensation += (float64(f) - total) + y
				} else {
					compensation += (y - total) + float64(f)
				}
				acc = starlark.Float(total)
				return nil
			}
			acc = starlark.Float(addCompensation(float64(f), compensation))
			compensation, inFloat = 0, false
		}
		next, err := starlark.Binary(syntax.PLUS, acc, x)
		if err != nil {
			return fmt.Errorf("unsupported operand type(s) for +: '%s' and '%s'", pyTypeName(acc), pyTypeName(x))
		}
		acc = next
		_, inFloat = acc.(starlark.Float)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if f, ok := acc.(starlark.Float); ok && inFloat {
		acc = starlark.Float(addCompensation(float64(f), compensation))
	}
	return acc, nil
}

func boolInt(b starlark.Bool) int {
	if b {
		return 1
	}
	return 0
}

func addCompensation(total, compensation float64) float64 {
	if compensation != 0 && !math.IsInf(compensation, 0) && !math.IsNaN(compensation) {
		return total + compensation
	}
	return total
}

// floatOf returns a float or a small int as a float64.
func floatOf(x starlark.Value) (float64, bool) {
	switch x := x.(type) {
	case starlark.Float:
		return float64(x), true
	case starlark.Int:
		if n, ok := x.Int64(); ok {
			return float64(n), true
		}
	}
	return 0, false
}

func roundBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, ndigits starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "number", &x, "ndigits?", &ndigits); err != nil {
		return nil, err
	}
	if bv, ok := x.(starlark.Bool); ok {
		x = starlark.MakeInt(boolInt(bv))
	}
	if ndigits == starlark.None {
		ndigits = nil
	}
	digits := 0
	if ndigits != nil {
		if err := starlark.AsInt(ndigits, &digits); err != nil {
			return nil, fmt.Errorf("'%s' object cannot be interpreted as an integer", pyTypeName(ndigits))
		}
	}
	switch x := x.(type) {
	case starlark.Int:
		if digits >= 0 {
			return x, nil
		}
		return starlark.MakeBigInt(roundRat(new(big.Rat).SetInt(x.BigInt()), digits).Num()), nil
	case starlark.Float:
		f := float64(x)
		if ndigits == nil {
			switch {
			case math.IsInf(f, 0):
				return nil, errors.New("cannot convert float infinity to integer")
			case math.IsNaN(f):
				return nil, errors.New("cannot convert float NaN to integer")
			}
			return starlark.NumberToInt(starlark.Float(math.RoundToEven(f)))
		}
		if math.IsInf(f, 0) || math.IsNaN(f) || f == 0 || digits > 400 {
			return x, nil
		}
		if digits < -400 {
			return starlark.Float(math.Copysign(0, f)), nil
		}
		rounded, _ := roundRat(new(big.Rat).SetFloat64(f), digits).Float64()
		return starlark.Float(math.Copysign(rounded, f)), nil
	}
	return nil, fmt.Errorf("type %s doesn't define __round__ method", pyTypeName(x))
}

// roundRat rounds r to the given number of decimal digits (negative means
// tens, hundreds, ...), with ties to even, as Python's round() does.
func roundRat(r *big.Rat, digits int) *big.Rat {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(digits))), nil)
	scaled := new(big.Rat).Set(r)
	if digits >= 0 {
		scaled.Mul(scaled, new(big.Rat).SetInt(scale))
	} else {
		scaled.Quo(scaled, new(big.Rat).SetInt(scale))
	}
	// floor(scaled + 1/2), then fix exact ties to the even neighbour.
	half := new(big.Rat).Add(scaled, big.NewRat(1, 2))
	n := new(big.Int).Div(half.Num(), half.Denom())
	if new(big.Rat).SetInt(n).Cmp(half) == 0 && n.Bit(0) == 1 {
		n.Sub(n, big.NewInt(1))
	}
	out := new(big.Rat).SetInt(n)
	if digits >= 0 {
		return out.Quo(out, new(big.Rat).SetInt(scale))
	}
	return out.Mul(out, new(big.Rat).SetInt(scale))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// powBuiltin is Python's pow(base, exp, mod) and the ** operator.
func powBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var base, exp, mod starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "base", &base, "exp", &exp, "mod?", &mod); err != nil {
		return nil, err
	}
	base, exp = boolAsInt(base), boolAsInt(exp)
	if mod != nil && mod != starlark.None {
		return modPow(base, exp, boolAsInt(mod))
	}
	bi, baseIsInt := base.(starlark.Int)
	ei, expIsInt := exp.(starlark.Int)
	if baseIsInt && expIsInt && ei.Sign() >= 0 {
		bits := bi.BigInt().BitLen()
		e, ok := ei.Int64()
		if !ok || bits > 1 && float64(bits-1)*float64(e) > maxPowerBits {
			return nil, errors.New(tooLargeNumber)
		}
		return starlark.MakeBigInt(new(big.Int).Exp(bi.BigInt(), ei.BigInt(), nil)), nil
	}
	x, ok1 := starlark.AsFloat(base)
	y, ok2 := starlark.AsFloat(exp)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("unsupported operand type(s) for ** or pow(): '%s' and '%s'", pyTypeName(base), pyTypeName(exp))
	}
	if x == 0 && y < 0 {
		return nil, errors.New("0.0 cannot be raised to a negative power")
	}
	if x < 0 && y != math.Trunc(y) && !math.IsInf(y, 0) {
		return nil, errors.New("a negative number cannot be raised to a fractional power")
	}
	result := math.Pow(x, y)
	if math.IsInf(result, 0) && !math.IsInf(x, 0) && !math.IsInf(y, 0) {
		return nil, errors.New("(34, 'Numerical result out of range')")
	}
	return starlark.Float(result), nil
}

func boolAsInt(v starlark.Value) starlark.Value {
	if bv, ok := v.(starlark.Bool); ok {
		return starlark.MakeInt(boolInt(bv))
	}
	return v
}

func modPow(base, exp, mod starlark.Value) (starlark.Value, error) {
	bi, ok1 := base.(starlark.Int)
	ei, ok2 := exp.(starlark.Int)
	mi, ok3 := mod.(starlark.Int)
	if !ok1 || !ok2 || !ok3 {
		return nil, errors.New("pow() 3rd argument not allowed unless all arguments are integers")
	}
	m := mi.BigInt()
	if m.Sign() == 0 {
		return nil, errors.New("pow() 3rd argument cannot be 0")
	}
	absM := new(big.Int).Abs(m)
	x := new(big.Int).Mod(bi.BigInt(), absM)
	e := ei.BigInt()
	if e.Sign() < 0 {
		if x.ModInverse(x, absM) == nil {
			return nil, errors.New("base is not invertible for the given modulus")
		}
		e = new(big.Int).Neg(e)
	}
	result := new(big.Int).Exp(x, e, absM)
	if m.Sign() < 0 && result.Sign() != 0 {
		result.Add(result, m)
	}
	return starlark.MakeBigInt(result), nil
}

// mathModule is go.starlark.net's math module with Python's errors and a few
// more functions that Python's math module has.
func mathModule() *starlarkstruct.Module {
	members := starlark.StringDict{}
	for name, v := range starlarkmath.Module.Members {
		members[name] = v
		if fn, ok := v.(*starlark.Builtin); ok && name != "floor" && name != "ceil" {
			members[name] = mathErrors(fn)
		}
	}
	unary := func(name string, fn func(float64) float64) {
		members[name] = mathErrors(builtin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var x starlark.Value
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
				return nil, err
			}
			f, ok := starlark.AsFloat(x)
			if !ok {
				return nil, fmt.Errorf("must be real number, not %s", pyTypeName(x))
			}
			return starlark.Float(fn(f)), nil
		}))
	}
	unary("log10", math.Log10)
	unary("log2", math.Log2)
	members["fmod"] = members["mod"]
	members["inf"] = starlark.Float(math.Inf(1))
	members["nan"] = starlark.Float(math.NaN())
	members["tau"] = starlark.Float(2 * math.Pi)
	members["isclose"] = builtin("isclose", mathIsClose)
	members["isfinite"] = floatTest("isfinite", func(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) })
	members["isinf"] = floatTest("isinf", func(f float64) bool { return math.IsInf(f, 0) })
	members["isnan"] = floatTest("isnan", math.IsNaN)
	members["trunc"] = builtin("trunc", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var x starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
			return nil, err
		}
		switch x := boolAsInt(x).(type) {
		case starlark.Int:
			return x, nil
		case starlark.Float:
			if math.IsInf(float64(x), 0) || math.IsNaN(float64(x)) {
				return nil, errors.New("cannot convert float infinity or NaN to integer")
			}
			return starlark.NumberToInt(x)
		}
		return nil, fmt.Errorf("type %s doesn't define __trunc__ method", pyTypeName(x))
	})
	members["factorial"] = builtin("factorial", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var n int
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &n); err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errors.New("factorial() not defined for negative values")
		}
		if n > 20000 {
			return nil, errors.New(tooLargeNumber)
		}
		return starlark.MakeBigInt(new(big.Int).MulRange(1, int64(n))), nil
	})
	members["gcd"] = builtin("gcd", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(kwargs) > 0 {
			return nil, errors.New("gcd() takes no keyword arguments")
		}
		result := new(big.Int)
		for _, arg := range args {
			n, ok := boolAsInt(arg).(starlark.Int)
			if !ok {
				return nil, fmt.Errorf("'%s' object cannot be interpreted as an integer", pyTypeName(arg))
			}
			result.GCD(nil, nil, result, new(big.Int).Abs(n.BigInt()))
		}
		return starlark.MakeBigInt(result), nil
	})
	module := &starlarkstruct.Module{Name: "math", Members: members}
	module.Freeze()
	return module
}

// mathErrors makes a math function fail like Python's: "math domain error"
// when the result is not a number, "math range error" when it is too large.
func mathErrors(fn *starlark.Builtin) *starlark.Builtin {
	// At a pole the function goes to infinity: Python calls that a domain
	// error, not a range error. pow(0, -1) is a pole too.
	pole := map[string]bool{"log": true, "log10": true, "log2": true, "atanh": true}[fn.Name()]
	return builtin(fn.Name(), func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		result, err := fn.CallInternal(thread, args, kwargs)
		if err != nil {
			return nil, err
		}
		f, ok := result.(starlark.Float)
		if !ok {
			return result, nil
		}
		finiteArgs, nanArg, zeroBase := true, false, false
		for i, arg := range args {
			if x, ok := starlark.AsFloat(arg); ok {
				finiteArgs = finiteArgs && !math.IsInf(x, 0) && !math.IsNaN(x)
				nanArg = nanArg || math.IsNaN(x)
				zeroBase = zeroBase || i == 0 && x == 0
			}
		}
		atPole := pole || fn.Name() == "pow" && zeroBase
		switch {
		case math.IsNaN(float64(f)) && !nanArg:
			return nil, errors.New("math domain error")
		case math.IsInf(float64(f), 0) && finiteArgs && atPole:
			return nil, errors.New("math domain error")
		case math.IsInf(float64(f), 0) && finiteArgs:
			return nil, errors.New("math range error")
		}
		return result, nil
	})
}

func floatTest(name string, test func(float64) bool) *starlark.Builtin {
	return builtin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var x starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
			return nil, err
		}
		f, ok := starlark.AsFloat(x)
		if !ok {
			return nil, fmt.Errorf("must be real number, not %s", pyTypeName(x))
		}
		return starlark.Bool(test(f)), nil
	})
}

// mathIsClose is Python's math.isclose(a, b, *, rel_tol=1e-09, abs_tol=0.0).
func mathIsClose(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var a, c starlark.Value
	var relTol, absTol starlark.Value = starlark.Float(1e-9), starlark.Float(0)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "a", &a, "b", &c, "rel_tol?", &relTol, "abs_tol?", &absTol); err != nil {
		return nil, err
	}
	x, ok1 := starlark.AsFloat(a)
	y, ok2 := starlark.AsFloat(c)
	rel, ok3 := starlark.AsFloat(relTol)
	abso, ok4 := starlark.AsFloat(absTol)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, errors.New("must be real number")
	}
	if rel < 0 || abso < 0 {
		return nil, errors.New("tolerances must be non-negative")
	}
	if x == y {
		return starlark.True, nil
	}
	if math.IsInf(x, 0) || math.IsInf(y, 0) {
		return starlark.False, nil
	}
	diff := math.Abs(y - x)
	return starlark.Bool(diff <= math.Abs(rel*y) || diff <= math.Abs(rel*x) || diff <= abso), nil
}
