package checker

// A small Python tokenizer. It knows what the rewrite step needs: names,
// numbers, operators, string literals (all prefixes, f-strings with their
// fields), comments, brackets and line joins. It groups the tokens into
// logical lines, like Python does.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tokName tokenKind = iota + 1
	tokNumber
	tokString
	tokOp
)

type token struct {
	kind tokenKind
	text string     // source text; numbers come without "_"
	line int        // line where the token starts
	lit  *stringLit // the decoded literal of a string token
}

// stringLit is one string literal with its escapes already decoded.
type stringLit struct {
	isBytes bool
	isF     bool
	value   string        // the text of a plain string
	parts   []fstringPart // the parts of an f-string
}

// fstringPart is a piece of literal text or a replacement field of an
// f-string. A field has a non-empty expr.
type fstringPart struct {
	literal    string
	expr       string        // source of the expression
	line       int           // line where the expression starts
	debug      string        // for f"{x=}": the text "x=" shown before the value
	conversion string        // "", "r", "s" or "a"
	hasSpec    bool          // true when the field has ":spec"
	spec       []fstringPart // the format spec, which may hold fields too
}

// logicalLine is one Python statement line: physical lines joined by
// brackets or by a backslash at the end of a line.
type logicalLine struct {
	indent  int // width of the indentation; a tab moves to the next multiple of 8
	tokens  []token
	line    int // first physical line
	endLine int // last physical line
}

type openBracket struct {
	char byte
	line int
}

type tokenizer struct {
	src      string
	pos      int
	line     int
	lines    []logicalLine
	cur      *logicalLine
	brackets []openBracket
}

// tokenize splits Python source into logical lines of tokens.
func tokenize(src string) ([]logicalLine, *SyntaxError) {
	return tokenizeAt(src, 1)
}

// tokenizeAt tokenizes source that starts on the given line, like the
// expression of an f-string field.
func tokenizeAt(src string, line int) ([]logicalLine, *SyntaxError) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	t := &tokenizer{src: src, line: line}
	if err := t.run(); err != nil {
		return nil, err
	}
	return t.lines, nil
}

func (t *tokenizer) errorf(line int, format string, args ...any) *SyntaxError {
	return &SyntaxError{Msg: fmt.Sprintf(format, args...), Line: line}
}

func (t *tokenizer) run() *SyntaxError {
	for t.pos < len(t.src) {
		if t.cur == nil {
			if !t.startLine() {
				continue
			}
		}
		c := t.src[t.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\f':
			t.pos++
		case c == '#':
			for t.pos < len(t.src) && t.src[t.pos] != '\n' {
				t.pos++
			}
		case c == '\\':
			if t.pos+1 >= len(t.src) || t.src[t.pos+1] != '\n' {
				return t.errorf(t.line, "unexpected character after line continuation character")
			}
			t.pos += 2
			t.line++
		case c == '\n':
			t.pos++
			if len(t.brackets) > 0 {
				t.line++
				continue
			}
			t.endLine()
			t.line++
		case c == '"' || c == '\'':
			if err := t.scanString(""); err != nil {
				return err
			}
		case c >= '0' && c <= '9' || c == '.' && t.pos+1 < len(t.src) && isDigit(t.src[t.pos+1]):
			if err := t.scanNumber(); err != nil {
				return err
			}
		case isNameStart(t.peekRune()):
			if err := t.scanName(); err != nil {
				return err
			}
		default:
			if err := t.scanOperator(); err != nil {
				return err
			}
		}
	}
	if len(t.brackets) > 0 {
		b := t.brackets[len(t.brackets)-1]
		return t.errorf(b.line, "'%c' was never closed", b.char)
	}
	if t.cur != nil {
		t.endLine()
	}
	return nil
}

