package checker

import (
	"fmt"
	"strings"
	"testing"
)

// describeLines shows logical lines in a short form for the tests:
// "1-2 indent=4: x = ( 1 , 2 )". Strings show their decoded value.
func describeLines(lines []logicalLine) []string {
	var out []string
	for _, l := range lines {
		var parts []string
		for _, tok := range l.tokens {
			parts = append(parts, describeToken(tok))
		}
		out = append(out, fmt.Sprintf("%d-%d indent=%d: %s", l.line, l.endLine, l.indent, strings.Join(parts, " ")))
	}
	return out
}

func describeToken(tok token) string {
	if tok.kind != tokString {
		return tok.text
	}
	lit := tok.lit
	switch {
	case lit.isF:
		return "f" + describeParts(lit.parts)
	case lit.isBytes:
		return fmt.Sprintf("b%q", lit.value)
	}
	return fmt.Sprintf("%q", lit.value)
}

func describeParts(parts []fstringPart) string {
	var b strings.Builder
	b.WriteString("[")
	for i, p := range parts {
		if i > 0 {
			b.WriteString(" ")
		}
		if p.expr == "" {
			fmt.Fprintf(&b, "%q", p.literal)
			continue
		}
		fmt.Fprintf(&b, "{%s", p.expr)
		if p.debug != "" {
			fmt.Fprintf(&b, " debug=%q", p.debug)
		}
		if p.conversion != "" {
			fmt.Fprintf(&b, " !%s", p.conversion)
		}
		if p.hasSpec {
			fmt.Fprintf(&b, " :%s", describeParts(p.spec))
		}
		b.WriteString("}")
	}
	b.WriteString("]")
	return b.String()
}

func TestTokenizeLogicalLines(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"simple", "x = 1\ny = x + 2\n", []string{"1-1 indent=0: x = 1", "2-2 indent=0: y = x + 2"}},
		{"blank and comment lines", "# start\n\nx = 1  # note\n   # inside\n\ny = 2", []string{"3-3 indent=0: x = 1", "6-6 indent=0: y = 2"}},
		{"brackets join lines", "x = (1,\n     2)\nz = [\n  3]\n", []string{"1-2 indent=0: x = ( 1 , 2 )", "3-4 indent=0: z = [ 3 ]"}},
		{"backslash joins lines", "x = 1 + \\\n    2\ny = 3", []string{"1-2 indent=0: x = 1 + 2", "3-3 indent=0: y = 3"}},
		{"indentation", "if x:\n    y = 1\n\tz = 2\n", []string{"1-1 indent=0: if x :", "2-2 indent=4: y = 1", "3-3 indent=8: z = 2"}},
		{"tab after spaces", "if x:\n  \ty = 1\n", []string{"1-1 indent=0: if x :", "2-2 indent=8: y = 1"}},
		{"windows line ends", "x = 1\r\ny = 2\r\n", []string{"1-1 indent=0: x = 1", "2-2 indent=0: y = 2"}},
		{"operators", "a **= b // c != d <= e -> f := g ... h >>= 1", []string{"1-1 indent=0: a **= b // c != d <= e -> f := g ... h >>= 1"}},
		{"numbers", "x = 1_000 + 0x_FF + .5 + 1e-3 + 3.", []string{"1-1 indent=0: x = 1000 + 0xFF + .5 + 1e-3 + 3."}},
		{"unicode names", "відповідь = user_answer", []string{"1-1 indent=0: відповідь = user_answer"}},
		{"semicolons", "a = 1; b = 2;", []string{"1-1 indent=0: a = 1 ; b = 2 ;"}},
		{"empty", "  \n# only a comment\n", nil},
	}
	for _, c := range cases {
		lines, err := tokenize(c.src)
		if err != nil {
			t.Errorf("%s: tokenize failed: %v", c.name, err)
			continue
		}
		got := describeLines(lines)
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}

