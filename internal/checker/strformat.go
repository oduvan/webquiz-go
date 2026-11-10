package checker

// Python's text.format(...) and text % values. Starlark has both, but
// without width or precision and without a limit on the size of the result.
// These versions follow Python 3 and stop when the text would grow larger
// than maxRepeatBytes.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.starlark.net/starlark"
)

// textBuilder builds a text and refuses to grow past maxRepeatBytes.
type textBuilder struct {
	b strings.Builder
}

func (t *textBuilder) write(s string) error {
	if t.b.Len()+len(s) > maxRepeatBytes {
		return errors.New(tooMuchMemory)
	}
	t.b.WriteString(s)
	return nil
}

// formatArgs holds the arguments of text.format() and the state of the
// automatic field numbering.
type formatArgs struct {
	args   starlark.Tuple
	kwargs []starlark.Tuple
	next   int // the next automatic field number
	auto   bool
	manual bool
}

// pyFormatString is Python's template.format(*args, **kwargs).
func pyFormatString(template string, args starlark.Tuple, kwargs []starlark.Tuple) (string, error) {
	fa := &formatArgs{args: args, kwargs: kwargs}
	if fa.lowerBound(template) > maxRepeatBytes {
		return "", errors.New(tooMuchMemory)
	}
	var out textBuilder
	if err := fa.render(&out, template, 0); err != nil {
		return "", err
	}
	return out.b.String(), nil
}

// lowerBound adds up the literal text of template and the text arguments
// of its plain fields, like {} or {0}. The result can only be longer, so
// when this is too large, format() can refuse before it builds anything.
func (fa *formatArgs) lowerBound(template string) uint64 {
	var size uint64
	next := 0
	for i := 0; i < len(template); i++ {
		switch {
		case strings.HasPrefix(template[i:], "{{"), strings.HasPrefix(template[i:], "}}"):
			size++
			i++
			continue
		case template[i] != '{':
			size++
			continue
		}
		end := fieldEnd(template, i)
		if end < 0 {
			return size
		}
		var value starlark.Value
		field := template[i+1 : end]
		if index, err := strconv.Atoi(field); err == nil && index >= 0 && index < len(fa.args) {
			value = fa.args[index]
		} else if field == "" && next < len(fa.args) {
			value = fa.args[next]
			next++
		} else {
			for _, kv := range fa.kwargs {
				if string(kv[0].(starlark.String)) == field {
					value = kv[1]
				}
			}
		}
		if text, ok := value.(starlark.String); ok {
			size += uint64(len(text))
		}
		i = end
	}
	return size
}

// render writes template with its fields filled in. depth counts nested
// fields inside format specs; Python allows one level.
func (fa *formatArgs) render(out *textBuilder, template string, depth int) error {
	if depth > 1 {
		return errors.New("Max string recursion exceeded")
	}
	for i := 0; i < len(template); {
		c := template[i]
		switch {
		case c == '{' && strings.HasPrefix(template[i:], "{{"):
			if err := out.write("{"); err != nil {
				return err
			}
			i += 2
		case c == '}' && strings.HasPrefix(template[i:], "}}"):
			if err := out.write("}"); err != nil {
				return err
			}
			i += 2
		case c == '}':
			return errors.New("Single '}' encountered in format string")
		case c == '{':
			end := fieldEnd(template, i)
			if end < 0 {
				if i == len(template)-1 {
					return errors.New("Single '{' encountered in format string")
				}
				return errors.New("expected '}' before end of string")
			}
			if err := fa.field(out, template[i+1:end], depth); err != nil {
				return err
			}
			i = end + 1
		default:
			next := strings.IndexAny(template[i:], "{}")
			if next < 0 {
				next = len(template) - i
			}
			if err := out.write(template[i : i+next]); err != nil {
				return err
			}
			i += next
		}
	}
	return nil
}

