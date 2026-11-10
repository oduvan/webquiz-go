package checker

// This file gives Python's text forms of values: str(), repr() and
// format(value, spec) with the format spec mini-language. The checker uses
// them for f-strings, str(), repr() and format(), so messages look the same
// as in Python 1.19.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.starlark.net/starlark"
)

// maxReprDepth stops repr() of very deeply nested lists.
const maxReprDepth = 200

// maxFloatPrecision limits the digits of a formatted number: making them
// takes about ten bytes of memory for each digit.
const maxFloatPrecision = 1_000_000

// intTextLimit is Python 3.11's error for an int with too many digits to
// turn into text. Making such a text takes long.
const intTextLimit = "Exceeds the limit (4300 digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit"

// intValueText is intText for a Starlark int, quick for small ones.
func intValueText(v starlark.Int) (string, error) {
	if small, ok := v.Int64(); ok {
		return strconv.FormatInt(small, 10), nil
	}
	return intText(v.BigInt())
}

// intText returns the decimal text of n, or Python's error when it has more
// than maxIntDigits digits.
func intText(n *big.Int) (string, error) {
	// A number of more than 14400 bits has more than 4334 digits.
	if n.BitLen() > 14400 {
		return "", errors.New(intTextLimit)
	}
	text := n.Text(10)
	digits := len(text)
	if n.Sign() < 0 {
		digits--
	}
	if digits > maxIntDigits {
		return "", errors.New(intTextLimit)
	}
	return text, nil
}

// maxReprBytes limits the text of repr() and str(). Text made of many small
// values is slow to build: 4 MB takes about a fifth of a second, and much
// more than any checker message needs.
const maxReprBytes = 4 << 20

// pyStr returns Python's str(v).
func pyStr(v starlark.Value) (string, error) {
	if s, ok := v.(starlark.String); ok {
		return string(s), nil
	}
	return pyRepr(v)
}

// pyRepr returns Python's repr(v). It fails when the text would be larger
// than maxReprBytes: a list that holds another list twice at each level has
// a huge text although it is small.
func pyRepr(v starlark.Value) (string, error) {
	w := &reprWriter{}
	if err := w.write(v); err != nil {
		return "", err
	}
	return w.b.String(), nil
}

// reprWriter builds the text of repr().
type reprWriter struct {
	b     strings.Builder
	depth int // how deep the value being written is nested
	// open holds the lists and dicts being written, outermost first. A
	// value that holds itself is written as [...], like Python does.
	open []starlark.Value
}

func (w *reprWriter) isOpen(v starlark.Value) bool {
	for _, x := range w.open {
		if x == v {
			return true
		}
	}
	return false
}

