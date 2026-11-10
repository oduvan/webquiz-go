package checker

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckEmptyCodeIsCorrect(t *testing.T) {
	// The caller uses CheckExact for an empty checker; Check itself accepts.
	if got := Check("", "anything"); !got.Correct || got.Error != "" {
		t.Errorf("Check(\"\") = %+v, want correct", got)
	}
}

func TestCheckExact(t *testing.T) {
	cases := []struct {
		answer, correct string
		want            bool
	}{
		{"test", "test", true},
		{"  test \n", "test", true},
		{"test", "  test  ", true},
		{"Test", "test", false},
		{"", "", true},
		{"a b", "a  b", false},
	}
	for _, c := range cases {
		if got := CheckExact(c.answer, c.correct); got != c.want {
			t.Errorf("CheckExact(%q, %q) = %v, want %v", c.answer, c.correct, got, c.want)
		}
	}
}

type checkCase struct {
	name, code, answer string
	correct            bool
	err                string // the exact error text when not correct
}

func runCheckCases(t *testing.T, cases []checkCase) {
	t.Helper()
	for _, c := range cases {
		got := Check(c.code, c.answer)
		if got.Correct != c.correct || got.Error != c.err {
			t.Errorf("%s: Check(%q, %q) = %+v, want {Correct:%v Error:%q}", c.name, c.code, c.answer, got, c.correct, c.err)
		}
	}
}

func TestCheckAssert(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"passes", "assert user_answer.strip() == '4', 'Expected 4'", " 4 ", true, ""},
		{"fails with message", "assert user_answer.strip() == '4', 'Expected 4'", "5", false, "Expected 4"},
		{"fails without message", "assert user_answer == 'x'", "y", false, ""},
		{"message only built on failure", "assert True, 1 / 0", "", true, ""},
		{"message is any value", "assert False, ['a', 1]", "", false, "['a', 1]"},
		{"one-line if", "n = int(user_answer)\nif n > 10: assert n % 2 == 0, 'even please'", "13", false, "even please"},
		{"one-line if not taken", "n = int(user_answer)\nif n > 10: assert n % 2 == 0, 'even please'", "3", true, ""},
		{"several lines", "assert (\n    user_answer.lower()\n    == 'kyiv'\n), 'Wrong city'", "Lviv", false, "Wrong city"},
		{"in parentheses", "assert(user_answer == '42')", "42", true, ""},
		{"tuple in parentheses always passes, as in Python", "assert(user_answer == '42', 'no')", "41", true, ""},
		{"inside a loop", "for w in user_answer.split():\n    assert w.isalpha(), f'{w} is not a word'", "ab c1", false, "c1 is not a word"},
	})
}

func TestCheckFStrings(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"all prefixes", `assert False, f"{1}" + F"{2}" + rf"\{3}" + fR"{4}" + Rf'{5}' + FR'{6}'`, "", false, `12\3456`},
		{"double braces", `assert False, f"{{x}} {1}}}"`, "", false, "{x} 1}"},
		{"repr", `assert False, f"got {user_answer!r}"`, "it's", false, `got "it's"`},
		{"str and ascii", `assert False, f"{user_answer!s}|{user_answer!a}"`, "é", false, `é|'\xe9'`},
		{"precision", "x = float(user_answer)\nassert False, f'{x:.2f}'", "3.14159", false, "3.14"},
		{"width", "assert False, f'[{user_answer:>8}]'", "abc", false, "[     abc]"},
		{"grouping", "assert False, f'{int(user_answer):,}'", "1234567", false, "1,234,567"},
		{"nested quotes", `d = {"k": user_answer}` + "\nassert False, f\"{d['k']}/{d[\"k\"]}\"", "v", false, "v/v"},
		{"nested f-string", `assert False, f"<{f'[{user_answer}]'}>"`, "x", false, "<[x]>"},
		{"nested spec", "w = 6\nassert False, f'{3.14159:{w}.{2}f}|'", "", false, "  3.14|"},
		{"triple quotes", "assert False, f'''line {1}\nline {2}'''", "", false, "line 1\nline 2"},
		{"debug", "x = 5\nassert False, f'{x=} {x = :>3}'", "", false, "x=5 x =   5"},
		{"floats like Python", "assert False, f'{1e15} {1e16} {2.5} {1/3}'", "", false, "1000000000000000.0 1e+16 2.5 0.3333333333333333"},
		{"lists like Python", "assert False, f'{sorted(user_answer.split())}'", "b a", false, "['a', 'b']"},
	})
}