// fieldEnd returns the index of the "}" that closes the field opened at
// start, or -1. A format spec may hold nested fields.
func fieldEnd(template string, start int) int {
	depth := 0
	for i := start; i < len(template); i++ {
		switch template[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// field writes one replacement field: name[!conversion][:spec].
func (fa *formatArgs) field(out *textBuilder, field string, depth int) error {
	nameEnd := len(field)
	brackets := 0
	for i := 0; i < len(field); i++ {
		if field[i] == '[' {
			brackets++
		} else if field[i] == ']' {
			brackets--
		} else if brackets == 0 && (field[i] == '!' || field[i] == ':') {
			nameEnd = i
			break
		}
	}
	name, rest := field[:nameEnd], field[nameEnd:]
	conversion := ""
	if strings.HasPrefix(rest, "!") {
		if len(rest) < 2 {
			return errors.New("end of string while looking for conversion specifier")
		}
		conversion, rest = rest[1:2], rest[2:]
		if rest != "" && rest[0] != ':' {
			return errors.New("expected ':' after conversion specifier")
		}
	}
	spec := strings.TrimPrefix(rest, ":")
	value, err := fa.value(name)
	if err != nil {
		return err
	}
	var text string
	switch conversion {
	case "":
	case "r":
		text, err = pyRepr(value)
	case "s":
		text, err = pyStr(value)
	case "a":
		text, err = pyASCII(value)
	default:
		return fmt.Errorf("Unknown conversion specifier %s", conversion)
	}
	if err != nil {
		return err
	}
	if conversion != "" {
		value = starlark.String(text)
	}
	if strings.Contains(spec, "{") {
		var expanded textBuilder
		if err := fa.render(&expanded, spec, depth+1); err != nil {
			return err
		}
		spec = expanded.b.String()
	}
	formatted, err := formatValue(value, spec)
	if err != nil {
		return err
	}
	return out.write(formatted)
}

// value finds the value of a field name like "", "0", "name", "0[1]" or
// "point.x".
func (fa *formatArgs) value(name string) (starlark.Value, error) {
	first := strings.IndexAny(name, ".[")
	if first < 0 {
		first = len(name)
	}
	argName, accessors := name[:first], name[first:]
	var value starlark.Value
	switch index, err := strconv.Atoi(argName); {
	case argName == "":
		if fa.manual {
			return nil, errors.New("cannot switch from manual field specification to automatic field numbering")
		}
		fa.auto = true
		if fa.next >= len(fa.args) {
			return nil, fmt.Errorf("Replacement index %d out of range for positional args tuple", fa.next)
		}
		value = fa.args[fa.next]
		fa.next++
	case err == nil:
		if fa.auto {
			return nil, errors.New("cannot switch from automatic field numbering to manual field specification")
		}
		fa.manual = true
		if index >= len(fa.args) {
			return nil, fmt.Errorf("Replacement index %d out of range for positional args tuple", index)
		}
		value = fa.args[index]
	default:
		for _, kv := range fa.kwargs {
			if string(kv[0].(starlark.String)) == argName {
				value = kv[1]
			}
		}
		if value == nil {
			return nil, errors.New(pyReprString(argName))
		}
	}
	for accessors != "" {
		var err error
		if accessors[0] == '.' {
			end := strings.IndexAny(accessors[1:], ".[")
			if end < 0 {
				end = len(accessors) - 1
			}
			if value, err = attr(value, accessors[1:1+end]); err != nil {
				return nil, err
			}
			accessors = accessors[1+end:]
			continue
		}
		end := strings.IndexByte(accessors, ']')
		if end < 0 {
			return nil, errors.New("Missing ']' in format string")
		}
		key := accessors[1:end]
		accessors = accessors[end+1:]
		if value, err = index(value, key); err != nil {
			return nil, err
		}
	}
	return value, nil
}

// index is value[key] in a format field: a key of digits is a number.
func index(value starlark.Value, key string) (starlark.Value, error) {
	if n, err := strconv.Atoi(key); err == nil {
		if seq, ok := value.(starlark.Indexable); ok {
			if n >= seq.Len() {
				return nil, fmt.Errorf("%s index out of range", pyTypeName(value))
			}
			return seq.Index(n), nil
		}
		if m, ok := value.(starlark.Mapping); ok {
			return mappingGet(m, starlark.MakeInt(n))
		}
	} else if m, ok := value.(starlark.Mapping); ok {
		return mappingGet(m, starlark.String(key))
	}
	return nil, fmt.Errorf("'%s' object is not subscriptable", pyTypeName(value))
}

func mappingGet(m starlark.Mapping, key starlark.Value) (starlark.Value, error) {
	v, found, err := m.Get(key)
	if err != nil {
		return nil, err
	}
	if !found {
		text, err := pyRepr(key)
		if err != nil {
			return nil, err
		}
		return nil, errors.New(text)
	}
	return v, nil
}

// pyPercentFormat is Python's template % values.
func pyPercentFormat(template string, values starlark.Value) (string, error) {
	var args starlark.Tuple
	var mapping starlark.Mapping
	if tuple, ok := values.(starlark.Tuple); ok {
		args = tuple
	} else {
		args = starlark.Tuple{values}
		if m, ok := values.(starlark.Mapping); ok {
			mapping = m
		}
	}
	if mapping == nil && percentLowerBound(template, args) > maxRepeatBytes {
		return "", errors.New(tooMuchMemory)
	}
	next := 0
	nextArg := func() (starlark.Value, error) {
		if next >= len(args) {
			return nil, errors.New("not enough arguments for format string")
		}
		next++
		return args[next-1], nil
	}
	var out textBuilder
	usedKey := false
	for i := 0; i < len(template); {
		if template[i] != '%' {
			end := strings.IndexByte(template[i:], '%')
			if end < 0 {
				end = len(template) - i
			}
			if err := out.write(template[i : i+end]); err != nil {
				return "", err
			}
			i += end
			continue
		}
		i++
		if i >= len(template) {
			return "", errors.New("incomplete format")
		}
		var value starlark.Value
		if template[i] == '(' {
			if mapping == nil {
				return "", errors.New("format requires a mapping")
			}
			depth, start := 1, i+1
			for i++; i < len(template) && depth > 0; i++ {
				if template[i] == '(' {
					depth++
				} else if template[i] == ')' {
					depth--
				}
			}
			if depth > 0 {
				return "", errors.New("incomplete format key")
			}
			v, err := mappingGet(mapping, starlark.String(template[start:i-1]))
			if err != nil {
				return "", err
			}
			value, usedKey = v, true
		}
		var flags string
		for i < len(template) && strings.IndexByte("-+ #0", template[i]) >= 0 {
			flags += template[i : i+1]
			i++
		}
		readNumber := func() (int, error) {
			if i < len(template) && template[i] == '*' {
				i++
				v, err := nextArg()
				if err != nil {
					return 0, err
				}
				n, ok := v.(starlark.Int)
				if !ok {
					return 0, errors.New("* wants int")
				}
				small, ok := n.Int64()
				if !ok || small > maxRepeatBytes || small < -maxRepeatBytes {
					return 0, errors.New(tooMuchMemory)
				}
				return int(small), nil
			}
			n := 0
			for ; i < len(template) && template[i] >= '0' && template[i] <= '9'; i++ {
				if n > maxRepeatBytes {
					return 0, errors.New(tooMuchMemory)
				}
				n = n*10 + int(template[i]-'0')
			}
			return n, nil
		}
		width, err := readNumber()
		if err != nil {
			return "", err
		}
		if width < 0 {
			// A negative width from "*" means "align left", as in Python.
			flags, width = flags+"-", -width
		}
		precision := -1
		if i < len(template) && template[i] == '.' {
			i++
			if precision, err = readNumber(); err != nil {
				return "", err
			}
			precision = max(precision, 0)
		}
		for i < len(template) && strings.IndexByte("hlL", template[i]) >= 0 {
			i++
		}
		if i >= len(template) {
			return "", errors.New("incomplete format")
		}
		conv := template[i]
		i++
		if conv == '%' {
			if err := out.write("%"); err != nil {
				return "", err
			}
			continue
		}
		if value == nil {
			if value, err = nextArg(); err != nil {
				return "", err
			}
		}
		text, err := percentField(conv, i-1, value, flags, width, precision)
		if err != nil {
			return "", err
		}
		if err := out.write(text); err != nil {
			return "", err
		}
	}
	if next < len(args) && mapping == nil && !usedKey {
		return "", errors.New("not all arguments converted during string formatting")
	}
	return out.b.String(), nil
}

// percentLowerBound adds up the literal text of template and the text
// arguments of its plain "%s" fields, up to the first other field. The
// result can only be longer, so when this is too large, "%" can refuse
// before it builds anything.
func percentLowerBound(template string, args starlark.Tuple) uint64 {
	var size uint64
	next := 0
	for i := 0; i < len(template); i++ {
		switch {
		case template[i] != '%':
			size++
		case strings.HasPrefix(template[i:], "%%"):
			size++
			i++
		case strings.HasPrefix(template[i:], "%s"):
			if next < len(args) {
				if text, ok := args[next].(starlark.String); ok {
					size += uint64(len(text))
				}
			}
			next++
			i++
		default:
			return size
		}
	}
	return size
}

// percentField formats one "%" field with Python's rules.
func percentField(conv byte, at int, value starlark.Value, flags string, width, precision int) (string, error) {
	fs := formatSpec{fill: ' ', align: '>', width: width, precision: -1}
	if width == 0 {
		fs.width = -1
	}
	switch {
	case strings.Contains(flags, "-"):
		fs.align = '<'
	case strings.Contains(flags, "0"):
		fs.fill, fs.align = '0', '='
	}
	switch {
	case strings.Contains(flags, "+"):
		fs.sign = '+'
	case strings.Contains(flags, " "):
		fs.sign = ' '
	}
	fs.alternate = strings.Contains(flags, "#")
	if fs.width > 0 && fs.width > maxRepeatBytes {
		return "", errors.New(tooMuchMemory)
	}
	switch conv {
	case 's', 'r', 'a':
		var text string
		var err error
		switch conv {
		case 's':
			text, err = pyStr(value)
		case 'r':
			text, err = pyRepr(value)
		default:
			text, err = pyASCII(value)
		}
		if err != nil {
			return "", err
		}
		if precision >= 0 {
			text = firstRunes(text, precision)
		}
		if fs.align == '=' {
			fs.fill, fs.align = ' ', '>'
		}
		return pad(text, fs), nil
	case 'c':
		var text string
		switch v := value.(type) {
		case starlark.Int:
			n, ok := v.Int64()
			if !ok || n < 0 || n > 0x10ffff {
				return "", errors.New("%c arg not in range(0x110000)")
			}
			text = string(rune(n))
		case starlark.String:
			if utf8.RuneCountInString(string(v)) != 1 {
				return "", errors.New("%c requires int or char")
			}
			text = string(v)
		default:
			return "", errors.New("%c requires int or char")
		}
		if fs.align == '=' {
			fs.fill, fs.align = ' ', '>'
		}
		return pad(text, fs), nil
	case 'd', 'i', 'u', 'o', 'x', 'X':
		n, err := percentInt(conv, value)
		if err != nil {
			return "", err
		}
		return percentInteger(conv, n, fs, precision)
	case 'e', 'E', 'f', 'F', 'g', 'G':
		x, ok := starlark.AsFloat(value)
		if !ok {
			return "", fmt.Errorf("must be real number, not %s", pyTypeName(value))
		}
		if precision < 0 {
			precision = 6
		}
		fs.precision, fs.typ = precision, rune(conv)
		return formatFloatSpec(x, fs)
	}
	return "", fmt.Errorf("unsupported format character '%c' (0x%x) at index %d", conv, conv, at)
}

// percentInt returns the integer that %d, %o or %x formats.
func percentInt(conv byte, value starlark.Value) (*big.Int, error) {
	switch v := value.(type) {
	case starlark.Bool:
		return big.NewInt(int64(boolInt(v))), nil
	case starlark.Int:
		return v.BigInt(), nil
	case starlark.Float:
		if conv == 'd' || conv == 'i' || conv == 'u' {
			f := float64(v)
			if math.IsInf(f, 0) {
				return nil, errors.New("cannot convert float infinity to integer")
			}
			if math.IsNaN(f) {
				return nil, errors.New("cannot convert float NaN to integer")
			}
			n, _ := big.NewFloat(f).Int(nil)
			return n, nil
		}
		return nil, fmt.Errorf("%%%c format: an integer is required, not float", conv)
	}
	if conv == 'd' || conv == 'i' || conv == 'u' {
		return nil, fmt.Errorf("%%%c format: a real number is required, not %s", conv, pyTypeName(value))
	}
	return nil, fmt.Errorf("%%%c format: an integer is required, not %s", conv, pyTypeName(value))
}

// percentInteger formats an integer for %d, %o, %x or %X. A precision is
// the least number of digits, as in C.
func percentInteger(conv byte, n *big.Int, fs formatSpec, precision int) (string, error) {
	abs := new(big.Int).Abs(n)
	var digits, prefix string
	switch conv {
	case 'o':
		digits, prefix = abs.Text(8), "0o"
	case 'x':
		digits, prefix = abs.Text(16), "0x"
	case 'X':
		digits, prefix = strings.ToUpper(abs.Text(16)), "0X"
	default:
		text, err := intText(abs)
		if err != nil {
			return "", err
		}
		digits = text
	}
	if !fs.alternate || conv == 'd' || conv == 'i' || conv == 'u' {
		prefix = ""
	}
	if precision > maxFloatPrecision {
		return "", errors.New(tooMuchMemory)
	}
	if precision > len(digits) {
		digits = strings.Repeat("0", precision-len(digits)) + digits
	}
	sign := ""
	if n.Sign() < 0 {
		sign = "-"
	}
	return assembleNumber(sign, prefix, digits, "", fs, 3), nil
}