// startLine reads the indentation of a new physical line. It returns false
// for blank lines and lines with only a comment, which it skips.
func (t *tokenizer) startLine() bool {
	indent := 0
	for t.pos < len(t.src) {
		switch t.src[t.pos] {
		case ' ', '\f':
			indent++
		case '\t':
			indent = (indent/8 + 1) * 8
		default:
			goto measured
		}
		t.pos++
	}
measured:
	if t.pos >= len(t.src) {
		return false
	}
	switch t.src[t.pos] {
	case '#':
		for t.pos < len(t.src) && t.src[t.pos] != '\n' {
			t.pos++
		}
		return false
	case '\n':
		t.pos++
		t.line++
		return false
	}
	t.cur = &logicalLine{indent: indent, line: t.line}
	return true
}

func (t *tokenizer) endLine() {
	if t.cur == nil {
		return
	}
	t.cur.endLine = t.line
	t.lines = append(t.lines, *t.cur)
	t.cur = nil
}

func (t *tokenizer) add(kind tokenKind, text string, line int, lit *stringLit) {
	t.cur.tokens = append(t.cur.tokens, token{kind: kind, text: text, line: line, lit: lit})
}

func (t *tokenizer) peekRune() rune {
	r, _ := utf8.DecodeRuneInString(t.src[t.pos:])
	return r
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isNameStart(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x80 && unicode.IsLetter(r)
}

func isNameChar(r rune) bool {
	return isNameStart(r) || r >= '0' && r <= '9' || r >= 0x80 && (unicode.IsDigit(r) || unicode.IsMark(r))
}

func (t *tokenizer) scanName() *SyntaxError {
	start := t.pos
	for t.pos < len(t.src) {
		r, size := utf8.DecodeRuneInString(t.src[t.pos:])
		if !isNameChar(r) {
			break
		}
		t.pos += size
	}
	name := t.src[start:t.pos]
	if t.pos < len(t.src) && (t.src[t.pos] == '"' || t.src[t.pos] == '\'') && isStringPrefix(name) {
		return t.scanString(name)
	}
	t.add(tokName, name, t.line, nil)
	return nil
}

func isStringPrefix(name string) bool {
	switch strings.ToLower(name) {
	case "r", "u", "b", "br", "rb", "f", "fr", "rf":
		return true
	}
	return false
}

func (t *tokenizer) scanNumber() *SyntaxError {
	start := t.pos
	src := t.src
	digitsWhile := func(ok func(byte) bool) {
		for t.pos < len(src) && (ok(src[t.pos]) || src[t.pos] == '_') {
			t.pos++
		}
	}
	if src[t.pos] == '0' && t.pos+1 < len(src) && strings.IndexByte("xXoObB", src[t.pos+1]) >= 0 {
		t.pos += 2
		digitsWhile(func(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' })
	} else {
		digitsWhile(isDigit)
		if t.pos < len(src) && src[t.pos] == '.' {
			t.pos++
			digitsWhile(isDigit)
		}
		if t.pos < len(src) && (src[t.pos] == 'e' || src[t.pos] == 'E') {
			next := t.pos + 1
			if next < len(src) && (src[next] == '+' || src[next] == '-') {
				next++
			}
			if next < len(src) && isDigit(src[next]) {
				t.pos = next
				digitsWhile(isDigit)
			}
		}
		if t.pos < len(src) && (src[t.pos] == 'j' || src[t.pos] == 'J') {
			return t.errorf(t.line, "complex numbers are not supported")
		}
	}
	if t.pos < len(src) && isNameChar(t.peekRune()) {
		return t.errorf(t.line, "invalid decimal literal")
	}
	text := src[start:t.pos]
	if strings.HasSuffix(text, "_") || strings.Contains(text, "__") || strings.Contains(text, "_.") || strings.Contains(text, "._") {
		return t.errorf(t.line, "invalid decimal literal")
	}
	t.add(tokNumber, strings.ReplaceAll(text, "_", ""), t.line, nil)
	return nil
}

// operators lists Python's operators, longest first.
var operators = []string{
	"**=", "//=", ">>=", "<<=", "...",
	"**", "//", ">>", "<<", "<=", ">=", "==", "!=", "->", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "@=", ":=",
	"+", "-", "*", "/", "%", "@", "&", "|", "^", "~", "<", ">", "(", ")", "[", "]", "{", "}", ",", ":", ".", ";", "=",
}

var closing = map[byte]byte{')': '(', ']': '[', '}': '{'}

func (t *tokenizer) scanOperator() *SyntaxError {
	for _, op := range operators {
		if !strings.HasPrefix(t.src[t.pos:], op) {
			continue
		}
		switch op {
		case "(", "[", "{":
			t.brackets = append(t.brackets, openBracket{op[0], t.line})
		case ")", "]", "}":
			if len(t.brackets) == 0 {
				return t.errorf(t.line, "unmatched '%s'", op)
			}
			open := t.brackets[len(t.brackets)-1]
			if open.char != closing[op[0]] {
				if open.line != t.line {
					return t.errorf(t.line, "closing parenthesis '%s' does not match opening parenthesis '%c' on line %d", op, open.char, open.line)
				}
				return t.errorf(t.line, "closing parenthesis '%s' does not match opening parenthesis '%c'", op, open.char)
			}
			t.brackets = t.brackets[:len(t.brackets)-1]
		}
		t.add(tokOp, op, t.line, nil)
		t.pos += len(op)
		return nil
	}
	r := t.peekRune()
	if r < 0x80 {
		return t.errorf(t.line, "invalid syntax")
	}
	return t.errorf(t.line, "invalid character '%c' (U+%04X)", r, r)
}

// scanString reads a string literal. t.pos is at the opening quote and
// prefix is the text just before it ("", "r", "b", "f", "rb", ...).
func (t *tokenizer) scanString(prefix string) *SyntaxError {
	lower := strings.ToLower(prefix)
	raw := strings.Contains(lower, "r")
	isBytes := strings.Contains(lower, "b")
	isF := strings.Contains(lower, "f")
	startLine := t.line
	start := t.pos - len(prefix)
	quote := t.src[t.pos : t.pos+1]
	if strings.HasPrefix(t.src[t.pos:], strings.Repeat(quote, 3)) {
		quote = strings.Repeat(quote, 3)
	}
	t.pos += len(quote)
	lit := &stringLit{isBytes: isBytes, isF: isF}
	if isF {
		parts, err := t.fstringBody(quote, raw, startLine)
		if err != nil {
			return err
		}
		lit.parts = parts
	} else {
		body, err := t.stringBody(quote, startLine, "string")
		if err != nil {
			return err
		}
		if raw {
			lit.value = body
		} else if lit.value, err = decodeEscapes(body, isBytes, startLine); err != nil {
			return err
		}
		if isBytes {
			for i := 0; i < len(body); i++ {
				if body[i] >= 0x80 {
					return t.errorf(startLine, "bytes can only contain ASCII literal characters")
				}
			}
		}
	}
	t.add(tokString, t.src[start:t.pos], startLine, lit)
	return nil
}

// stringBody returns the raw text between the quotes of a plain string and
// moves past the closing quote.
func (t *tokenizer) stringBody(quote string, startLine int, kind string) (string, *SyntaxError) {
	start := t.pos
	for {
		if t.pos >= len(t.src) {
			return "", t.unterminated(quote, startLine, kind)
		}
		c := t.src[t.pos]
		switch {
		case strings.HasPrefix(t.src[t.pos:], quote):
			body := t.src[start:t.pos]
			t.pos += len(quote)
			return body, nil
		case c == '\\':
			t.pos++
			if t.pos < len(t.src) {
				if t.src[t.pos] == '\n' {
					t.line++
				}
				t.pos++
			}
		case c == '\n':
			if len(quote) == 1 {
				return "", t.unterminated(quote, startLine, kind)
			}
			t.line++
			t.pos++
		default:
			t.pos++
		}
	}
}

func (t *tokenizer) unterminated(quote string, startLine int, kind string) *SyntaxError {
	if len(quote) == 3 {
		return t.errorf(startLine, "unterminated triple-quoted %s literal (detected at line %d)", kind, t.line)
	}
	return t.errorf(startLine, "unterminated %s literal (detected at line %d)", kind, t.line)
}

// fstringBody reads the body of an f-string up to its closing quote and
// returns its parts. It follows Python 3.12, where a field may use the same
// quotes as the f-string itself.
func (t *tokenizer) fstringBody(quote string, raw bool, startLine int) ([]fstringPart, *SyntaxError) {
	var parts []fstringPart
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, fstringPart{literal: text.String()})
			text.Reset()
		}
	}
	for {
		if t.pos >= len(t.src) {
			return nil, t.unterminated(quote, startLine, "f-string")
		}
		c := t.src[t.pos]
		switch {
		case strings.HasPrefix(t.src[t.pos:], quote):
			t.pos += len(quote)
			flush()
			return parts, nil
		case c == '\n' && len(quote) == 1:
			return nil, t.unterminated(quote, startLine, "f-string")
		case c == '{' && strings.HasPrefix(t.src[t.pos:], "{{"):
			text.WriteByte('{')
			t.pos += 2
		case c == '}' && strings.HasPrefix(t.src[t.pos:], "}}"):
			text.WriteByte('}')
			t.pos += 2
		case c == '{':
			flush()
			t.pos++
			field, err := t.fstringField(quote, raw, startLine)
			if err != nil {
				return nil, err
			}
			parts = append(parts, field)
		case c == '}':
			return nil, t.errorf(t.line, "f-string: single '}' is not allowed")
		default:
			if err := t.fstringText(&text, raw, startLine); err != nil {
				return nil, err
			}
		}
	}
}