func TestCheckPower(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"right-associative", "assert 2 ** 3 ** 2 == 512", "", true, ""},
		{"unary minus", "assert -2 ** 2 == -4", "", true, ""},
		{"negative exponent", "assert 2 ** -1 == 0.5", "", true, ""},
		{"call and index", "a = {'b': lambda c: [c, c + 1]}\nassert a['b'](2)[1] ** 2 == 9", "", true, ""},
		{"float power", "assert abs(2 ** 0.5 - 1.41421356) < 1e-6", "", true, ""},
		{"big integers", "assert 2 ** 100 == 1267650600228229401496703205376", "", true, ""},
		{"zero to a negative power", "x = 0 ** -1", "", false, "0.0 cannot be raised to a negative power"},
		{"power assignment", "x = 3\nx **= 2\nassert x == 9", "", true, ""},
	})
}

func TestCheckComparisons(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"range", "x = float(user_answer)\nassert 0.49 < x < 0.51, 'out of range'", "0.5", true, ""},
		{"out of range", "x = float(user_answer)\nassert 0.49 < x < 0.51, 'out of range'", "0.6", false, "out of range"},
		{"in and not in", "assert 'a' in 'abc' not in ['x']", "", true, ""},
		{"in comprehension", "assert [x for x in range(10) if 2 < x < 5] == [3, 4]", "", true, ""},
		{"in lambda", "ok = lambda v: 0 <= v <= 1\nassert ok(0.5) and not ok(2)", "", true, ""},
		{"middle evaluated twice is fine", "assert 1 < len(user_answer) < 4", "abc", true, ""},
		{"is None", "v = None\nassert v is None and v is not 0", "", true, ""},
		// Starlark puts NaN above every number; Python says every comparison
		// with NaN is false, except "!=". A student must not pass x > 0 with "nan".
		{"nan is not greater", "x = float(user_answer)\nassert x > 0", "nan", false, ""},
		{"nan is not equal to itself", "x = float(user_answer)\nassert not (x == x) and x != x and not (x <= 1e308)", "nan", true, ""},
		{"nan with an int", "x = float(user_answer)\nassert not (x > 1) and not (1 < x) and not (0 <= x <= 2)", "nan", true, ""},
		{"comparison errors stay", "assert 1 < 'a'", "", false, "int < string not implemented"},
		// Python raises TypeError for NaN against text or None; == and != work.
		{"nan against text", "x = float('nan') < 'a'", "", false, "float < string not implemented"},
		{"nan against None", "x = None > float('nan')", "", false, "NoneType > float not implemented"},
		{"nan equality with other types", "assert not (float('nan') == 'a') and float('nan') != None", "", true, ""},
	})
}

