package checker

// The rewrite step turns Python checker code into Starlark code that does
// the same thing. It parses each statement with a small Python parser and
// writes it out again with these changes:
//
//   - assert X, Y            ->  _assert_(X, lambda: (Y))
//   - f"..{x!r:>8}.."        ->  _cat_("..", _fmt_((x), "r", ">8"), "..")
//   - a ** b                 ->  _pow_(a, b)
//   - a * b, a + b           ->  _mul_(a, b), _add_(a, b)  (refuse a text or list too large)
//   - a % b                  ->  _mod_(a, b)          (Python's text formatting for text)
//   - x += y                 ->  x = _iadd_(x, y)     (a list still grows in place)
//   - a < b                  ->  _cmp_("<", a, b)      (NaN compares like in Python)
//   - a < b < c              ->  (_cmp_("<", a, b) and _cmp_("<", b, c))
//   - a is b, a is not b     ->  _cmp_("==", a, b), _cmp_("!=", a, b)
//   - for x in s             ->  for x in _iter_(s)   (Python can loop over text)
//   - (x for x in s)         ->  [x for x in s]       (Starlark has no generators)
//   - {a, b}                 ->  _set_([a, b])
//   - "a" "b"                ->  "ab"
//   - a = b = 0              ->  _chained_value_ = 0; a = _chained_value_; b = _chained_value_
//   - x.sort()               ->  _attr_(x, "sort")()  (methods Starlark lacks)
//   - type hints are dropped, strings get Starlark quotes and escapes.
//
// The names that start and end with "_" are builtins that the checker
// cannot change, so a checker may still assign to pow or set.
//
// The code is put inside a function, so that it can assign to user_answer
// and use if, for and while at the top level:
//
//	def _check_(user_answer):
//	    <checker code>
//	    pass
//	_check_(_answer_)
//
// Every statement stays on its own line number (plus one for the "def"
// line), so Starlark errors point at the teacher's lines.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const (
	wrapperHeader = "def _check_(user_answer):"
	wrapperCall   = "_check_(_answer_)"
	// chainedValue holds the value of a chained assignment "a = b = 0".
	chainedValue = "_chained_value_"
)

// methodFallbacks are methods that go through _attr_, which finds them at
// run time: Python methods that Starlark values lack (x.sort()), and
// methods that need a size check or Python's rules (replace, join, format,
// update).
var methodFallbacks = map[string]bool{
	"sort": true, "reverse": true, "count": true, "copy": true,
	"isnumeric": true, "isdecimal": true, "isascii": true, "casefold": true, "swapcase": true,
	"zfill": true, "ljust": true, "rjust": true, "center": true, "is_integer": true,
	"replace": true, "join": true, "format": true, "split": true, "rsplit": true, "splitlines": true,
	"elems": true, "codepoints": true, "elem_ords": true, "codepoint_ords": true, "extend": true,
	"update": true, "union": true, "intersection": true, "symmetric_difference": true,
}

// notSupported are Python statements and expressions that Starlark has no
// way to run.
var notSupported = map[string]bool{
	"try": true, "except": true, "finally": true, "raise": true, "class": true,
	"import": true, "from": true, "with": true, "global": true, "nonlocal": true,
	"del": true, "yield": true, "async": true, "await": true,
}

// keywords are Python keywords. A keyword is never a function name, so the
// writer keeps a space before a "(" that follows it.
var keywords = map[string]bool{
	"and": true, "as": true, "assert": true, "async": true, "await": true, "break": true,
	"class": true, "continue": true, "def": true, "del": true, "elif": true, "else": true,
	"except": true, "finally": true, "for": true, "from": true, "global": true, "if": true,
	"import": true, "in": true, "is": true, "lambda": true, "nonlocal": true, "not": true,
	"or": true, "pass": true, "raise": true, "return": true, "try": true, "while": true,
	"with": true, "yield": true,
}

// piece is one token of the generated code.
type piece struct {
	text string
	line int  // line of the teacher's code, or 0 for added text
	glue bool // no space after it, as after a unary minus
}

func added(text string) piece { return piece{text: text} }

// copyAdded returns the pieces as added text, for parts that appear twice
// in the output, like the middle of a < b < c.
func copyAdded(pieces []piece) []piece {
	out := make([]piece, len(pieces))
	for i, p := range pieces {
		out[i] = piece{text: p.text, glue: p.glue}
	}
	return out
}

// rewrite turns checker code into a Starlark program.
func rewrite(code string) (string, *SyntaxError) {
	lines, err := tokenize(code)
	if err != nil {
		return "", err
	}
	w := &writer{line: 1}
	w.b.WriteString(wrapperHeader + "\n")
	blocks := &blockTracker{stack: []block{{indent: 0}}, lastHeader: map[int]string{}}
	lastLine := 1
	for _, l := range lines {
		if err := blocks.enter(l); err != nil {
			return "", err
		}
		p := &parser{toks: l.tokens}
		pieces, header, err := p.statementLine(blocks.inDef())
		if err != nil {
			return "", err
		}
		if err := blocks.statement(l, header); err != nil {
			return "", err
		}
		w.statement(l, pieces)
		lastLine = l.endLine
	}
	if err := blocks.finish(lastLine); err != nil {
		return "", err
	}
	w.b.WriteString("\n    pass\n" + wrapperCall + "\n")
	return w.b.String(), nil
}

// lineCount returns the number of lines of code, for clamping error lines.
func lineCount(code string) int {
	code = strings.ReplaceAll(code, "\r\n", "\n")
	code = strings.ReplaceAll(code, "\r", "\n")
	return max(1, strings.Count(strings.TrimRight(code, "\n"), "\n")+1)
}

// header describes a compound statement whose body is on the next lines.
type header struct {
	keyword string // "if", "for", "def", ...
	line    int
}

func (h header) describe() string {
	if h.keyword == "def" {
		return "function definition"
	}
	return fmt.Sprintf("'%s' statement", h.keyword)
}

type block struct {
	indent int
	isDef  bool
}