// fstringText copies one character (or one escape sequence) of the literal
// text of an f-string into text.
func (t *tokenizer) fstringText(text *strings.Builder, raw bool, startLine int) *SyntaxError {
	c := t.src[t.pos]
	next := byte(0)
	if t.pos+1 < len(t.src) {
		next = t.src[t.pos+1]
	}
	switch {
	case c == '\\' && (next == '{' || next == '}'):
		// Python keeps the backslash; the brace still opens or closes a field.
		text.WriteByte('\\')
		t.pos++
		return nil
	case c == '\\' && raw:
		// In a raw f-string a backslash still keeps the next quote from
		// closing the string.
		text.WriteByte('\\')
		t.pos++
		if next != 0 {
			if next == '\n' {
				t.line++
			}
			text.WriteByte(next)
			t.pos++
		}
		return nil
	case c == '\\':
		return t.fstringEscape(text, startLine)
	}
	if c == '\n' {
		t.line++
	}
	_, size := utf8.DecodeRuneInString(t.src[t.pos:])
	text.WriteString(t.src[t.pos : t.pos+size])
	t.pos += size
	return nil
}

// fstringEscape decodes one escape sequence in the text of an f-string.
func (t *tokenizer) fstringEscape(text *strings.Builder, startLine int) *SyntaxError {
	end := t.pos + 2
	if end > len(t.src) {
		return t.errorf(startLine, "unterminated f-string literal (detected at line %d)", t.line)
	}
	switch t.src[t.pos+1] {
	case 'x':
		end = t.pos + 4
	case 'u':
		end = t.pos + 6
	case 'U':
		end = t.pos + 10
	case 'N':
		if close := strings.IndexByte(t.src[t.pos:], '}'); close > 0 {
			end = t.pos + close + 1
		}
	case '0', '1', '2', '3', '4', '5', '6', '7':
		for end < len(t.src) && end < t.pos+4 && t.src[end] >= '0' && t.src[end] <= '7' {
			end++
		}
	case '\n':
		t.line++
	}
	end = min(end, len(t.src))
	decoded, err := decodeEscapes(t.src[t.pos:end], false, t.line)
	if err != nil {
		return err
	}
	text.WriteString(decoded)
	t.pos = end
	return nil
}