func (w *reprWriter) write(v starlark.Value) error {
	b := &w.b
	if b.Len() > maxReprBytes {
		return errors.New(tooMuchMemory)
	}
	if w.depth > maxReprDepth {
		b.WriteString("...")
		return nil
	}
	w.depth++
	defer func() { w.depth-- }()
	switch v := v.(type) {
	// A repr is at least as long as the text, so a long text fails at once.
	case starlark.String:
		if b.Len()+len(v) > maxReprBytes {
			return errors.New(tooMuchMemory)
		}
		return writeReprString(b, string(v), maxReprBytes)
	case starlark.Bytes:
		if b.Len()+len(v) > maxReprBytes {
			return errors.New(tooMuchMemory)
		}
		return writeReprBytes(b, string(v), maxReprBytes)
	case starlark.Float:
		b.WriteString(pyFloatRepr(float64(v)))
	case starlark.Int:
		text, err := intValueText(v)
		if err != nil {
			return err
		}
		b.WriteString(text)
	case *starlark.List:
		if w.isOpen(v) {
			b.WriteString("[...]")
			return nil
		}
		w.open = append(w.open, v)
		b.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := w.write(v.Index(i)); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		w.open = w.open[:len(w.open)-1]
	case starlark.Tuple:
		b.WriteByte('(')
		for i, x := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := w.write(x); err != nil {
				return err
			}
		}
		if len(v) == 1 {
			b.WriteByte(',')
		}
		b.WriteByte(')')
	case *starlark.Dict:
		if w.isOpen(v) {
			b.WriteString("{...}")
			return nil
		}
		w.open = append(w.open, v)
		b.WriteByte('{')
		for i, item := range v.Items() {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := w.write(item[0]); err != nil {
				return err
			}
			b.WriteString(": ")
			if err := w.write(item[1]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		w.open = w.open[:len(w.open)-1]
	case *starlark.Set:
		if v.Len() == 0 {
			b.WriteString("set()")
			return nil
		}
		b.WriteByte('{')
		iter := v.Iterate()
		defer iter.Done()
		var x starlark.Value
		for i := 0; iter.Next(&x); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := w.write(x); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		// The text of text.elems() and the like holds the whole text.
		if b.Len()+sourceSize(v) > maxReprBytes {
			return errors.New(tooMuchMemory)
		}
		b.WriteString(v.String())
	}
	if b.Len() > maxReprBytes {
		return errors.New(tooMuchMemory)
	}
	return nil
}

// pyReprString returns Python's repr() of a string. It fails when the text
// would be larger than maxReprBytes.
func pyReprString(s string) (string, error) {
	var b strings.Builder
	if err := writeReprString(&b, s, maxReprBytes); err != nil {
		return "", err
	}
	return b.String(), nil
}

// writeReprString writes Python's repr() of a string to b: single quotes,
// or double quotes when the text has a single quote and no double quote.
// It stops with an error as soon as b is longer than limit; b then holds
// the start of the text.
func writeReprString(b *strings.Builder, s string, limit int) error {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	b.WriteRune(quote)
	for _, r := range s {
		if b.Len() > limit {
			return errors.New(tooMuchMemory)
		}
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < ' ' || r == 0x7f:
			writeEscape(b, 'x', r, 2)
		case r < 0x80 || unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xff:
			writeEscape(b, 'x', r, 2)
		case r <= 0xffff:
			writeEscape(b, 'u', r, 4)
		default:
			writeEscape(b, 'U', r, 8)
		}
	}
	b.WriteRune(quote)
	if b.Len() > limit {
		return errors.New(tooMuchMemory)
	}
	return nil
}