// blockTracker checks indentation the way Python does and knows whether a
// line is inside a function.
type blockTracker struct {
	stack   []block
	pending *header // a header that waits for its indented body
	// lastHeader holds the keyword of the last statement at each indentation,
	// to find "else" after "for" and "while".
	lastHeader map[int]string
}

func (b *blockTracker) top() block { return b.stack[len(b.stack)-1] }

func (b *blockTracker) inDef() bool {
	for _, bl := range b.stack {
		if bl.isDef {
			return true
		}
	}
	return false
}

func (b *blockTracker) enter(l logicalLine) *SyntaxError {
	if b.pending != nil {
		if l.indent <= b.top().indent {
			return &SyntaxError{Msg: "expected an indented block after " + b.pending.describe() + fmt.Sprintf(" on line %d", b.pending.line), Line: l.line}
		}
		b.stack = append(b.stack, block{indent: l.indent, isDef: b.pending.keyword == "def"})
		b.pending = nil
		return nil
	}
	if l.indent > b.top().indent {
		return &SyntaxError{Msg: "unexpected indent", Line: l.line}
	}
	for l.indent < b.top().indent {
		b.stack = b.stack[:len(b.stack)-1]
	}
	if l.indent != b.top().indent {
		return &SyntaxError{Msg: "unindent does not match any outer indentation level", Line: l.line}
	}
	for indent := range b.lastHeader {
		if indent > l.indent {
			delete(b.lastHeader, indent)
		}
	}
	return nil
}

// statement records a parsed line. keyword is the compound statement
// keyword when the line opens a block, else "".
func (b *blockTracker) statement(l logicalLine, h *header) *SyntaxError {
	first := l.tokens[0]
	if first.kind == tokName && first.text == "else" {
		if last := b.lastHeader[l.indent]; last == "for" || last == "while" {
			return &SyntaxError{Msg: "'else' after 'for' or 'while' is not supported in checker code", Line: l.line}
		}
	}
	b.lastHeader[l.indent] = ""
	if first.kind == tokName && (first.text == "for" || first.text == "while") {
		b.lastHeader[l.indent] = first.text
	}
	b.pending = h
	return nil
}

func (b *blockTracker) finish(lastLine int) *SyntaxError {
	if b.pending != nil {
		return &SyntaxError{
			Msg:  "expected an indented block after " + b.pending.describe() + fmt.Sprintf(" on line %d", b.pending.line),
			Line: min(b.pending.line+1, lastLine),
		}
	}
	return nil
}

// writer writes the generated code and keeps every piece on the line it
// came from.
type writer struct {
	b     strings.Builder
	line  int // the teacher's line that the writer is on
	depth int // brackets open in the output
	prev  piece
}

func (w *writer) statement(l logicalLine, pieces []piece) {
	for w.line < l.line {
		w.b.WriteByte('\n')
		w.line++
	}
	w.b.WriteString(strings.Repeat(" ", 4+l.indent))
	w.prev = piece{}
	for _, p := range pieces {
		for p.line > w.line {
			if w.depth == 0 {
				w.b.WriteString(" \\")
			}
			w.b.WriteByte('\n')
			w.line++
			w.prev = piece{}
		}
		if needSpace(w.prev, p) {
			w.b.WriteByte(' ')
		}
		w.b.WriteString(p.text)
		switch p.text {
		case "(", "[", "{":
			w.depth++
		case ")", "]", "}":
			w.depth--
		}
		w.prev = p
	}
}

// needSpace tells whether the writer puts a space between two pieces.
func needSpace(prev, next piece) bool {
	if prev.text == "" || prev.glue {
		return false
	}
	switch prev.text {
	case "(", "[", "{", ".":
		return false
	}
	switch next.text {
	case ")", "]", "}", ",", ":", ";":
		return false
	case ".":
		return isNumberText(prev.text)
	case "(", "[":
		return !callable(prev.text)
	}
	return true
}

func isNumberText(s string) bool { return s != "" && (s[0] >= '0' && s[0] <= '9' || s[0] == '.') }