// fstringField reads a replacement field. t.pos is just after its "{".
func (t *tokenizer) fstringField(quote string, raw bool, startLine int) (fstringPart, *SyntaxError) {
	field := fstringPart{line: t.line}
	start := t.pos
	depth := 0
	for {
		if t.pos >= len(t.src) {
			return field, t.errorf(t.line, "f-string: expecting '}'")
		}
		c := t.src[t.pos]
		switch {
		case c == '"' || c == '\'':
			// Since Python 3.12 a quote in a field starts a nested string,
			// even the quote of the f-string itself.
			if err := t.skipNestedString(); err != nil {
				return field, t.errorf(field.line, "f-string: expecting '}'")
			}
			continue
		case c == '\n':
			t.line++
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == '}' && depth > 0:
			depth--
		case depth == 0 && (c == '}' || c == ':' || c == '!' && !strings.HasPrefix(t.src[t.pos:], "!=")):
			field.expr = t.src[start:t.pos]
			if strings.TrimSpace(field.expr) == "" {
				return field, t.errorf(t.line, "f-string: valid expression required before '%c'", c)
			}
			if debug, ok := debugExpr(field.expr); ok {
				field.debug = field.expr
				field.expr = debug
			}
			return field, t.fieldEnd(&field, quote, raw, startLine)
		}
		t.pos++
	}
}