func TestTokenizeStrings(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"single quotes", `'it''s'`, `"it" "s"`},
		{"escapes", `"a\tb\n\x41\101\u0439\U0001F600\\\"\q"`, `"a\tb\nAAй😀\\\"\\q"`},
		{"backslash newline", "'ab\\\ncd'", `"abcd"`},
		{"raw", `r"\d+\n" R'\''`, `"\\d+\\n" "\\'"`},
		{"unicode prefix", `u"x" U'y'`, `"x" "y"`},
		{"bytes", `b"a\x00\xff" Rb"\n"`, `b"a\x00\xff" b"\\n"`},
		{"triple", "'''a\n'b'\nc'''", `"a\n'b'\nc"`},
		{"triple double", `"""say "hi" """`, `"say \"hi\" "`},
		{"f plain", `f"abc"`, `f["abc"]`},
		{"f field", `f"x={x}!"`, `f["x=" {x} "!"]`},
		{"f braces", `f"{{a}} {b}}}"`, `f["{a} " {b} "}"]`},
		{"f conversion and spec", `f"{x!r:>8}"`, `f[{x !r :[">8"]}]`},
		{"f nested spec", `f"{x:{w}.{p}f}"`, `f[{x :[{w} "." {p} "f"]}]`},
		{"f not equal is not a conversion", `f"{a!=b}"`, `f[{a!=b}]`},
		{"f debug", `f"{x = }"`, `f[{x  debug="x = " !r}]`},
		{"f debug with spec", `f"{x=:.2f}"`, `f[{x debug="x=" :[".2f"]}]`},
		{"f dict and lambda in brackets", `f"{d['k']:>3} {(lambda y: y)(1)}"`, `f[{d['k'] :[">3"]} " " {(lambda y: y)(1)}]`},
		{"f same quotes inside", `f"{d["k"]}"`, `f[{d["k"]}]`},
		{"f nested f-string", `f"{f'<{x}>'}"`, `f[{f'<{x}>'}]`},
		{"f escapes in text", `f"a\n{x}\t"`, `f["a\n" {x} "\t"]`},
		{"f raw", `rf"\d{x}"`, `f["\\d" {x}]`},
		{"f triple", "f'''a\n{x}\nb'''", `f["a\n" {x} "\nb"]`},
	}
	for _, c := range cases {
		lines, err := tokenize(c.src)
		if err != nil {
			t.Errorf("%s: tokenize failed: %v", c.name, err)
			continue
		}
		if len(lines) != 1 {
			t.Errorf("%s: got %d lines", c.name, len(lines))
			continue
		}
		var parts []string
		for _, tok := range lines[0].tokens {
			parts = append(parts, describeToken(tok))
		}
		if got := strings.Join(parts, " "); got != c.want {
			t.Errorf("%s:\n got: %s\nwant: %s", c.name, got, c.want)
		}
	}
}

func TestTokenizeTripleQuotedLines(t *testing.T) {
	lines, err := tokenize("x = '''a\nb\nc'''\ny = 1")
	if err != nil {
		t.Fatal(err)
	}
	got := describeLines(lines)
	want := []string{`1-3 indent=0: x = "a\nb\nc"`, "4-4 indent=0: y = 1"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTokenizeErrors(t *testing.T) {
	cases := []struct {
		src  string
		msg  string
		line int
	}{
		{"x = 'abc\ny = 1", "unterminated string literal (detected at line 1)", 1},
		{"x = 1\ny = \"\"\"abc\n\nz", "unterminated triple-quoted string literal (detected at line 4)", 2},
		{"x = f'{a'", "f-string: expecting '}'", 1},
		{"x = f'}'", "f-string: single '}' is not allowed", 1},
		{"x = f'{}'", "f-string: valid expression required before '}'", 1},
		{"x = f'{a!z}'", "f-string: invalid conversion character 'z': expected 's', 'r', or 'a'", 1},
		{"x = (1,\n2", "'(' was never closed", 1},
		{"x = 1)", "unmatched ')'", 1},
		{"x = (1]", "closing parenthesis ']' does not match opening parenthesis '('", 1},
		{"x = (1,\n2]", "closing parenthesis ']' does not match opening parenthesis '(' on line 1", 2},
		{"x = 1 \\ 2", "unexpected character after line continuation character", 1},
		{"x = 5 € 3", "invalid character '€' (U+20AC)", 1},
		{"x = “a”", "invalid character '“' (U+201C)", 1},
		{"x = $", "invalid syntax", 1},
		{"x = 1j", "complex numbers are not supported", 1},
		{"x = 1abc", "invalid decimal literal", 1},
		{"x = '\\N{DASH}'", `\N{...} escapes are not supported`, 1},
		{"x = '\\x4'", `truncated \xXX escape`, 1},
		{"x = '\\u12'", `truncated \uXXXX escape`, 1},
		{"x = '\\U1234'", `truncated \UXXXXXXXX escape`, 1},
		{"x = b'ї'", "bytes can only contain ASCII literal characters", 1},
	}
	for _, c := range cases {
		_, err := tokenize(c.src)
		if err == nil {
			t.Errorf("tokenize(%q) did not fail, want %q", c.src, c.msg)
			continue
		}
		if err.Msg != c.msg || err.Line != c.line {
			t.Errorf("tokenize(%q) = %q on line %d, want %q on line %d", c.src, err.Msg, err.Line, c.msg, c.line)
		}
	}
}