// writeReprBytes writes Python's repr() of a bytes value to b, like
// writeReprString.
func writeReprBytes(b *strings.Builder, s string, limit int) error {
	quote := byte('\'')
	if strings.IndexByte(s, '\'') >= 0 && strings.IndexByte(s, '"') < 0 {
		quote = '"'
	}
	b.WriteString("b")
	b.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		if b.Len() > limit {
			return errors.New(tooMuchMemory)
		}
		c := s[i]
		switch {
		case c == quote || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < ' ' || c >= 0x7f:
			writeEscape(b, 'x', rune(c), 2)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(quote)
	if b.Len() > limit {
		return errors.New(tooMuchMemory)
	}
	return nil
}

// writeEscape writes r as an escape like \x00, \u00ff or \U0001f600, with
// the given number of hex digits.
func writeEscape(b *strings.Builder, kind byte, r rune, digits int) {
	const hexDigits = "0123456789abcdef"
	b.WriteByte('\\')
	b.WriteByte(kind)
	for shift := (digits - 1) * 4; shift >= 0; shift -= 4 {
		b.WriteByte(hexDigits[(r>>shift)&0xf])
	}
}

// pyASCII returns Python's ascii(v): repr() with non-ASCII letters escaped.
func pyASCII(v starlark.Value) (string, error) {
	text, err := pyRepr(v)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range text {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	return b.String(), nil
}

// pyTypeName returns the name Python uses for the type of v.
func pyTypeName(v starlark.Value) string {
	switch v.(type) {
	case starlark.String:
		return "str"
	case starlark.NoneType:
		return "NoneType"
	case *starlark.Function:
		return "function"
	case *starlark.Builtin:
		return "builtin_function_or_method"
	}
	return v.Type()
}

// pyFloatRepr returns Python's repr() of a float, for example "1e+16",
// "1000000000000000.0" and "inf".
func pyFloatRepr(x float64) string {
	return formatFloatShort(x, 'r', 0, false, true, false)
}

// formatSpec holds a parsed format spec:
// [[fill]align][sign]["z"]["#"]["0"][width][grouping]["." precision][type]
type formatSpec struct {
	fill       rune
	align      rune
	sign       rune
	coerceZero bool
	alternate  bool
	width      int // -1 when not given
	grouping   rune
	precision  int // -1 when not given
	typ        rune
}

// parseFormatSpec reads a spec the way CPython does. defaultAlign is '<' for
// text and '>' for numbers.
func parseFormatSpec(spec string, defaultAlign rune, typeName string) (formatSpec, error) {
	fs := formatSpec{fill: ' ', align: defaultAlign, width: -1, precision: -1}
	r := []rune(spec)
	pos := 0
	isAlign := func(c rune) bool { return c == '<' || c == '>' || c == '=' || c == '^' }
	fillGiven, alignGiven := false, false
	if len(r) >= 2 && isAlign(r[1]) {
		fs.fill, fs.align = r[0], r[1]
		fillGiven, alignGiven = true, true
		pos = 2
	} else if len(r) >= 1 && isAlign(r[0]) {
		fs.align = r[0]
		alignGiven = true
		pos = 1
	}
	if pos < len(r) && (r[pos] == '+' || r[pos] == '-' || r[pos] == ' ') {
		fs.sign = r[pos]
		pos++
	}
	if pos < len(r) && r[pos] == 'z' {
		fs.coerceZero = true
		pos++
	}
	if pos < len(r) && r[pos] == '#' {
		fs.alternate = true
		pos++
	}
	if !fillGiven && pos < len(r) && r[pos] == '0' {
		fs.fill = '0'
		if !alignGiven && defaultAlign == '>' {
			fs.align = '='
		}
		pos++
	}
	readNumber := func() (int, bool, error) {
		start := pos
		n := 0
		for pos < len(r) {
			d, ok := decimalValue(r[pos])
			if !ok {
				break
			}
			if n > (math.MaxInt-d)/10 {
				return 0, false, errors.New("Too many decimal digits in format string")
			}
			n = n*10 + d
			pos++
		}
		return n, pos > start, nil
	}
	width, ok, err := readNumber()
	if err != nil {
		return fs, err
	}
	if ok {
		fs.width = width
	}
	if pos < len(r) && r[pos] == ',' {
		fs.grouping = ','
		pos++
	}
	if pos < len(r) && r[pos] == '_' {
		if fs.grouping != 0 {
			return fs, errors.New("Cannot specify both ',' and '_'.")
		}
		fs.grouping = '_'
		pos++
	}
	if pos < len(r) && r[pos] == ',' && fs.grouping == '_' {
		return fs, errors.New("Cannot specify both ',' and '_'.")
	}
	if pos < len(r) && r[pos] == '.' {
		pos++
		precision, ok, err := readNumber()
		if err != nil {
			return fs, err
		}
		if !ok {
			return fs, errors.New("Format specifier missing precision")
		}
		fs.precision = precision
	}
	if len(r)-pos > 1 {
		return fs, fmt.Errorf("Invalid format specifier '%s' for object of type '%s'", spec, typeName)
	}
	if pos < len(r) {
		fs.typ = r[pos]
	}
	if fs.grouping != 0 {
		switch fs.typ {
		case 'd', 'e', 'f', 'g', 'E', 'G', '%', 'F', 0:
		case 'b', 'o', 'x', 'X':
			if fs.grouping != '_' {
				return fs, groupingError(fs)
			}
		default:
			return fs, groupingError(fs)
		}
	}
	// A huge width would make a huge text in one step, before the memory
	// watch could stop the checker.
	if fillSize := utf8.RuneLen(fs.fill); fs.width > 0 && uint64(fs.width)*uint64(max(fillSize, 1)) > maxRepeatBytes {
		return fs, errors.New(tooMuchMemory)
	}
	return fs, nil
}

func groupingError(fs formatSpec) error {
	return fmt.Errorf("Cannot specify '%c' with %s.", fs.grouping, typeCode(fs.typ))
}

func typeCode(c rune) string {
	if c > 32 && c < 128 {
		return fmt.Sprintf("'%c'", c)
	}
	return fmt.Sprintf(`'\x%x'`, c)
}

func unknownCode(c rune, typeName string) error {
	return fmt.Errorf("Unknown format code %s for object of type '%s'", typeCode(c), typeName)
}

// formatValue returns Python's format(v, spec).
func formatValue(v starlark.Value, spec string) (string, error) {
	switch v := v.(type) {
	case starlark.String:
		return formatString(string(v), spec)
	case starlark.Bool:
		if spec == "" {
			return v.String(), nil
		}
		n := big.NewInt(0)
		if v {
			n.SetInt64(1)
		}
		return formatInt(n, spec, "bool")
	case starlark.Int:
		if spec == "" {
			return intValueText(v)
		}
		return formatInt(v.BigInt(), spec, "int")
	case starlark.Float:
		if spec == "" {
			return pyFloatRepr(float64(v)), nil
		}
		return formatFloat(float64(v), spec, "float")
	}
	if spec == "" {
		return pyStr(v)
	}
	return "", fmt.Errorf("unsupported format string passed to %s.__format__", pyTypeName(v))
}

func formatString(s, spec string) (string, error) {
	if spec == "" {
		return s, nil
	}
	fs, err := parseFormatSpec(spec, '<', "str")
	if err != nil {
		return "", err
	}
	switch {
	case fs.typ != 0 && fs.typ != 's':
		return "", unknownCode(fs.typ, "str")
	case fs.grouping != 0:
		return "", fmt.Errorf("Cannot specify '%c' with 's'.", fs.grouping)
	case fs.sign == ' ':
		return "", errors.New("Space not allowed in string format specifier")
	case fs.sign != 0:
		return "", errors.New("Sign not allowed in string format specifier")
	case fs.coerceZero:
		return "", errors.New("Negative zero coercion (z) not allowed in string format specifier")
	case fs.alternate:
		return "", errors.New("Alternate form (#) not allowed in string format specifier")
	case fs.align == '=':
		return "", errors.New("'=' alignment not allowed in string format specifier")
	}
	if fs.precision >= 0 {
		s = firstRunes(s, fs.precision)
	}
	return pad(s, fs), nil
}

// firstRunes returns the first n characters of s.
func firstRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// pad adds fill characters to reach the width, for text and for numbers
// that need no '=' alignment.
func pad(s string, fs formatSpec) string {
	n := fs.width - utf8.RuneCountInString(s)
	if n <= 0 {
		return s
	}
	fill := string(fs.fill)
	switch fs.align {
	case '>':
		return strings.Repeat(fill, n) + s
	case '^':
		return strings.Repeat(fill, n/2) + s + strings.Repeat(fill, n-n/2)
	}
	return s + strings.Repeat(fill, n)
}

func formatInt(n *big.Int, spec, typeName string) (string, error) {
	fs, err := parseFormatSpec(spec, '>', typeName)
	if err != nil {
		return "", err
	}
	switch fs.typ {
	case 'e', 'E', 'f', 'F', 'g', 'G', '%':
		x, accuracy := new(big.Float).SetInt(n).Float64()
		if math.IsInf(x, 0) && accuracy != big.Exact {
			return "", errors.New("int too large to convert to float")
		}
		return formatFloatSpec(x, fs)
	case 0, 'd', 'n', 'b', 'o', 'x', 'X', 'c':
	default:
		return "", unknownCode(fs.typ, typeName)
	}
	if fs.precision >= 0 {
		return "", errors.New("Precision not allowed in integer format specifier")
	}
	if fs.coerceZero {
		return "", errors.New("Negative zero coercion (z) not allowed in integer format specifier")
	}
	if fs.typ == 'c' {
		if fs.sign != 0 {
			return "", errors.New("Sign not allowed with integer format specifier 'c'")
		}
		if fs.alternate {
			return "", errors.New("Alternate form (#) not allowed with integer format specifier 'c'")
		}
		if n.Sign() < 0 || n.Cmp(big.NewInt(0x10ffff)) > 0 {
			return "", errors.New("%c arg not in range(0x110000)")
		}
		return assembleNumber("", "", "", string(rune(n.Int64())), fs, 0), nil
	}
	base, prefix := 10, ""
	switch fs.typ {
	case 'b':
		base, prefix = 2, "0b"
	case 'o':
		base, prefix = 8, "0o"
	case 'x':
		base, prefix = 16, "0x"
	case 'X':
		base, prefix = 16, "0X"
	}
	if !fs.alternate {
		prefix = ""
	}
	// Python limits only decimal text: other bases are quick to make.
	var digits string
	if base == 10 {
		text, err := intText(new(big.Int).Abs(n))
		if err != nil {
			return "", err
		}
		digits = text
	} else {
		digits = new(big.Int).Abs(n).Text(base)
	}
	if fs.typ == 'X' {
		digits = strings.ToUpper(digits)
	}
	sign := ""
	if n.Sign() < 0 {
		sign = "-"
	}
	groupSize := 3
	if base != 10 {
		groupSize = 4
	}
	return assembleNumber(sign, prefix, digits, "", fs, groupSize), nil
}

func formatFloat(x float64, spec, typeName string) (string, error) {
	fs, err := parseFormatSpec(spec, '>', typeName)
	if err != nil {
		return "", err
	}
	switch fs.typ {
	case 0, 'e', 'E', 'f', 'F', 'g', 'G', 'n', '%':
	default:
		return "", unknownCode(fs.typ, typeName)
	}
	return formatFloatSpec(x, fs)
}

// formatFloatSpec follows format_float_internal in CPython.
func formatFloatSpec(x float64, fs formatSpec) (string, error) {
	if fs.precision > maxFloatPrecision {
		return "", errors.New(tooMuchMemory)
	}
	code, precision, addDot0, percent := fs.typ, fs.precision, false, false
	defaultPrecision := 6
	if code == 0 {
		addDot0, code, defaultPrecision = true, 'r', 0
	}
	if code == 'n' {
		code = 'g'
	}
	if code == '%' {
		code, x, percent = 'f', x*100, true
	}
	if precision < 0 {
		precision = defaultPrecision
	} else if code == 'r' {
		code = 'g'
	}
	text := formatFloatShort(x, code, precision, fs.alternate, addDot0, fs.coerceZero)
	if percent {
		text += "%"
	}
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	end := 0
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	return assembleNumber(sign, "", text[:end], text[end:], fs, 3), nil
}

// assembleNumber puts together sign, prefix ("0x"), the digits before the
// decimal point (grouped when asked) and the rest, then pads to the width.
// It follows calc_number_widths and fill_number in CPython.
func assembleNumber(sign, prefix, digits, rest string, fs formatSpec, groupSize int) string {
	switch fs.sign {
	case '+':
		if sign == "" {
			sign = "+"
		}
	case ' ':
		if sign == "" {
			sign = " "
		}
	}
	nonDigits := len(sign) + len(prefix) + utf8.RuneCountInString(rest)
	if fs.grouping != 0 && digits != "" {
		minWidth := 0
		if fs.fill == '0' && fs.align == '=' {
			minWidth = fs.width - nonDigits
		}
		digits = groupDigits(digits, byte(fs.grouping), groupSize, minWidth)
	}
	n := fs.width - nonDigits - len(digits)
	if n <= 0 {
		return sign + prefix + digits + rest
	}
	fill := string(fs.fill)
	switch fs.align {
	case '<':
		return sign + prefix + digits + rest + strings.Repeat(fill, n)
	case '^':
		return strings.Repeat(fill, n/2) + sign + prefix + digits + rest + strings.Repeat(fill, n-n/2)
	case '=':
		return sign + prefix + strings.Repeat(fill, n) + digits + rest
	}
	return strings.Repeat(fill, n) + sign + prefix + digits + rest
}

// groupDigits puts sep between groups of size digits, counted from the right.
// With minWidth it first adds leading zeros (grouped too) until the text is
// at least that wide, the way Python pads format(1234, "010,").
func groupDigits(digits string, sep byte, size, minWidth int) string {
	if need := digitsForWidth(minWidth, size); need > len(digits) {
		digits = strings.Repeat("0", need-len(digits)) + digits
	}
	n := len(digits)
	var b strings.Builder
	b.Grow(n + n/size)
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%size == 0 {
			b.WriteByte(sep)
		}
		b.WriteByte(digits[i])
	}
	return b.String()
}

// digitsForWidth returns the fewest digits that, grouped by size, make a
// text at least width long. n digits make n + (n-1)/size characters. A text
// never starts with a separator, so when width would end on one, one more
// digit is needed.
func digitsForWidth(width, size int) int {
	if width <= 1 {
		return 1
	}
	groups, rest := (width-1)/(size+1), (width-1)%(size+1)
	if rest < size {
		return groups*size + rest + 1
	}
	return (groups+1)*size + 1
}

// floatDigits returns the decimal digits of |x| without leading and trailing
// zeros, and the place of the decimal point (x = 0.DIGITS * 10**decpt), like
// _Py_dg_dtoa. Mode 0 gives the shortest digits that read back as x, mode 2
// gives n significant digits, mode 3 gives n digits after the decimal point.
func floatDigits(x float64, mode, n int) (string, int) {
	x = math.Abs(x)
	if x == 0 {
		return "0", 1
	}
	if mode == 3 {
		whole, fraction, _ := strings.Cut(strconv.FormatFloat(x, 'f', n, 64), ".")
		all := whole + fraction
		trimmed := strings.TrimLeft(all, "0")
		decpt := len(whole) - (len(all) - len(trimmed))
		trimmed = strings.TrimRight(trimmed, "0")
		if trimmed == "" {
			return "", -n
		}
		return trimmed, decpt
	}
	precision := -1
	if mode == 2 {
		precision = n - 1
	}
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(x, 'e', precision, 64), "e")
	e, _ := strconv.Atoi(exponent)
	digits := strings.TrimRight(strings.Replace(mantissa, ".", "", 1), "0")
	return digits, e + 1
}

