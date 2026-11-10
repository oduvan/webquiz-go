package checker

import (
	"strings"
	"testing"
	"time"

	"go.starlark.net/starlark"
)

func pyList(values ...starlark.Value) *starlark.List { return starlark.NewList(values) }

// The expected texts come from Python 3.13: template.format(*args, **kwargs).
func TestPythonStrFormat(t *testing.T) {
	i := func(n int64) starlark.Value { return starlark.MakeInt64(n) }
	s := func(x string) starlark.Value { return starlark.String(x) }
	f := func(x float64) starlark.Value { return starlark.Float(x) }
	kw := func(pairs ...any) []starlark.Tuple {
		var out []starlark.Tuple
		for k := 0; k < len(pairs); k += 2 {
			out = append(out, starlark.Tuple{starlark.String(pairs[k].(string)), pairs[k+1].(starlark.Value)})
		}
		return out
	}
	cases := []struct {
		template string
		args     starlark.Tuple
		kwargs   []starlark.Tuple
		want     string
		err      string
	}{
		{"{}-{}", starlark.Tuple{i(1), s("a")}, nil, "1-a", ""},
		{"{0}{1}{0}", starlark.Tuple{s("x"), s("y")}, nil, "xyx", ""},
		{"{name}!", nil, kw("name", s("Kyiv")), "Kyiv!", ""},
		{"{:.2f}", starlark.Tuple{f(3.14159)}, nil, "3.14", ""},
		{"{:>8}|{:<6}|{:^7}", starlark.Tuple{s("a"), s("b"), s("c")}, nil, "       a|b     |   c   ", ""},
		{"{:,}", starlark.Tuple{i(1234567)}, nil, "1,234,567", ""},
		{"{!r} {!s} {!a}", starlark.Tuple{s("é"), s("é"), s("é")}, nil, "'é' é '\\xe9'", ""},
		{"{{}} {}", starlark.Tuple{i(5)}, nil, "{} 5", ""},
		{"{0[1]} {1[k]}", starlark.Tuple{pyList(i(10), i(20)), pyDict(t, s("k"), s("v"))}, nil, "20 v", ""},
		{"{:{w}.{p}f}", starlark.Tuple{f(2.71828)}, kw("w", i(8), "p", i(3)), "   2.718", ""},
		{"{} {}", starlark.Tuple{f(1.0), f(1e16)}, nil, "1.0 1e+16", ""},
		{"{}", starlark.Tuple{pyList(i(1), s("a"))}, nil, "[1, 'a']", ""},
		{"{:%}", starlark.Tuple{f(0.5)}, nil, "50.000000%", ""},
		{"{x:>{w}}", nil, kw("x", s("ab"), "w", i(5)), "   ab", ""},
		{"{a[0]}", nil, kw("a", pyList(i(9))), "9", ""},
		{"{} {0}", starlark.Tuple{i(1)}, nil, "", "cannot switch from automatic field numbering to manual field specification"},
		{"{0} {}", starlark.Tuple{i(1)}, nil, "", "cannot switch from manual field specification to automatic field numbering"},
		{"{2}", starlark.Tuple{i(1)}, nil, "", "Replacement index 2 out of range for positional args tuple"},
		{"{name}", nil, nil, "", "'name'"},
		{"{", nil, nil, "", "Single '{' encountered in format string"},
		{"}", nil, nil, "", "Single '}' encountered in format string"},
		{"{!x}", starlark.Tuple{i(1)}, nil, "", "Unknown conversion specifier x"},
		{"{:d}", starlark.Tuple{s("a")}, nil, "", "Unknown format code 'd' for object of type 'str'"},
	}
	for _, c := range cases {
		got, err := pyFormatString(c.template, c.args, c.kwargs)
		if c.err != "" {
			if err == nil || err.Error() != c.err {
				t.Errorf("%q.format(%v) = %q, %v; want error %q", c.template, c.args, got, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q.format(%v) = %q, %v; want %q", c.template, c.args, got, err, c.want)
		}
	}
}

// The expected texts come from Python 3.13: template % values.
func TestPythonPercentFormat(t *testing.T) {
	i := func(n int64) starlark.Value { return starlark.MakeInt64(n) }
	s := func(x string) starlark.Value { return starlark.String(x) }
	f := func(x float64) starlark.Value { return starlark.Float(x) }
	tuple := func(v ...starlark.Value) starlark.Value { return starlark.Tuple(v) }
	cases := []struct {
		template string
		values   starlark.Value
		want     string
		err      string
	}{
		{"%s and %s", tuple(s("a"), i(1)), "a and 1", ""},
		{"%d%%", tuple(i(50)), "50%", ""},
		{"%5d|%-5d|%05d|%+d|% d", tuple(i(42), i(42), i(42), i(42), i(42)), "   42|42   |00042|+42| 42", ""},
		{"%.2f %e %g %G", tuple(f(3.14159), f(12345.678), f(0.0001), f(1e20)), "3.14 1.234568e+04 0.0001 1E+20", ""},
		{"%x %X %#x %#o %o", tuple(i(255), i(255), i(255), i(8), i(8)), "ff FF 0xff 0o10 10", ""},
		{"%r %a", tuple(s("é"), s("é")), "'é' '\\xe9'", ""},
		{"%c%c", tuple(i(65), s("b")), "Ab", ""},
		{"%(name)s is %(age)d", pyDict(t, s("name"), s("Ann"), s("age"), i(30)), "Ann is 30", ""},
		{"%*d|%-*s|%.*f", tuple(i(5), i(1), i(4), s("ab"), i(2), f(3.14159)), "    1|ab  |3.14", ""},
		{"%5.1s|", tuple(s("abc")), "    a|", ""},
		{"%s", tuple(pyList(i(1), s("a"))), "[1, 'a']", ""},
		{"%s", tuple(pyDict(t, s("a"), i(1))), "{'a': 1}", ""},
		{"%d", tuple(f(3.7)), "3", ""},
		{"%i %u", tuple(i(7), i(8)), "7 8", ""},
		{"%s", s("single"), "single", ""},
		{"%.3s", s("abcdef"), "abc", ""},
		{"%10.4f|", tuple(f(3.14159)), "    3.1416|", ""},
		{"%-10s|", tuple(s("ab")), "ab        |", ""},
		{"%#.0f %#g", tuple(f(1.0), f(1.5)), "1. 1.50000", ""},
		{"abc", tuple(), "abc", ""},
		{"%s", tuple(tuple(i(1), i(2))), "(1, 2)", ""},
		{"%d", tuple(s("5")), "", "%d format: a real number is required, not str"},
		{"%x", tuple(f(1.5)), "", "%x format: an integer is required, not float"},
		{"%s %s", tuple(s("a")), "", "not enough arguments for format string"},
		{"%s", tuple(s("a"), s("b")), "", "not all arguments converted during string formatting"},
		{"%q", tuple(i(1)), "", "unsupported format character 'q' (0x71) at index 1"},
		{"%", tuple(), "", "incomplete format"},
		{"%(a)s", tuple(s("x")), "", "format requires a mapping"},
		{"%f", tuple(s("x")), "", "must be real number, not str"},
		{"%c", tuple(i(1114112)), "", "%c arg not in range(0x110000)"},
		{"%c", tuple(s("ab")), "", "%c requires int or char"},
		{"%s", tuple(), "", "not enough arguments for format string"},
		{"abc", tuple(s("x")), "", "not all arguments converted during string formatting"},
		{"%s", tuple(i(1), i(2)), "", "not all arguments converted during string formatting"},
	}
	for _, c := range cases {
		got, err := pyPercentFormat(c.template, c.values)
		if c.err != "" {
			if err == nil || err.Error() != c.err {
				t.Errorf("%q %% %v = %q, %v; want error %q", c.template, c.values, got, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q %% %v = %q, %v; want %q", c.template, c.values, got, err, c.want)
		}
	}
}

// TestFormatLowerBound checks that the quick size estimate never says more
// than the real text, and that it is quick for many "{{" or a lone "{".
func TestFormatLowerBound(t *testing.T) {
	args := starlark.Tuple{starlark.String("abc"), starlark.MakeInt(12345)}
	kwargs := []starlark.Tuple{{starlark.String("k"), starlark.String("xy")}}
	for _, template := range []string{
		"{}{}", "{0}{0}{1}", "{k}{{}}{}", "a{{b}}c{}", "{0:>9}{1!r}", "{0[0]}", "{:.1}{}",
	} {
		fa := &formatArgs{args: args, kwargs: kwargs}
		bound := fa.lowerBound(template)
		text, err := pyFormatString(template, args, kwargs)
		if err != nil {
			t.Fatalf("%q: %v", template, err)
		}
		if bound > uint64(len(text)) {
			t.Errorf("lowerBound(%q) = %d, more than the %d bytes of %q", template, bound, len(text), text)
		}
	}
	for _, template := range []string{strings.Repeat("{{", 500000), strings.Repeat("{", 500000)} {
		start := time.Now()
		(&formatArgs{}).lowerBound(template)
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Errorf("lowerBound took %v for %d bytes", elapsed, len(template))
		}
	}
	if bound := percentLowerBound("%s-%s%%", starlark.Tuple{starlark.String("ab"), starlark.MakeInt(7)}); bound != 4 {
		t.Errorf("percentLowerBound = %d, want 4 (the real text is ab-7%%)", bound)
	}
}