// debugExpr finds the "=" at the end of a field like f"{x = }" and returns
// the expression without it.
func debugExpr(expr string) (string, bool) {
	trimmed := strings.TrimRight(expr, " \t\n")
	if !strings.HasSuffix(trimmed, "=") {
		return "", false
	}
	before := trimmed[:len(trimmed)-1]
	if before == "" || strings.ContainsAny(before[len(before)-1:], "=!<>") {
		return "", false
	}
	return before, true
}

// fieldEnd reads the end of a replacement field: an optional "!r"
// conversion, an optional ":spec" and the closing "}".
func (t *tokenizer) fieldEnd(field *fstringPart, quote string, raw bool, startLine int) *SyntaxError {
	explicit := false
	if t.src[t.pos] == '!' {
		t.pos++
		end := t.pos
		for end < len(t.src) && t.src[end] != ':' && t.src[end] != '}' && !strings.HasPrefix(t.src[end:], quote) {
			end++
		}
		conv := strings.TrimSpace(t.src[t.pos:end])
		if conv != "r" && conv != "s" && conv != "a" {
			if conv == "" {
				return t.errorf(t.line, "f-string: missing conversion character")
			}
			return t.errorf(t.line, "f-string: invalid conversion character '%s': expected 's', 'r', or 'a'", conv)
		}
		field.conversion = conv
		explicit = true
		t.pos = end
		if t.pos >= len(t.src) || strings.HasPrefix(t.src[t.pos:], quote) {
			return t.errorf(t.line, "f-string: expecting '}'")
		}
	}
	if t.src[t.pos] == ':' {
		t.pos++
		field.hasSpec = true
		spec, err := t.fstringSpec(quote, raw, startLine)
		if err != nil {
			return err
		}
		field.spec = spec
	} else {
		t.pos++ // the closing "}"
	}
	// f"{x=}" shows repr(x), unless a conversion or a format spec is given.
	if field.debug != "" && !explicit && !field.hasSpec {
		field.conversion = "r"
	}
	return nil
}