func TestCheckStatements(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"strip the answer", "user_answer = user_answer.strip()\nassert user_answer == 'ok'", "  ok\n", true, ""},
		{"multi-line string keeps its text", "expected = '''first\n  second\nthird'''\nassert user_answer == expected", "first\n  second\nthird", true, ""},
		{"top-level if, for and while", "n = 0\nfor c in user_answer:\n    if c.isdigit():\n        n += 1\ni = 0\nwhile i < 3:\n    i += 1\nassert n == 2 and i == 3", "a1b2", true, ""},
		{"helper function", "def norm(s):\n    return s.strip().lower()\nassert norm(user_answer) == 'kyiv'", " KYIV ", true, ""},
		{"chained assignment", "a = b = 0\nb += 1\nassert (a, b) == (0, 1)", "", true, ""},
		{"type hints", "def half(v: float) -> float:\n    return v / 2\nx: float = half(float(user_answer))\nassert x == 1.5", "3", true, ""},
		{"set literal", "assert user_answer.lower() in {'yes', 'так'}", "ТАК", true, ""},
		{"generator in any and all", "s = user_answer\nassert all(c.isdigit() for c in s) and any(c == '7' for c in s)", "172", true, ""},
		{"sum of a generator", "assert sum(int(d) for d in user_answer) == 10", "1234", true, ""},
		{"text as a list", "assert sorted(user_answer) == sorted('listen')", "silent", true, ""},
		{"list sort and count", "w = user_answer.split()\nw.sort()\nassert w == ['a', 'b'] and w.count('a') == 1", "b a", true, ""},
		{"string methods Starlark lacks", "assert user_answer.isnumeric() and user_answer.zfill(5) == '00042'", "42", true, ""},
		{"float is_integer", "assert float(user_answer).is_integer()", "4.0", true, ""},
		{"print does nothing", "print('checking', user_answer, end='')\nassert True", "", true, ""},
		// Like Python, *text, += text and extend(text) give the characters.
		{"text as arguments", "def f(*a):\n    return a\nassert f(*user_answer) == ('o', 'k')", "ok", true, ""},
		{"text added to a list", "x = [1]\nx += user_answer\nx.extend('cd')\nassert x == [1, 'a', 'b', 'c', 'd']", "ab", true, ""},
		{"+= keeps the same list", "x = [1]\ny = x\nx += [2]\nassert y == [1, 2]", "", true, ""},
	})
}

func TestCheckBuiltins(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"int strips spaces", "assert int(user_answer) == 42", " 42\n", true, ""},
		{"int error like Python", "x = int(user_answer)", "3.14", false, "invalid literal for int() with base 10: '3.14'"},
		{"float error like Python", "x = float(user_answer)", "abc", false, "could not convert string to float: 'abc'"},
		{"float with spaces", "assert float(user_answer) == 2.5", " 2.5 ", true, ""},
		// int(text, base): the expected results and errors are Python's.
		{"int with a base", "assert int('ff', 16) == 255 and int('0xff', 0) == 255 and int('  ff  ', 16) == 255 and int('z', 36) == 35 and int('1_2', 7) == 9 and int('\u0661\u0662', 7) == 9 and int('0', 0) == 0", "", true, ""},
		{"int with a base and a bad digit", "x = int('13', 3)", "", false, "invalid literal for int() with base 3: '13'"},
		{"int with base 0 and a leading zero", "x = int('010', 0)", "", false, "invalid literal for int() with base 0: '010'"},
		{"int with a bad base", "x = int('12', 37)", "", false, "int() base must be >= 2 and <= 36, or 0"},
		{"int with a base of two has no digit limit", "assert int('7' * 4301, 8) > 0", "", true, ""},
		{"int with another base has the digit limit", "x = int('6' * 4301, 7)", "", false, "Exceeds the limit (4300 digits) for integer string conversion: value has 4301 digits; use sys.set_int_max_str_digits() to increase the limit"},
		{"round half to even", "assert round(2.5) == 2 and round(3.5) == 4 and round(2.675, 2) == 2.67", "", true, ""},
		{"round to tens", "assert round(1250, -2) == 1200 and round(1351.0, -2) == 1400.0", "", true, ""},
		{"pow with modulus", "assert pow(3, 2, 7) == 2 and pow(2, 10) == 1024", "", true, ""},
		{"sum of floats like Python 3.12+", "assert sum([0.1] * 10) == 1.0", "", true, ""},
		{"sum of booleans", "assert sum([True, True, False]) == 2", "", true, ""},
		{"sum of strings", "x = sum(['a', 'b'], '')", "", false, "sum() can't sum strings [use ''.join(seq) instead]"},
		{"map and filter", "n = list(map(int, user_answer.split(',')))\nassert list(filter(lambda v: v % 2 == 0, n)) == [2, 4]", "1,2,3,4", true, ""},
		{"isinstance", "assert isinstance(1, int) and isinstance(True, int) and isinstance(1.0, (int, float)) and not isinstance('1', int)", "", true, ""},
		{"type", "assert type(1) == int and type('a') is str", "", true, ""},
		{"frozenset", "assert frozenset('ab') == {'a', 'b'} and isinstance(frozenset([1]), frozenset)", "", true, ""},
		{"format", "assert format(3.14159, '.2f') == '3.14' and format(42, '>5') == '   42'", "", true, ""},
		{"str and repr like Python", "assert str(1e15) == '1000000000000000.0' and repr('a') == \"'a'\" and str([1, 'a']) == \"[1, 'a']\"", "", true, ""},
		{"min and max of text", "assert max(user_answer) == 'c' and min(user_answer) == 'a'", "bca", true, ""},
		{"enumerate and zip text", "assert list(enumerate('ab')) == [(0, 'a'), (1, 'b')] and list(zip('ab', 'cd')) == [('a', 'c'), ('b', 'd')]", "", true, ""},
		{"reversed text", "assert ''.join(reversed(user_answer)) == user_answer", "abba", true, ""},
		{"math", "assert math.sqrt(16) == 4 and math.floor(math.pi) == 3 and math.isclose(0.1 + 0.2, 0.3)", "", true, ""},
		{"math extras", "assert math.log10(1000) == 3 and math.factorial(5) == 120 and math.gcd(12, 18) == 6 and math.trunc(-2.5) == -2", "", true, ""},
		{"math domain error", "x = math.sqrt(-1)", "", false, "math domain error"},
		{"helpers", "assert to_int(user_answer) == 42 and distance('2км') == 2000 and direction_angle('20-30') == 2030", "42", true, ""},
		{"helper error", "assert distance(user_answer) == 2000", "abc", false, "Неверный формат ответа. Ожидается число, например: '2000', '2000м', '2км'."},
	})
}