// callable tells whether a "(" or "[" after this text is a call or an index.
func callable(s string) bool {
	switch {
	case s == ")" || s == "]" || s == "}":
		return true
	case strings.HasPrefix(s, `"`) || strings.HasPrefix(s, `b"`):
		return true
	case isNumberText(s):
		return false
	}
	return isNameStart(firstRune(s)) && !keywords[s]
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// parser reads the tokens of one logical line.
type parser struct {
	toks []token
	pos  int
}

func (p *parser) done() bool { return p.pos >= len(p.toks) }

func (p *parser) peek() token {
	if p.done() {
		return token{}
	}
	return p.toks[p.pos]
}

func (p *parser) at(text string) bool {
	tok := p.peek()
	return tok.kind == tokOp && tok.text == text
}

func (p *parser) atKeyword(word string) bool {
	tok := p.peek()
	return tok.kind == tokName && tok.text == word
}

// take returns the current token as a piece and moves on.
func (p *parser) take() piece {
	tok := p.peek()
	p.pos++
	text := tok.text
	if tok.kind == tokName && text == "load" {
		// "load" is a keyword in Starlark but a plain name in Python.
		text = "_load_"
	}
	return piece{text: text, line: tok.line}
}

// line returns the line of the current token, or of the last one at the end.
func (p *parser) line() int {
	if p.done() {
		if len(p.toks) == 0 {
			return 1
		}
		return p.toks[len(p.toks)-1].line
	}
	return p.toks[p.pos].line
}

// fail returns the error for the current token.
func (p *parser) fail() *SyntaxError {
	tok := p.peek()
	switch {
	case tok.kind == tokOp && (tok.text == ":=" || tok.text == "@" || tok.text == "@=" || tok.text == "..."):
		return p.notSupported(tok.text)
	case tok.kind == tokName && notSupported[tok.text]:
		return p.notSupported(tok.text)
	}
	return &SyntaxError{Msg: "invalid syntax", Line: p.line()}
}

func (p *parser) notSupported(what string) *SyntaxError {
	return &SyntaxError{Msg: fmt.Sprintf("'%s' is not supported in checker code", what), Line: p.line()}
}

func (p *parser) expect(text string) (piece, *SyntaxError) {
	if !p.at(text) {
		if text == ":" {
			return piece{}, &SyntaxError{Msg: "expected ':'", Line: p.line()}
		}
		return piece{}, p.fail()
	}
	return p.take(), nil
}

func (p *parser) expectName() (piece, *SyntaxError) {
	tok := p.peek()
	if tok.kind != tokName || keywords[tok.text] {
		return piece{}, p.fail()
	}
	return p.take(), nil
}

// statementLine rewrites a logical line. When the line is a compound
// statement whose body follows on the next lines, it also returns its header.
func (p *parser) statementLine(inDef bool) ([]piece, *header, *SyntaxError) {
	first := p.peek()
	var out []piece
	var err *SyntaxError
	if first.kind == tokName {
		switch first.text {
		case "if", "elif", "while":
			out = append(out, p.take())
			cond, err := p.test()
			if err != nil {
				return nil, nil, err
			}
			out = append(out, cond...)
		case "else":
			out = append(out, p.take())
		case "for":
			out, err = p.forHeader()
		case "def":
			out, err = p.defHeader()
		default:
			if notSupported[first.text] {
				return nil, nil, p.notSupported(first.text)
			}
			body, err := p.simpleStatements(inDef)
			return body, nil, err
		}
		if err != nil {
			return nil, nil, err
		}
		colon, err := p.expect(":")
		if err != nil {
			return nil, nil, err
		}
		out = append(out, colon)
		if p.done() {
			return out, &header{keyword: first.text, line: first.line}, nil
		}
		body, err := p.simpleStatements(inDef || first.text == "def")
		if err != nil {
			return nil, nil, err
		}
		return append(out, body...), nil, nil
	}
	if first.kind == tokOp && first.text == "@" {
		return nil, nil, &SyntaxError{Msg: "decorators are not supported in checker code", Line: first.line}
	}
	body, err := p.simpleStatements(inDef)
	return body, nil, err
}

func (p *parser) forHeader() ([]piece, *SyntaxError) {
	out := []piece{p.take()}
	targets, err := p.targets()
	if err != nil {
		return nil, err
	}
	out = append(out, targets...)
	if !p.atKeyword("in") {
		return nil, p.fail()
	}
	out = append(out, p.take())
	iterable, items, err := p.exprList(true)
	if err != nil {
		return nil, err
	}
	return append(out, iterCall(iterable, items)...), nil
}

// iterCall wraps the thing a for loop goes over in _iter_(...), so that a
// loop over text gets its characters, as in Python.
func iterCall(iterable []piece, items int) []piece {
	out := []piece{added("_iter_"), added("(")}
	if items > 1 {
		out = append(out, added("("))
		out = append(out, iterable...)
		out = append(out, added(")"))
	} else {
		out = append(out, iterable...)
	}
	return append(out, added(")"))
}

func (p *parser) defHeader() ([]piece, *SyntaxError) {
	out := []piece{p.take()}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	open, err := p.expect("(")
	if err != nil {
		return nil, err
	}
	out = append(out, name, open)
	params, err := p.params(")", true)
	if err != nil {
		return nil, err
	}
	out = append(out, params...)
	closeParen, err := p.expect(")")
	if err != nil {
		return nil, err
	}
	out = append(out, closeParen)
	if p.at("->") {
		// A return type hint: Starlark has none, so it is dropped.
		p.take()
		if _, err := p.test(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// params reads function parameters up to the end token. Type hints are
// dropped when hints is true (def); a lambda cannot have them.
func (p *parser) params(end string, hints bool) ([]piece, *SyntaxError) {
	var out []piece
	for !p.at(end) && !p.done() {
		switch {
		case p.at("*") || p.at("**"):
			star := p.take()
			star.glue = true
			out = append(out, star)
			if p.peek().kind != tokName {
				break
			}
			fallthrough
		default:
			if p.at("/") {
				return nil, p.notSupported("/")
			}
			name, err := p.expectName()
			if err != nil {
				return nil, err
			}
			out = append(out, name)
			if hints && p.at(":") {
				p.take()
				if _, err := p.test(); err != nil {
					return nil, err
				}
			}
			if p.at("=") {
				out = append(out, p.take())
				value, err := p.test()
				if err != nil {
					return nil, err
				}
				out = append(out, value...)
			}
		}
		if !p.at(",") {
			break
		}
		out = append(out, p.take())
	}
	return out, nil
}

func (p *parser) simpleStatements(inDef bool) ([]piece, *SyntaxError) {
	var out []piece
	for {
		stmt, err := p.simpleStatement(inDef)
		if err != nil {
			return nil, err
		}
		out = append(out, stmt...)
		if p.done() {
			return out, nil
		}
		if !p.at(";") {
			return nil, p.fail()
		}
		out = append(out, p.take())
		if p.done() {
			return out, nil
		}
	}
}

func (p *parser) atStatementEnd() bool { return p.done() || p.at(";") }

func (p *parser) simpleStatement(inDef bool) ([]piece, *SyntaxError) {
	tok := p.peek()
	if tok.kind == tokName {
		switch tok.text {
		case "pass", "break", "continue":
			return []piece{p.take()}, nil
		case "return":
			if !inDef {
				return nil, &SyntaxError{Msg: "'return' outside function", Line: tok.line}
			}
			out := []piece{p.take()}
			if p.atStatementEnd() {
				return out, nil
			}
			value, _, err := p.exprList(true)
			return append(out, value...), err
		case "assert":
			return p.assertStatement()
		}
		if notSupported[tok.text] {
			return nil, p.notSupported(tok.text)
		}
		if keywords[tok.text] && tok.text != "not" && tok.text != "lambda" {
			return nil, p.fail()
		}
	}
	return p.expressionStatement()
}

func (p *parser) assertStatement() ([]piece, *SyntaxError) {
	keyword := p.take()
	cond, err := p.test()
	if err != nil {
		return nil, err
	}
	out := []piece{{text: "_assert_", line: keyword.line}, added("(")}
	out = append(out, cond...)
	if p.at(",") {
		out = append(out, p.take(), added("lambda"), added(":"), added("("))
		msg, err := p.test()
		if err != nil {
			return nil, err
		}
		out = append(out, msg...)
		out = append(out, added(")"))
	}
	if !p.atStatementEnd() {
		return nil, p.fail()
	}
	return append(out, added(")")), nil
}

var augmentedOps = map[string]bool{
	"+=": true, "-=": true, "*=": true, "/=": true, "//=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, ">>=": true, "<<=": true, "**=": true, "@=": true,
}

// augmentedCalls holds the operators that the rewrite turns into calls:
// x += y becomes x = _iadd_(x, (y)). Starlark has no **; the others go
// through functions that check the size of the result. Starlark runs
// x *= y as x = x * y too, also for lists.
var augmentedCalls = map[string]string{
	"**=": "_pow_",
	"*=":  "_mul_",
	"+=":  "_iadd_",
	"%=":  "_mod_",
}

func (p *parser) expressionStatement() ([]piece, *SyntaxError) {
	start := p.pos
	first, _, err := p.exprList(true)
	if err != nil {
		return nil, err
	}
	switch {
	case p.at("="):
		parts := [][]piece{first}
		for p.at("=") {
			p.take()
			next, _, err := p.exprList(true)
			if err != nil {
				return nil, err
			}
			parts = append(parts, next)
		}
		value := parts[len(parts)-1]
		targets := parts[:len(parts)-1]
		if len(targets) == 1 {
			out := append(targets[0], added("="))
			return append(out, value...), nil
		}
		// a = b = value: Starlark has no chained assignment.
		out := []piece{{text: chainedValue, line: first[0].line}, added("=")}
		out = append(out, value...)
		for _, target := range targets {
			out = append(out, added(";"))
			out = append(out, target...)
			out = append(out, added("="), added(chainedValue))
		}
		return out, nil
	case p.peek().kind == tokOp && augmentedOps[p.peek().text]:
		target := p.toks[start:p.pos]
		op := p.take()
		value, _, err := p.exprList(false)
		if err != nil {
			return nil, err
		}
		// Python checks the target only when the whole line can be read.
		if msg := augTargetError(target); msg != "" {
			return nil, &SyntaxError{Msg: msg, Line: target[0].line}
		}
		if op.text == "@=" {
			return nil, &SyntaxError{Msg: "'@=' is not supported in checker code", Line: op.line}
		}
		// These become x = f(x, (value)). The brackets keep a value like
		// "a, b" in one piece: x += a, b adds the tuple (a, b).
		if name, ok := augmentedCalls[op.text]; ok {
			value = append(append([]piece{added("(")}, value...), added(")"))
			return append(first, append([]piece{added("=")}, call(name, copyAdded(first), value)...)...), nil
		}
		out := append(first, op)
		return append(out, value...), nil
	case p.at(":"):
		// A variable type hint, "x: int = 5": the hint is dropped.
		p.take()
		if _, err := p.test(); err != nil {
			return nil, err
		}
		if !p.at("=") {
			return []piece{{text: "pass", line: first[0].line}}, nil
		}
		out := append(first, p.take())
		value, _, err := p.exprList(false)
		if err != nil {
			return nil, err
		}
		return append(out, value...), nil
	}
	if !p.atStatementEnd() {
		return nil, p.fail()
	}
	return first, nil
}

// augTargetError returns Python's error for the target of x += y, or "" for
// a good target: a name, an attribute or an index, also in brackets.
// Without this check the rewrite would let a, b += 1 through, as the
// assignment a, b = _iadd_(a, b, (1)).
func augTargetError(target []token) string {
	inBrackets := false
	// Brackets around the whole target do not count: (a) += 1 is fine.
	for isOpToken(target[0], "(") && closingBracket(target, 0) == len(target)-1 {
		target = target[1 : len(target)-1]
		inBrackets = true
		switch {
		case len(target) == 0:
			return illegalTarget("tuple")
		case containsName(topLevel(target), "for"):
			return illegalTarget("generator expression")
		}
	}
	top := topLevel(target)
	first := target[0]
	switch {
	case containsOp(top, ","):
		return illegalTarget("tuple")
	case isOpToken(first, "*") && inBrackets:
		return "cannot use starred expression here"
	case isOpToken(first, "*"):
		return illegalTarget("starred")
	case isNameToken(first, "lambda"):
		return illegalTarget("lambda")
	case containsName(top, "if"):
		return illegalTarget("conditional expression")
	case isNameToken(first, "not") || containsName(top, "and") || containsName(top, "or"):
		return illegalTarget("expression")
	case containsName(top, "in") || containsName(top, "is") || containsName(top, "not") ||
		containsOp(top, "<", ">", "==", ">=", "<=", "!="):
		return illegalTarget("comparison")
	case containsOp(top, "+", "-", "*", "/", "//", "%", "**", "@", "|", "^", "&", "<<", ">>", "~"):
		return illegalTarget("expression")
	}
	// What is left is one value, maybe with calls, indexes and attributes
	// after it. The last of these decides.
	end := 1
	switch {
	case isOpToken(first, "(", "[", "{"):
		end = closingBracket(target, 0) + 1
	case first.kind == tokString:
		for end < len(target) && target[end].kind == tokString {
			end++
		}
	}
	if end < len(target) {
		if isOpToken(target[len(target)-1], ")") {
			return illegalTarget("function call")
		}
		return ""
	}
	switch first.kind {
	case tokName:
		if first.text == "True" || first.text == "False" || first.text == "None" {
			return illegalTarget(first.text)
		}
		return ""
	case tokNumber:
		return illegalTarget("literal")
	case tokString:
		for _, t := range target {
			if t.lit != nil && t.lit.isF {
				return illegalTarget("f-string expression")
			}
		}
		return illegalTarget("literal")
	}
	if !isOpToken(first, "[", "{") || len(target) < 2 {
		return ""
	}
	inner := target[1 : len(target)-1]
	comprehension := containsName(topLevel(inner), "for")
	switch {
	case first.text == "[" && comprehension:
		return illegalTarget("list comprehension")
	case first.text == "[":
		return illegalTarget("list")
	case len(inner) == 0 || isOpToken(inner[0], "**") || hasDictColon(inner):
		if comprehension {
			return illegalTarget("dict comprehension")
		}
		return illegalTarget("dict literal")
	case comprehension:
		return illegalTarget("set comprehension")
	}
	return illegalTarget("set display")
}

func illegalTarget(kind string) string {
	return fmt.Sprintf("'%s' is an illegal expression for augmented assignment", kind)
}

func isOpToken(t token, texts ...string) bool {
	return t.kind == tokOp && contains(texts, t.text)
}

func isNameToken(t token, text string) bool { return t.kind == tokName && t.text == text }

// closingBracket returns the index of the bracket that closes toks[open].
func closingBracket(toks []token, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		switch {
		case isOpToken(toks[i], "(", "[", "{"):
			depth++
		case isOpToken(toks[i], ")", "]", "}"):
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(toks)
}

// topLevel returns the tokens that are not inside brackets.
func topLevel(toks []token) []token {
	var out []token
	depth := 0
	for _, t := range toks {
		switch {
		case isOpToken(t, "(", "[", "{"):
			depth++
		case isOpToken(t, ")", "]", "}"):
			depth--
		case depth == 0:
			out = append(out, t)
		}
	}
	return out
}

func containsOp(toks []token, texts ...string) bool {
	for _, t := range toks {
		if isOpToken(t, texts...) {
			return true
		}
	}
	return false
}

func containsName(toks []token, text string) bool {
	for _, t := range toks {
		if isNameToken(t, text) {
			return true
		}
	}
	return false
}

// hasDictColon tells whether the inside of {...} has the ":" of a dict.
// A lambda has its own ":": {lambda: 1} is a set.
func hasDictColon(inner []token) bool {
	lambdas := 0
	for _, t := range topLevel(inner) {
		switch {
		case isNameToken(t, "lambda"):
			lambdas++
		case isOpToken(t, ":") && lambdas > 0:
			lambdas--
		case isOpToken(t, ":"):
			return true
		}
	}
	return false
}

// canStartExpression tells whether the current token can begin an expression.
func (p *parser) canStartExpression() bool {
	tok := p.peek()
	switch tok.kind {
	case tokName:
		return !keywords[tok.text] || tok.text == "not" || tok.text == "lambda" || tok.text == "await" || tok.text == "yield"
	case tokNumber, tokString:
		return true
	case tokOp:
		switch tok.text {
		case "(", "[", "{", "-", "+", "~", "*", "**", "...":
			return true
		}
	}
	return false
}

// exprList reads expressions separated by commas, a tuple without brackets.
// It returns the number of items; a trailing comma counts as a second item.
// With star, items may be "*x".
func (p *parser) exprList(star bool) ([]piece, int, *SyntaxError) {
	var out []piece
	items := 0
	for {
		item, err := p.starOrTest(star)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item...)
		items++
		if !p.at(",") {
			return out, items, nil
		}
		out = append(out, p.take())
		if !p.canStartExpression() {
			return out, items + 1, nil
		}
	}
}

func (p *parser) starOrTest(star bool) ([]piece, *SyntaxError) {
	if star && p.at("*") {
		s := p.take()
		s.glue = true
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		return append([]piece{s}, e...), nil
	}
	return p.test()
}

// targets reads the names of a for loop, up to "in".
func (p *parser) targets() ([]piece, *SyntaxError) {
	var out []piece
	for {
		if p.at("*") {
			s := p.take()
			s.glue = true
			out = append(out, s)
		}
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		out = append(out, e...)
		if !p.at(",") {
			return out, nil
		}
		out = append(out, p.take())
		if p.atKeyword("in") {
			return out, nil
		}
	}
}

func (p *parser) test() ([]piece, *SyntaxError) {
	if p.atKeyword("lambda") {
		return p.lambda(true)
	}
	out, err := p.orTest()
	if err != nil {
		return nil, err
	}
	if !p.atKeyword("if") {
		return out, nil
	}
	out = append(out, p.take())
	cond, err := p.orTest()
	if err != nil {
		return nil, err
	}
	out = append(out, cond...)
	if !p.atKeyword("else") {
		return nil, &SyntaxError{Msg: "expected 'else' after 'if' expression", Line: p.line()}
	}
	out = append(out, p.take())
	other, err := p.test()
	if err != nil {
		return nil, err
	}
	return append(out, other...), nil
}

// testNoCond is a test without "x if c else y", used after "if" in a
// comprehension.
func (p *parser) testNoCond() ([]piece, *SyntaxError) {
	if p.atKeyword("lambda") {
		return p.lambda(false)
	}
	return p.orTest()
}

func (p *parser) lambda(allowCond bool) ([]piece, *SyntaxError) {
	out := []piece{p.take()}
	params, err := p.params(":", false)
	if err != nil {
		return nil, err
	}
	out = append(out, params...)
	colon, err := p.expect(":")
	if err != nil {
		return nil, err
	}
	out = append(out, colon)
	var body []piece
	if allowCond {
		body, err = p.test()
	} else {
		body, err = p.testNoCond()
	}
	if err != nil {
		return nil, err
	}
	return append(out, body...), nil
}

func (p *parser) orTest() ([]piece, *SyntaxError) {
	return p.boolOp("or", p.andTest)
}

func (p *parser) andTest() ([]piece, *SyntaxError) {
	return p.boolOp("and", p.notTest)
}

func (p *parser) boolOp(word string, operand func() ([]piece, *SyntaxError)) ([]piece, *SyntaxError) {
	out, err := operand()
	if err != nil {
		return nil, err
	}
	for p.atKeyword(word) {
		out = append(out, p.take())
		next, err := operand()
		if err != nil {
			return nil, err
		}
		out = append(out, next...)
	}
	return out, nil
}

func (p *parser) notTest() ([]piece, *SyntaxError) {
	if p.atKeyword("not") {
		out := []piece{p.take()}
		operand, err := p.notTest()
		if err != nil {
			return nil, err
		}
		return append(out, operand...), nil
	}
	return p.comparison()
}

// compOp reads a comparison operator and returns its Python text, and the
// pieces of "in" and "not in", which stay in the code. "is" and "is not"
// become "==" and "!=": Starlark has no "is", and checkers use it for None,
// True and False.
func (p *parser) compOp() (string, []piece, bool) {
	tok := p.peek()
	switch {
	case tok.kind == tokOp:
		switch tok.text {
		case "<", ">", "==", ">=", "<=", "!=":
			p.take()
			return tok.text, nil, true
		}
	case tok.kind == tokName && tok.text == "in":
		return "in", []piece{p.take()}, true
	case tok.kind == tokName && tok.text == "not" && p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == tokName && p.toks[p.pos+1].text == "in":
		not := p.take()
		return "not in", []piece{not, p.take()}, true
	case tok.kind == tokName && tok.text == "is":
		p.take()
		if p.atKeyword("not") {
			p.take()
			return "!=", nil, true
		}
		return "==", nil, true
	}
	return "", nil, false
}

// compare writes one comparison. "in" and "not in" stay as they are; the
// others go through _cmp_, because Starlark puts NaN above every number,
// while in Python every comparison with NaN is false, except "!=".
func compare(op string, opPieces, left, right []piece) []piece {
	if op == "in" || op == "not in" {
		out := append(append([]piece{}, left...), opPieces...)
		return append(out, right...)
	}
	out := []piece{added("_cmp_"), added("("), added(strconv.Quote(op)), added(",")}
	out = append(out, left...)
	out = append(out, added(","))
	out = append(out, right...)
	return append(out, added(")"))
}

func (p *parser) comparison() ([]piece, *SyntaxError) {
	first, err := p.expr()
	if err != nil {
		return nil, err
	}
	type link struct {
		op       string
		opPieces []piece
		right    []piece
	}
	var links []link
	for {
		op, opPieces, ok := p.compOp()
		if !ok {
			break
		}
		right, err := p.expr()
		if err != nil {
			return nil, err
		}
		links = append(links, link{op, opPieces, right})
	}
	switch len(links) {
	case 0:
		return first, nil
	case 1:
		return compare(links[0].op, links[0].opPieces, first, links[0].right), nil
	}
	// a < b < c: Starlark does not chain comparisons.
	out := []piece{added("(")}
	left := first
	for i, l := range links {
		if i > 0 {
			out = append(out, added("and"))
			left = copyAdded(links[i-1].right)
		}
		out = append(out, compare(l.op, l.opPieces, left, l.right)...)
	}
	return append(out, added(")")), nil
}

// binaryLevels lists binary operators from the weakest to the strongest.
var binaryLevels = [][]string{{"|"}, {"^"}, {"&"}, {"<<", ">>"}, {"+", "-"}, {"*", "@", "/", "%", "//"}}

func (p *parser) expr() ([]piece, *SyntaxError) { return p.binary(0) }

func (p *parser) binary(level int) ([]piece, *SyntaxError) {
	if level == len(binaryLevels) {
		return p.factor()
	}
	out, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	for {
		tok := p.peek()
		if tok.kind != tokOp || !contains(binaryLevels[level], tok.text) {
			return out, nil
		}
		if tok.text == "@" {
			return nil, p.notSupported("@")
		}
		op := p.take()
		right, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		if name := operatorCalls[op.text]; name != "" {
			out = call(name, out, right)
			continue
		}
		out = append(append(out, op), right...)
	}
}

// operatorCalls are the operators that become calls. "*" and "+" can make a
// huge text or list in one step, before the memory watch can stop the
// checker; "%" formats text the Python way.
var operatorCalls = map[string]string{"*": "_mul_", "+": "_add_", "%": "_mod_"}

// call1 writes name(arg).
func call1(name string, arg []piece) []piece {
	out := append([]piece{added(name), added("(")}, arg...)
	return append(out, added(")"))
}

// call writes name(left, right).
func call(name string, left, right []piece) []piece {
	out := append([]piece{added(name), added("(")}, left...)
	out = append(out, added(","))
	out = append(out, right...)
	return append(out, added(")"))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (p *parser) factor() ([]piece, *SyntaxError) {
	if p.at("-") || p.at("+") || p.at("~") {
		op := p.take()
		op.glue = true
		operand, err := p.factor()
		if err != nil {
			return nil, err
		}
		return append([]piece{op}, operand...), nil
	}
	return p.power()
}

func (p *parser) power() ([]piece, *SyntaxError) {
	base, err := p.atomExpr()
	if err != nil {
		return nil, err
	}
	if !p.at("**") {
		return base, nil
	}
	p.take()
	exponent, err := p.factor()
	if err != nil {
		return nil, err
	}
	out := []piece{added("_pow_"), added("(")}
	out = append(out, base...)
	out = append(out, added(","))
	out = append(out, exponent...)
	return append(out, added(")")), nil
}

func (p *parser) atomExpr() ([]piece, *SyntaxError) {
	out, err := p.atom()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.at("("):
			args, err := p.callArgs()
			if err != nil {
				return nil, err
			}
			out = append(out, args...)
		case p.at("["):
			sub, err := p.subscript()
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		case p.at("."):
			dot := p.take()
			name := p.peek()
			if name.kind != tokName {
				return nil, p.fail()
			}
			p.pos++
			if methodFallbacks[name.text] {
				wrapped := []piece{added("_attr_"), added("(")}
				wrapped = append(wrapped, out...)
				wrapped = append(wrapped, added(","), piece{text: strconv.Quote(name.text), line: name.line}, added(")"))
				out = wrapped
				continue
			}
			out = append(out, dot, piece{text: name.text, line: name.line})
		default:
			return out, nil
		}
	}
}

func (p *parser) callArgs() ([]piece, *SyntaxError) {
	out := []piece{p.take()}
	for !p.at(")") {
		var arg []piece
		var err *SyntaxError
		switch {
		case p.at("*") || p.at("**"):
			star := p.take()
			star.glue = true
			arg, err = p.test()
			if star.text == "*" {
				// f(*x): _star_ refuses a long x before Starlark makes
				// the arguments.
				arg = call1("_star_", arg)
			}
			arg = append([]piece{star}, arg...)
		default:
			arg, err = p.test()
			if err != nil {
				return nil, err
			}
			switch {
			case p.at("="):
				eq := p.take()
				value, err := p.test()
				if err != nil {
					return nil, err
				}
				arg = append(append(arg, eq), value...)
			case p.atKeyword("for"):
				// f(x for x in y): a generator becomes a list.
				comp, err := p.compFor()
				if err != nil {
					return nil, err
				}
				arg = append(append([]piece{added("[")}, arg...), comp...)
				arg = append(arg, added("]"))
			}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, arg...)
		if !p.at(",") {
			break
		}
		out = append(out, p.take())
	}
	closeParen, err := p.expect(")")
	if err != nil {
		return nil, err
	}
	return append(out, closeParen), nil
}

func (p *parser) subscript() ([]piece, *SyntaxError) {
	out := []piece{p.take()}
	for !p.at("]") {
		if !p.at(":") {
			e, err := p.test()
			if err != nil {
				return nil, err
			}
			out = append(out, e...)
		}
		for i := 0; i < 2 && p.at(":"); i++ {
			out = append(out, p.take())
			if !p.at(":") && !p.at("]") && !p.at(",") {
				e, err := p.test()
				if err != nil {
					return nil, err
				}
				out = append(out, e...)
			}
		}
		if !p.at(",") {
			break
		}
		out = append(out, p.take())
	}
	closeBracket, err := p.expect("]")
	if err != nil {
		return nil, err
	}
	return append(out, closeBracket), nil
}

// compFor reads the "for ... in ... if ..." part of a comprehension.
func (p *parser) compFor() ([]piece, *SyntaxError) {
	var out []piece
	for p.atKeyword("for") || p.atKeyword("async") {
		if p.atKeyword("async") {
			return nil, p.notSupported("async")
		}
		out = append(out, p.take())
		targets, err := p.targets()
		if err != nil {
			return nil, err
		}
		out = append(out, targets...)
		if !p.atKeyword("in") {
			return nil, p.fail()
		}
		out = append(out, p.take())
		iterable, err := p.orTest()
		if err != nil {
			return nil, err
		}
		out = append(out, iterCall(iterable, 1)...)
		for p.atKeyword("if") {
			out = append(out, p.take())
			cond, err := p.testNoCond()
			if err != nil {
				return nil, err
			}
			out = append(out, cond...)
		}
	}
	return out, nil
}

func (p *parser) atom() ([]piece, *SyntaxError) {
	tok := p.peek()
	switch tok.kind {
	case tokName:
		if keywords[tok.text] {
			return nil, p.fail()
		}
		return []piece{p.take()}, nil
	case tokNumber:
		return []piece{p.take()}, nil
	case tokString:
		return p.strings()
	case tokOp:
		switch tok.text {
		case "(":
			return p.parenAtom()
		case "[":
			return p.listAtom()
		case "{":
			return p.braceAtom()
		}
	}
	return nil, p.fail()
}

func (p *parser) parenAtom() ([]piece, *SyntaxError) {
	open := p.take()
	if p.at(")") {
		return []piece{open, p.take()}, nil
	}
	if p.atKeyword("yield") {
		return nil, p.notSupported("yield")
	}
	first, err := p.starOrTest(true)
	if err != nil {
		return nil, err
	}
	if p.atKeyword("for") {
		// (x for x in y): a generator becomes a list.
		comp, err := p.compFor()
		if err != nil {
			return nil, err
		}
		closeParen, err := p.expect(")")
		if err != nil {
			return nil, err
		}
		out := append([]piece{{text: "[", line: open.line}}, first...)
		out = append(out, comp...)
		return append(out, piece{text: "]", line: closeParen.line}), nil
	}
	out := append([]piece{open}, first...)
	rest, err := p.restOfList(")")
	if err != nil {
		return nil, err
	}
	return append(out, rest...), nil
}

// restOfList reads ", item, item" up to and including the closing bracket.
func (p *parser) restOfList(closing string) ([]piece, *SyntaxError) {
	var out []piece
	for p.at(",") {
		out = append(out, p.take())
		if p.at(closing) {
			break
		}
		item, err := p.starOrTest(true)
		if err != nil {
			return nil, err
		}
		out = append(out, item...)
	}
	end, err := p.expect(closing)
	if err != nil {
		return nil, err
	}
	return append(out, end), nil
}

func (p *parser) listAtom() ([]piece, *SyntaxError) {
	open := p.take()
	if p.at("]") {
		return []piece{open, p.take()}, nil
	}
	first, err := p.starOrTest(true)
	if err != nil {
		return nil, err
	}
	out := append([]piece{open}, first...)
	if p.atKeyword("for") {
		comp, err := p.compFor()
		if err != nil {
			return nil, err
		}
		closeBracket, err := p.expect("]")
		if err != nil {
			return nil, err
		}
		out = append(out, comp...)
		return append(out, closeBracket), nil
	}
	rest, err := p.restOfList("]")
	if err != nil {
		return nil, err
	}
	return append(out, rest...), nil
}

func (p *parser) braceAtom() ([]piece, *SyntaxError) {
	open := p.take()
	if p.at("}") {
		return []piece{open, p.take()}, nil
	}
	if p.at("**") {
		return nil, p.notSupported("**")
	}
	first, err := p.starOrTest(true)
	if err != nil {
		return nil, err
	}
	if p.at(":") {
		return p.dictRest(open, first)
	}
	// A set: Starlark has no set literal, so {a, b} becomes _set_([a, b]).
	out := []piece{{text: "_set_", line: open.line}, added("("), added("[")}
	out = append(out, first...)
	if p.atKeyword("for") {
		comp, err := p.compFor()
		if err != nil {
			return nil, err
		}
		out = append(out, comp...)
		closeBrace, err := p.expect("}")
		if err != nil {
			return nil, err
		}
		return append(out, piece{text: "]", line: closeBrace.line}, added(")")), nil
	}
	rest, err := p.restOfList("}")
	if err != nil {
		return nil, err
	}
	rest[len(rest)-1].text = "]"
	return append(append(out, rest...), added(")")), nil
}

func (p *parser) dictRest(open piece, key []piece) ([]piece, *SyntaxError) {
	out := append([]piece{open}, key...)
	out = append(out, p.take())
	value, err := p.test()
	if err != nil {
		return nil, err
	}
	out = append(out, value...)
	if p.atKeyword("for") {
		comp, err := p.compFor()
		if err != nil {
			return nil, err
		}
		closeBrace, err := p.expect("}")
		if err != nil {
			return nil, err
		}
		out = append(out, comp...)
		return append(out, closeBrace), nil
	}
	for p.at(",") {
		out = append(out, p.take())
		if p.at("}") {
			break
		}
		if p.at("**") {
			return nil, p.notSupported("**")
		}
		k, err := p.test()
		if err != nil {
			return nil, err
		}
		colon, err := p.expect(":")
		if err != nil {
			return nil, err
		}
		v, err := p.test()
		if err != nil {
			return nil, err
		}
		out = append(append(append(out, k...), colon), v...)
	}
	closeBrace, err := p.expect("}")
	if err != nil {
		return nil, err
	}
	return append(out, closeBrace), nil
}

// segment is a part of a string expression: literal text or an f-string
// field.
type segment struct {
	text  string
	field *fstringPart
}

// strings reads one or more string literals in a row. Python joins them;
// f-strings become a sum of texts and _fmt_ calls.
func (p *parser) strings() ([]piece, *SyntaxError) {
	first := p.peek()
	var segments []segment
	isBytes := first.lit.isBytes
	for p.peek().kind == tokString {
		tok := p.peek()
		if tok.lit.isBytes != isBytes {
			return nil, &SyntaxError{Msg: "cannot mix bytes and nonbytes literals", Line: tok.line}
		}
		p.pos++
		if !tok.lit.isF {
			segments = appendText(segments, tok.lit.value)
			continue
		}
		for i := range tok.lit.parts {
			part := &tok.lit.parts[i]
			if part.expr == "" {
				segments = appendText(segments, part.literal)
				continue
			}
			if part.debug != "" {
				segments = appendText(segments, part.debug)
			}
			segments = append(segments, segment{field: part})
		}
	}
	if isBytes {
		return []piece{{text: starlarkQuote(segments[0].text, true), line: first.line}}, nil
	}
	out, err := joinSegments(segments)
	if err != nil {
		return nil, err
	}
	out[0].line = first.line
	return out, nil
}

func appendText(segments []segment, text string) []segment {
	if n := len(segments); n > 0 && segments[n-1].field == nil {
		segments[n-1].text += text
		return segments
	}
	return append(segments, segment{text: text})
}

// joinSegments writes _cat_("text", _fmt_(...), "text"), or just one part
// when there is only one.
func joinSegments(segments []segment) ([]piece, *SyntaxError) {
	if len(segments) == 0 {
		return []piece{added(`""`)}, nil
	}
	var out []piece
	for i, s := range segments {
		if i > 0 {
			out = append(out, added(","))
		}
		if s.field == nil {
			out = append(out, added(starlarkQuote(s.text, false)))
			continue
		}
		call, err := fieldCall(s.field)
		if err != nil {
			return nil, err
		}
		out = append(out, call...)
	}
	if len(segments) == 1 {
		return out, nil
	}
	out = append([]piece{added("_cat_"), added("(")}, out...)
	return append(out, added(")")), nil
}

// fieldCall writes _fmt_((expr), "conversion", spec) for an f-string field.
func fieldCall(f *fstringPart) ([]piece, *SyntaxError) {
	lines, err := tokenizeAt("("+f.expr+")", f.line)
	if err != nil {
		return nil, err
	}
	if len(lines) != 1 {
		return nil, &SyntaxError{Msg: "f-string: invalid syntax", Line: f.line}
	}
	sub := &parser{toks: lines[0].tokens}
	sub.take()
	expr, _, err := sub.exprList(true)
	if err != nil {
		return nil, err
	}
	if !sub.at(")") || sub.pos != len(sub.toks)-1 {
		return nil, sub.fail()
	}
	out := []piece{added("_fmt_"), added("("), added("(")}
	out = append(out, copyAdded(expr)...)
	out = append(out, added(")"), added(","), added(strconv.Quote(f.conversion)), added(","))
	if !f.hasSpec {
		out = append(out, added(`""`))
	} else {
		var segments []segment
		for i := range f.spec {
			part := &f.spec[i]
			if part.expr == "" {
				segments = appendText(segments, part.literal)
			} else {
				segments = append(segments, segment{field: part})
			}
		}
		spec, err := joinSegments(segments)
		if err != nil {
			return nil, err
		}
		out = append(out, spec...)
	}
	return append(out, added(")")), nil
}

// starlarkQuote writes text as a Starlark string literal on one line.
func starlarkQuote(text string, isBytes bool) string {
	var b strings.Builder
	if isBytes {
		b.WriteByte('b')
	}
	b.WriteByte('"')
	if isBytes {
		for i := 0; i < len(text); i++ {
			c := text[i]
			switch {
			case c == '"' || c == '\\':
				b.WriteByte('\\')
				b.WriteByte(c)
			case c == '\n':
				b.WriteString(`\n`)
			case c == '\t':
				b.WriteString(`\t`)
			case c < ' ' || c >= 0x7f:
				fmt.Fprintf(&b, `\x%02x`, c)
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('"')
		return b.String()
	}
	for _, r := range text {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < ' ' || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x80 || unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