// formatFloatShort follows format_float_short in CPython. code is one of
// 'r' (repr), 'e', 'f', 'g' and their upper case forms.
func formatFloatShort(x float64, code rune, precision int, alternate, addDot0, coerceZero bool) string {
	upper := unicode.IsUpper(code)
	code = unicode.ToLower(code)
	if math.IsInf(x, 0) || math.IsNaN(x) {
		text := "inf"
		if math.IsNaN(x) {
			text = "nan"
		}
		if upper {
			text = strings.ToUpper(text)
		}
		if math.IsInf(x, -1) {
			text = "-" + text
		}
		return text
	}
	var digits string
	var decpt int
	switch code {
	case 'r':
		digits, decpt = floatDigits(x, 0, 0)
	case 'e':
		precision++
		digits, decpt = floatDigits(x, 2, precision)
	case 'f':
		digits, decpt = floatDigits(x, 3, precision)
	case 'g':
		if precision == 0 {
			precision = 1
		}
		digits, decpt = floatDigits(x, 2, precision)
	}
	negative := math.Signbit(x)
	if coerceZero && negative && (digits == "" || digits == "0") {
		negative = false
	}

	useExp := false
	end := len(digits)
	switch code {
	case 'e':
		useExp = true
		end = precision
	case 'f':
		end = decpt + precision
	case 'g':
		limit := precision
		if addDot0 {
			limit = precision - 1
		}
		if decpt <= -4 || decpt > limit {
			useExp = true
		}
		if alternate {
			end = precision
		}
	case 'r':
		if decpt <= -4 || decpt > 16 {
			useExp = true
		}
	}
	exp := 0
	if useExp {
		exp = decpt - 1
		decpt = 1
	}
	start := 0
	if decpt <= 0 {
		start = decpt - 1
	}
	if !useExp && addDot0 {
		end = max(end, decpt+1)
	} else {
		end = max(end, decpt)
	}

	// digit returns the digit at index i of digits padded with zeros on
	// both sides.
	digit := func(i int) byte {
		if i < 0 || i >= len(digits) {
			return '0'
		}
		return digits[i]
	}
	var b strings.Builder
	if negative {
		b.WriteByte('-')
	}
	for i := start; i < end; i++ {
		if i == decpt {
			b.WriteByte('.')
		}
		b.WriteByte(digit(i))
	}
	if decpt == end {
		b.WriteByte('.')
	}
	text := b.String()
	if strings.HasSuffix(text, ".") && !alternate {
		text = text[:len(text)-1]
	}
	if useExp {
		e := "e"
		if upper {
			e = "E"
		}
		sign := "+"
		if exp < 0 {
			sign, exp = "-", -exp
		}
		text += fmt.Sprintf("%s%s%02d", e, sign, exp)
	}
	return text
}