func TestCheckRuntimeErrors(t *testing.T) {
	runCheckCases(t, []checkCase{
		// The example in the 1.19 docs uses sqrt without "math.": it fails
		// when it runs, as in Python, but the code still compiles.
		{"undefined name", "result = float(user_answer)\nassert abs(result - sqrt(16)) < 0.01", "4", false, "name 'sqrt' is not defined"},
		{"undefined name not reached", "if user_answer == 'never':\n    oops()\nassert True", "x", true, ""},
		{"used before assignment", "y = x\nx = 1", "", false, "name 'x' is not defined"},
		{"missing method", "x = [1].nosuch()", "", false, "list has no .nosuch field or method"},
		{"syntax error", "if True\n    pass", "", false, "expected ':' (line 1)"},
		{"unsupported statement", "try:\n    x = 1\nexcept ValueError:\n    pass", "", false, "'try' is not supported in checker code (line 1)"},
	})
}

// Python 1.19 ran checkers with exec() and separate globals and locals, so a
// helper function could not see other names of the checker and failed with
// NameError. The Go version runs the checker inside one function, so this
// works now. The package docs list this difference.
func TestHelpersCanUseEachOther(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"helper calls helper", "def a(x):\n    return x + 1\ndef b(x):\n    return a(x) * 2\nassert b(int(user_answer)) == 4", "1", true, ""},
		{"recursion", "def fact(n):\n    return 1 if n <= 1 else n * fact(n - 1)\nassert fact(int(user_answer)) == 120", "5", true, ""},
		{"helper reads user_answer", "def f():\n    return user_answer\nassert f() == 'x'", "x", true, ""},
		{"generator uses a checker variable", "allowed = 'abc'\nassert all(c in allowed for c in user_answer)", "cab", true, ""},
	})
}

