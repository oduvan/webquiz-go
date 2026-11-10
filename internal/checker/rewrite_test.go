package checker

import (
	"strings"
	"testing"
)

// rewrittenBody returns the generated Starlark lines of the teacher's code,
// without the wrapper function around them and without its indentation.
func rewrittenBody(t *testing.T, code string) string {
	t.Helper()
	src, err := rewrite(code)
	if err != nil {
		t.Fatalf("rewrite(%q) failed: %v", code, err)
	}
	lines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	if lines[0] != wrapperHeader || lines[len(lines)-1] != wrapperCall || lines[len(lines)-2] != "    pass" {
		t.Fatalf("rewrite(%q) has no wrapper:\n%s", code, src)
	}
	var body []string
	for _, line := range lines[1 : len(lines)-2] {
		body = append(body, strings.TrimPrefix(line, "    "))
	}
	return strings.Join(body, "\n")
}

func TestRewrite(t *testing.T) {
	cases := []struct {
		name, code, want string
	}{
		// assert
		{"assert", "assert x", "_assert_(x)"},
		{"assert with message", `assert x == 1, "Expected 1"`, `_assert_(x == 1, lambda: ("Expected 1"))`},
		{"assert with parentheses", "assert(x)", "_assert_((x))"},
		{"assert of a tuple", `assert(x, "m")`, `_assert_((x, "m"))`},
		{"one-line if with assert", `if x: assert y, "m"`, `if x: _assert_(y, lambda: ("m"))`},
		{"assert after a semicolon", "a = 1; assert a", "a = 1; _assert_(a)"},
		{"assert on several lines", "assert (\n    a ==\n    b\n), \"m\"", "_assert_((\na ==\nb\n), lambda: (\"m\"))"},
		{"assert message on the next line", "assert a, \\\n    \"m\"", "_assert_(a, lambda: (\n\"m\"))"},
		{"backslash line join outside brackets", "x = 1 + \\\n    2", "x = 1 + \\\n2"},

		// f-strings
		{"f-string", `x = f"got {v}!"`, `x = ("got " + _fmt_((v), "", "") + "!")`},
		{"f-string only a field", `x = f"{v}"`, `x = _fmt_((v), "", "")`},
		{"f-string prefixes", `x = F"{a}" + rf"\d{b}" + fR'{c}'`, `x = _fmt_((a), "", "") + ("\\d" + _fmt_((b), "", "")) + _fmt_((c), "", "")`},
		{"f-string braces", `x = f"{{a}} {b}"`, `x = ("{a} " + _fmt_((b), "", ""))`},
		{"f-string conversion", `x = f"{v!r}"`, `x = _fmt_((v), "r", "")`},
		{"f-string precision", `x = f"{v:.2f}"`, `x = _fmt_((v), "", ".2f")`},
		{"f-string width", `x = f"{v:>8}"`, `x = _fmt_((v), "", ">8")`},
		{"f-string grouping", `x = f"{v:,}"`, `x = _fmt_((v), "", ",")`},
		{"f-string nested spec", `x = f"{v:{w}.2f}"`, `x = _fmt_((v), "", (_fmt_((w), "", "") + ".2f"))`},
		{"f-string nested quotes", `x = f"{d['k']}" + f'{d["k"]}'`, `x = _fmt_((d["k"]), "", "") + _fmt_((d["k"]), "", "")`},
		{"f-string same quotes inside", `x = f"{d["k"]}"`, `x = _fmt_((d["k"]), "", "")`},
		{"nested f-string", `x = f"a{f'<{v}>'}b"`, `x = ("a" + _fmt_((("<" + _fmt_((v), "", "") + ">")), "", "") + "b")`},
		{"triple-quoted f-string", "x = f'''a\n{v}\nb'''\ny = 1", "x = (\"a\\n\" + _fmt_((v), \"\", \"\") + \"\\nb\")\n\n\ny = 1"},
		{"f-string debug", `x = f"{v=}"`, `x = ("v=" + _fmt_((v), "r", ""))`},
		{"f-string with power inside", `x = f"{a ** 2}"`, `x = _fmt_((pow(a, 2)), "", "")`},
		{"f-string tuple", `x = f"{a, b}"`, `x = _fmt_((a, b), "", "")`},

		// **
		{"power", "y = a ** b", "y = pow(a, b)"},
		{"power is right-associative", "y = 2 ** 3 ** 2", "y = pow(2, pow(3, 2))"},
		{"unary minus before power", "y = -2 ** 2", "y = -pow(2, 2)"},
		{"unary minus in exponent", "y = 2 ** -1", "y = pow(2, -1)"},
		{"power of a call and subscript", "y = a.b(c)[d] ** 2", "y = pow(a.b(c)[d], 2)"},
		{"power binds tighter than times", "y = 2 * x ** 2 + 1", "y = _mul_(2, pow(x, 2)) + 1"},
		{"power assignment", "x **= 2", "x = pow(x, 2)"},

		// * goes through _mul_, which refuses results that are too large
		{"times", "y = a * b * c / d", "y = _mul_(_mul_(a, b), c) / d"},
		{"times assignment", "x *= 3", "x = _mul_(x, 3)"},

		// comparisons
		{"single comparison", "ok = a < b", "ok = a < b"},
		{"chained comparison", "ok = 0 < x < 10", "ok = (0 < x and x < 10)"},
		{"long chain", "ok = a <= b == c != d", "ok = (a <= b and b == c and c != d)"},
		{"chain with in and not in", "ok = a in b not in c", "ok = (a in b and b not in c)"},
		{"chain in a comprehension", "v = [x for x in xs if 0 < x < 5]", "v = [x for x in _iter_(xs) if (0 < x and x < 5)]"},
		{"chain in a lambda", "f = lambda x: 0 <= x <= 1", "f = lambda x: (0 <= x and x <= 1)"},
		{"chain with arithmetic", "ok = 0.49 < x * 2 - 1 < 0.51", "ok = (0.49 < _mul_(x, 2) - 1 and _mul_(x, 2) - 1 < 0.51)"},
		{"is", "ok = x is None", "ok = x == None"},
		{"is not", "ok = x is not None", "ok = x != None"},
		{"not in and not", "ok = not a not in b", "ok = not a not in b"},

		// strings and literals
		{"string prefixes", `x = u"a" + R"\d" + B"b"`, `x = "a" + "\\d" + b"b"`},
		{"adjacent strings", `x = ("ab" 'cd')`, `x = ("abcd")`},
		{"adjacent string and f-string", `x = "a" f"{b}"`, `x = ("a" + _fmt_((b), "", ""))`},
		{"set literal", `ok = x in {"a", "b"}`, `ok = x in set(["a", "b"])`},
		{"set comprehension", "s = {c for c in t}", "s = set([c for c in _iter_(t)])"},
		{"dict", `d = {"a": 1, "b": {}}`, `d = {"a": 1, "b": {}}`},
		{"dict comprehension", "d = {k: v for k, v in items}", "d = {k: v for k, v in _iter_(items)}"},
		{"generator in a call", "ok = all(c.isdigit() for c in s)", "ok = all([c.isdigit() for c in _iter_(s)])"},
		{"generator in parentheses", "g = (x * 2 for x in xs if x)", "g = [_mul_(x, 2) for x in _iter_(xs) if x]"},
		{"numbers with underscores", "n = 1_000_000", "n = 1000000"},
		{"load is not a keyword", "load = 5", "_load_ = 5"},

		// statements
		{"strip the answer", "user_answer = user_answer.strip()", "user_answer = user_answer.strip()"},
		{"for loop", "for c in user_answer:\n    n += 1", "for c in _iter_(user_answer):\n    n += 1"},
		{"for loop over a tuple", "for v in 1, 2:\n    pass", "for v in _iter_((1, 2)):\n    pass"},
		{"chained assignment", "a = b = 0", "_chained_value_ = 0; a = _chained_value_; b = _chained_value_"},
		{"annotated assignment", "x: int = 5\ny: str", "x = 5\npass"},
		{"type hints in def", "def f(a: str, b: int = 1) -> bool:\n    return a", "def f(a, b = 1):\n    return a"},
		{"method that Starlark lacks", "words.sort(key=len)", `_attr_(words, "sort")(key = len)`},
		{"lines inside a multi-line string keep their text", "x = '''a\n  b\nc'''\nassert x", "x = \"a\\n  b\\nc\"\n\n\n_assert_(x)"},
		{"blank lines and comments keep line numbers", "# check\n\nx = 1  # one\n\ny = 2", "\n\nx = 1\n\ny = 2"},
		{"indentation with tabs", "if x:\n\ty = 1", "if x:\n        y = 1"},
	}
	for _, c := range cases {
		got := rewrittenBody(t, c.code)
		if got != c.want {
			t.Errorf("%s:\ncode:\n%s\n got:\n%s\nwant:\n%s", c.name, c.code, got, c.want)
		}
	}
}