// fstringSpec reads a format spec up to and including the closing "}" of
// its field. The spec may hold nested fields, as in f"{x:{width}}".
func (t *tokenizer) fstringSpec(quote string, raw bool, startLine int) ([]fstringPart, *SyntaxError) {
	var parts []fstringPart
	var text strings.Builder
	for {
		if t.pos >= len(t.src) || strings.HasPrefix(t.src[t.pos:], quote) {
			return nil, t.errorf(t.line, "f-string: expecting '}', or format specs")
		}
		switch t.src[t.pos] {
		case '}':
			t.pos++
			if text.Len() > 0 {
				parts = append(parts, fstringPart{literal: text.String()})
			}
			return parts, nil
		case '{':
			if text.Len() > 0 {
				parts = append(parts, fstringPart{literal: text.String()})
				text.Reset()
			}
			t.pos++
			field, err := t.fstringField(quote, raw, startLine)
			if err != nil {
				return nil, err
			}
			parts = append(parts, field)
		default:
			if err := t.fstringText(&text, raw, startLine); err != nil {
				return nil, err
			}
		}
	}
}

// skipNestedString moves past a string literal inside an f-string field.
func (t *tokenizer) skipNestedString() *SyntaxError {
	prefixStart := t.pos
	for prefixStart > 0 && isNameChar(rune(t.src[prefixStart-1])) {
		prefixStart--
	}
	prefix := strings.ToLower(t.src[prefixStart:t.pos])
	if !isStringPrefix(prefix) {
		prefix = ""
	}
	startLine := t.line
	quote := t.src[t.pos : t.pos+1]
	if strings.HasPrefix(t.src[t.pos:], strings.Repeat(quote, 3)) {
		quote = strings.Repeat(quote, 3)
	}
	t.pos += len(quote)
	if strings.Contains(prefix, "f") {
		_, err := t.fstringBody(quote, strings.Contains(prefix, "r"), startLine)
		return err
	}
	_, err := t.stringBody(quote, startLine, "string")
	return err
}

// decodeEscapes decodes the backslash escapes of a Python string literal.
// Unknown escapes keep their backslash, as in Python.
func decodeEscapes(body string, isBytes bool, line int) (string, *SyntaxError) {
	if !strings.Contains(body, `\`) {
		return body, nil
	}
	var b strings.Builder
	for i := 0; i < len(body); {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			i++
			continue
		}
		e := body[i+1]
		i += 2
		switch e {
		case '\n':
		case '\\', '\'', '"':
			b.WriteByte(e)
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			n := int(e - '0')
			for k := 0; k < 2 && i < len(body) && body[i] >= '0' && body[i] <= '7'; k++ {
				n = n*8 + int(body[i]-'0')
				i++
			}
			writeCode(&b, n, isBytes)
		case 'x':
			n, err := strconv.ParseUint(body[i:min(i+2, len(body))], 16, 32)
			if err != nil || i+2 > len(body) {
				return "", &SyntaxError{Msg: `truncated \xXX escape`, Line: line}
			}
			writeCode(&b, int(n), isBytes)
			i += 2
		case 'u', 'U':
			if isBytes {
				b.WriteByte('\\')
				b.WriteByte(e)
				continue
			}
			size := 4
			if e == 'U' {
				size = 8
			}
			n, err := strconv.ParseUint(body[i:min(i+size, len(body))], 16, 32)
			if err != nil || i+size > len(body) {
				return "", &SyntaxError{Msg: fmt.Sprintf(`truncated \%c%s escape`, e, strings.Repeat("X", size)), Line: line}
			}
			if n > unicode.MaxRune {
				return "", &SyntaxError{Msg: "illegal Unicode character", Line: line}
			}
			b.WriteRune(rune(n))
			i += size
		case 'N':
			if isBytes {
				b.WriteString(`\N`)
				continue
			}
			return "", &SyntaxError{Msg: `\N{...} escapes are not supported`, Line: line}
		default:
			b.WriteByte('\\')
			b.WriteByte(e)
		}
	}
	return b.String(), nil
}

func writeCode(b *strings.Builder, n int, isBytes bool) {
	if isBytes {
		b.WriteByte(byte(n))
	} else {
		b.WriteRune(rune(n))
	}
}