func TestValidate(t *testing.T) {
	valid := []string{
		"assert user_answer.strip() == '4', 'Expected 4'",
		"result = float(user_answer)\nassert abs(result - sqrt(16)) < 0.01, f'Expected 4, got {result}'",
		"x = 1\n\n\n# comment\n",
		"def f(x):\n    return x\nassert f(1)",
		"   \n",
	}
	for _, code := range valid {
		if err := Validate(code); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", code, err)
		}
	}
	invalid := []struct {
		code string
		msg  string
		line int
	}{
		{"if True\n    print('missing colon')", "expected ':'", 1},
		{"def broken(\n    pass", "'(' was never closed", 1},
		{"x = 1\n\nbreak", "break not in a loop", 3},
		{"x = 1\ny = [1, 2]\nfor a, *b in [y]:\n    pass", "got '*', want primary expression", 3},
		{"return 1", "'return' outside function", 1},
		{"import math", "'import' is not supported in checker code", 1},
		{"x = 1\nif x:\n    pass\n    pass\ny = (\n", "'(' was never closed", 5},
	}
	for _, c := range invalid {
		err := Validate(c.code)
		if err == nil {
			t.Errorf("Validate(%q) = nil, want %q", c.code, c.msg)
			continue
		}
		if err.Msg != c.msg || err.Line != c.line {
			t.Errorf("Validate(%q) = %q on line %d, want %q on line %d", c.code, err.Msg, err.Line, c.msg, c.line)
		}
	}
}

func TestSyntaxErrorText(t *testing.T) {
	err := &SyntaxError{Msg: "expected ':'", Line: 3}
	if got := err.Error(); got != "expected ':' (line 3)" {
		t.Errorf("Error() = %q", got)
	}
}

func TestInfiniteLoopTimesOut(t *testing.T) {
	codes := []string{
		"while True: pass",
		"while True:\n    x = [i * 2 for i in range(100)]",
		"def spin():\n    while True:\n        pass\nspin()",
	}
	for _, code := range codes {
		start := time.Now()
		got := Check(code, "")
		elapsed := time.Since(start)
		if got.Correct {
			t.Errorf("Check(%q) is correct, want a failure", code)
		}
		if got.Error != tooManySteps && got.Error != timeoutTextFor(Timeout) {
			t.Errorf("Check(%q) error = %q, want a step or time limit error", code, got.Error)
		}
		if elapsed > 3*time.Second {
			t.Errorf("Check(%q) took %v, want under 3s", code, elapsed)
		}
	}
}

func TestTimeoutStopsChecker(t *testing.T) {
	old := Timeout
	Timeout = 50 * time.Millisecond
	defer func() { Timeout = old }()
	start := time.Now()
	got := Check("while True:\n    s = str(list(range(50)))", "")
	if got.Correct || got.Error != timeoutTextFor(Timeout) {
		t.Errorf("Check = %+v, want the timeout error %q", got, timeoutTextFor(Timeout))
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Check took %v, want about 50ms", elapsed)
	}
}