func TestRewriteWrapsCode(t *testing.T) {
	src, err := rewrite("x = 1")
	if err != nil {
		t.Fatal(err)
	}
	want := wrapperHeader + "\n    x = 1\n    pass\n" + wrapperCall + "\n"
	if src != want {
		t.Errorf("got:\n%s\nwant:\n%s", src, want)
	}
}

func TestRewriteErrors(t *testing.T) {
	cases := []struct {
		code string
		msg  string
		line int
	}{
		{"if True\n    print('missing colon')", "expected ':'", 1},
		{"def broken(\n    pass", "'(' was never closed", 1},
		{"x = 1\n    y = 2", "unexpected indent", 2},
		{"  x = 1", "unexpected indent", 1},
		{"if x:\n    y = 1\n  z = 2", "unindent does not match any outer indentation level", 3},
		{"if x:\ny = 1", "expected an indented block after 'if' statement on line 1", 2},
		{"x = 1\ndef f():\n", "expected an indented block after function definition on line 2", 2},
		{"return 5", "'return' outside function", 1},
		{"if x: return", "'return' outside function", 1},
		{"try:\n    x = 1\nexcept ValueError:\n    x = 2", "'try' is not supported in checker code", 1},
		{"raise ValueError('no')", "'raise' is not supported in checker code", 1},
		{"import math", "'import' is not supported in checker code", 1},
		{"class A:\n    pass", "'class' is not supported in checker code", 1},
		{"with open('f') as f:\n    pass", "'with' is not supported in checker code", 1},
		{"global x", "'global' is not supported in checker code", 1},
		{"del x", "'del' is not supported in checker code", 1},
		{"for x in y:\n    pass\nelse:\n    pass", "'else' after 'for' or 'while' is not supported in checker code", 3},
		{"x = (y := 5)", "':=' is not supported in checker code", 1},
		{"x = a @ b", "'@' is not supported in checker code", 1},
		{"x = ...", "'...' is not supported in checker code", 1},
		{"assert x, 1, 2", "invalid syntax", 1},
		{"x = = 1", "invalid syntax", 1},
		{"x = 1 +", "invalid syntax", 1},
		{"x = b'a' 'b'", "cannot mix bytes and nonbytes literals", 1},
		{"x = 1\ny = f'{1 +}'", "invalid syntax", 2},
		{"x = 1\n\ny = 'abc", "unterminated string literal (detected at line 3)", 3},
	}
	for _, c := range cases {
		_, err := rewrite(c.code)
		if err == nil {
			t.Errorf("rewrite(%q) did not fail, want %q", c.code, c.msg)
			continue
		}
		if err.Msg != c.msg || err.Line != c.line {
			t.Errorf("rewrite(%q) = %q on line %d, want %q on line %d", c.code, err.Msg, err.Line, c.msg, c.line)
		}
	}
}