func TestDeepRecursionFails(t *testing.T) {
	start := time.Now()
	got := Check("def f(n):\n    return f(n + 1)\nf(0)", "")
	if got.Correct || got.Error != "maximum recursion depth exceeded" {
		t.Errorf("Check = %+v, want the recursion error", got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Check took %v", elapsed)
	}
	// Recursion that is not too deep works.
	if got := Check("def f(n):\n    return 0 if n == 0 else 1 + f(n - 1)\nassert f(500) == 500", ""); !got.Correct {
		t.Errorf("recursion of depth 500 failed: %+v", got)
	}
}

func TestHugeStringFails(t *testing.T) {
	cases := []struct {
		code string
		want []string // one of these errors
	}{
		{"s = 'x' * 10 ** 10", []string{tooMuchMemory}},
		{"s = 'x' * 10 ** 9", []string{tooMuchMemory}},
		{"x = [0] * 10 ** 8", []string{tooMuchMemory}},
		{"s = 'x'\ns *= 10 ** 9", []string{tooMuchMemory}},
		{"s = f'{1:0100000000,}'", []string{tooMuchMemory}},
		{"s = format('a', '>999999999')", []string{tooMuchMemory}},
		{"s = 'a'.zfill(2 ** 31 - 1)", []string{tooMuchMemory}},
		{"s = 'a'.ljust(10 ** 9)", []string{tooMuchMemory}},
		{"s = 'a'.center(10 ** 9, '*')", []string{tooMuchMemory}},
		{"x = list('x' * 60000000)", []string{tooMuchMemory}},
		{"x = str(['a' * 1000] * 5000)", []string{tooMuchMemory}},
		// Starlark's own text methods and operators, which have no size limit.
		{"s = 'a' * 1000000\nx = s.replace('a', s)", []string{tooMuchMemory}},
		{"big = 'a' * 1000000\nx = ''.join([big] * 1000)", []string{tooMuchMemory}},
		{"x = getattr('a' * 1000000, 'replace')('a', 'a' * 1000000)", []string{tooMuchMemory}},
		{"x = ('{0}' * 1000).format('a' * 100000)", []string{tooMuchMemory}},
		{"t = ('a' * 100000,) * 1000\nx = ('%s' * 1000) % t", []string{tooMuchMemory}},
		{"s = 'a' * 30000000\nx = s + s + s + s", []string{tooMuchMemory}},
		{"s = 'a' * 30000000\nx = f'{s}{s}{s}{s}'", []string{tooMuchMemory}},
		{"x = [1] * 3000000\nx += x", []string{tooMuchMemory}},
		{"x = [1] * 3000000\nx.extend(x)", []string{tooMuchMemory}},
		{"x = ('a,' * 30000000).split(',')", []string{tooMuchMemory}},
		{"x = []\nx.extend(range(10 ** 9))", []string{tooMuchMemory}},
		{"x = []\nx.extend(('a' * 60000000).elems())", []string{tooMuchMemory}},
		{"def f(*args):\n    return len(args)\nx = f(*range(10 ** 9))", []string{tooMuchMemory}},
		{"def f(*args):\n    return len(args)\nx = f(*('a' * 60000000))", []string{tooMuchMemory}},
		{"s = 'ab'\nwhile True:\n    s = s + s", []string{tooMuchMemory, timeoutTextFor(Timeout), tooManySteps}},
		{"x = list(range(10 ** 12))", []string{tooMuchMemory}},
	}
	for _, c := range cases {
		start := time.Now()
		got := Check(c.code, "")
		elapsed := time.Since(start)
		if got.Correct {
			t.Errorf("Check(%q) is correct, want a failure", c.code)
			continue
		}
		found := false
		for _, w := range c.want {
			found = found || got.Error == w
		}
		if !found {
			t.Errorf("Check(%q) error = %q, want one of %q", c.code, got.Error, c.want)
		}
		if elapsed > 3*time.Second {
			t.Errorf("Check(%q) took %v, want under 3s", c.code, elapsed)
		}
	}
}

func TestHugeNumberFails(t *testing.T) {
	for _, code := range []string{
		"x = 10 ** 10 ** 10",
		"n = 2\nwhile True:\n    n = n * n",
		"x = pow(3, 2 ** 9000, 2 ** 9000 + 7)",
	} {
		start := time.Now()
		got := Check(code, "")
		if got.Correct || got.Error != "the number is too large" {
			t.Errorf("Check(%q) = %+v, want the number error", code, got)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Check(%q) took %v", code, elapsed)
		}
	}
	// Big numbers that a checker may really use still work, and rounding
	// to a huge number of tens is quick.
	for _, code := range []string{
		"assert 2 ** 1000 * 3 ** 500 > 10 ** 500",
		"assert pow(3, 2 ** 2048 + 1, 2 ** 2048 + 7) > 0",
		"assert round(5, -10 ** 9) == 0 and round(-15, -1) == -20",
	} {
		start := time.Now()
		if got := Check(code, ""); !got.Correct {
			t.Errorf("Check(%q) = %+v", code, got)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Check(%q) took %v", code, elapsed)
		}
	}
}

// TestLoopOverLongTextIsLazy: a loop over text goes through the characters
// one by one, so leaving the loop early is quick even for a long text.
func TestLoopOverLongTextIsLazy(t *testing.T) {
	start := time.Now()
	got := Check("n = 0\nfor c in 'x' * 60000000:\n    n += 1\n    if n == 3:\n        break\nassert n == 3", "")
	if !got.Correct {
		t.Errorf("Check = %+v", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Check took %v", elapsed)
	}
}

// TestHugeFloatPrecisionFails: every digit of a formatted float takes about
// ten bytes of work memory, so the precision is limited much lower than
// widths are.
func TestHugeFloatPrecisionFails(t *testing.T) {
	for _, code := range []string{
		"x = format(1.5, '.60000000e')",
		"x = format(1.5, '.60000000f')",
		"x = format(1.5, '#.60000000g')",
		"x = f'{1.5:.60000000e}'",
		"x = '%.60000000f' % 1.5",
		"x = '{:.60000000f}'.format(1.5)",
	} {
		start := time.Now()
		got := Check(code, "")
		if got.Correct || got.Error != tooMuchMemory {
			t.Errorf("Check(%q) = %+v, want %q", code, got, tooMuchMemory)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Check(%q) took %v", code, elapsed)
		}
	}
	if got := Check("assert len(format(1.5, '.100000f')) == 100002", ""); !got.Correct {
		t.Errorf("a precision of 100000 failed: %+v", got)
	}
}

// TestIntToTextLimit: like Python 3.11+, an int of more than 4300 decimal
// digits cannot become text, because that takes long. Other bases can.
func TestIntToTextLimit(t *testing.T) {
	const limit = "Exceeds the limit (4300 digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit"
	runCheckCases(t, []checkCase{
		{"str", "n = int('f' * 4000000, 16)\nx = str(n)", "", false, limit},
		{"repr", "x = repr(10 ** 4300)", "", false, limit},
		{"f-string", "x = f'{-10 ** 4300}'", "", false, limit},
		{"format", "x = format(10 ** 4300, ',')", "", false, limit},
		{"percent", "x = '%d' % (10 ** 4300)", "", false, limit},
		{"format method", "x = '{}'.format(10 ** 4300)", "", false, limit},
		{"in a list", "x = str([10 ** 4300])", "", false, limit},
		{"4300 digits work", "assert len(str(10 ** 4300 - 1)) == 4300 and len(str(-10 ** 4299)) == 4301", "", true, ""},
		{"hex has no limit", "assert len(format(16 ** 5000, 'x')) == 5001 and len('%x' % 16 ** 5000) == 5001", "", true, ""},
	})
}

// TestLargeButAllowedWidths checks that big widths below the limit work and
// are quick: zero padding with grouping used to take seconds.
func TestLargeButAllowedWidths(t *testing.T) {
	for _, code := range []string{
		// The lengths are Python's: a width that ends on a separator gets one
		// more digit.
		"assert len(f'{1:01000000,}') == 1000001",
		"assert len(format(1, '040000,')) == 40001 and format(1234, '08,') == '0,001,234'",
		"assert len('a'.zfill(1000000)) == 1000000 and len('a'.center(1000000)) == 1000000",
	} {
		start := time.Now()
		if got := Check(code, ""); !got.Correct {
			t.Errorf("Check(%q) = %+v", code, got)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Check(%q) took %v", code, elapsed)
		}
	}
}

// TestConcurrentChecks runs checks at the same time, as the server does for
// many students. A slow checker must not hold up the others.
func TestConcurrentChecks(t *testing.T) {
	var wg sync.WaitGroup
	slowDone := make(chan Result, 1)
	go func() { slowDone <- Check("while True: pass", "") }()
	errs := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			got := Check("x = float(user_answer)\nassert 0.49 < x < 0.51, f'got {x:.2f}'", "0.6")
			if got.Correct || got.Error != "got 0.60" {
				errs <- "wrong result: " + got.Error
			}
			if time.Since(start) > time.Second {
				errs <- "a normal check waited for the slow one"
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if got := <-slowDone; got.Correct {
		t.Errorf("slow check = %+v", got)
	}
}

func TestErrorTextHasNoStarlarkDetails(t *testing.T) {
	got := Check("def f():\n    return 1 / 0\nf()", "")
	if got.Correct || strings.Contains(got.Error, "checker") || strings.Contains(got.Error, "Traceback") || strings.Contains(got.Error, ":2:") {
		t.Errorf("error text = %q, want only the message", got.Error)
	}
}

// TestRewriteKeepsTeacherNames checks that the code the rewrite adds does
// not use names that the checker itself may assign, like pow and set.
func TestRewriteKeepsTeacherNames(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"pow", "x = 2 ** 3\npow = 5\nassert x == 8", "", true, ""},
		{"set", "set = 1\nassert 'a' in {'a', 'b'}", "", true, ""},
		{"set comprehension", "set = 1\nassert {c for c in 'aab'} == {'a', 'b'}", "", true, ""},
		{"power assignment", "pow = None\nx = 3\nx **= 2\nassert x == 9", "", true, ""},
	})
}

// TestCheckPythonSyntax runs code with many ordinary Python forms, to make
// sure the rewrite step keeps them working.
func TestCheckPythonSyntax(t *testing.T) {
	code := `# a checker that uses many Python forms
import_name = "not an import"  # a name, not the keyword
values = [int(v) for v in user_answer.split(",") if v.strip()]
first, second = values[:2] if False else (values[0], None)
squares = {v: v ** 2 for v in values}
evens = {v for v in values if v % 2 == 0}
nested = [[i * j for j in range(3)] for i in range(2)]
total = 0
for i, v in enumerate(values, start=1):
    if v < 0:
        continue
    elif v > 100:
        break
    else:
        total += v * i
i = 0
while True:
    i += 1
    if i >= 3: break
def scale(x, factor=2, *rest, offset=0, **extra):
    def inner(y):
        return y * factor + offset
    return inner(x)
pick = lambda a, b=1: a if a > b else b
data = {"k": [1, 2, 3]}
data["k"][0] += 10; data["k"].append(4)
text = "a,b" + \
    ",c"
parts = text.split(","); parts.reverse()
assert values == [1, 2, 3], values
assert squares[3] == 9 and evens == {2} and nested[1][2] == 2
assert total == 1 * 1 + 2 * 2 + 3 * 3, total
assert scale(5) == 10 and scale(5, 3, offset=1) == 16
assert pick(5) == 5 and pick(0, b=2) == 2
assert data == {"k": [11, 2, 3, 4]}
assert parts == ["c", "b", "a"] and text[::-1] == "c,b,a" and text[-1] == "c"
assert f"" == "" and f"{'yes' if total else 'no'}" == "yes"
assert (1,) + (2,) == (1, 2) and [] == list() and {} == dict()
assert not (values is None) and 2 in values and 7 not in values
assert ~1 == -2 and 7 // 2 == 3 and -7 % 3 == 2 and 2 << 3 == 16 and 6 & 3 == 2 and 6 | 1 == 7 and 6 ^ 3 == 5
assert max(values, key=lambda v: -v) == 1 and sorted(values, reverse=True)[0] == 3
assert i == 3 and first == 1
`
	if got := Check(code, "1,2,3"); !got.Correct {
		t.Errorf("Check = %+v", got)
	}
}

// The package docs tell teachers to count letters of non-ASCII text with
// len(list(text)), because len(text) counts bytes in Starlark.
func TestLettersOfNonASCIIText(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"len counts bytes", "assert len(user_answer) == 8", "Київ", true, ""},
		{"len of a list counts letters", "assert len(list(user_answer)) == 4 and list(user_answer)[0] == 'К'", "Київ", true, ""},
	})
}

// TestDocAdviceWorks runs the ways around the known differences that the
// package docs suggest.
func TestDocAdviceWorks(t *testing.T) {
	runCheckCases(t, []checkCase{
		{"count with a condition", "n = 0\nfor c in user_answer:\n    n += 1 if c.isdigit() else 0\nassert n == 2 and sum(c.isdigit() for c in user_answer) == 2", "a1b2", true, ""},
		{"unpack a list of letters", "a, b = list(user_answer)\nassert (a, b) == ('o', 'k')", "ok", true, ""},
		{"tuple in brackets", "x = (1,)\nassert len(x) == 1", "", true, ""},
		{"slice syntax", "assert 'abcdefg'[1:5:2] == 'bd'", "", true, ""},
		{"format with precision", "x = float(user_answer)\nassert False, f'{x:.2f} ' + format(x, '.1f')", "2.345", false, "2.35 2.3"},
	})
}
