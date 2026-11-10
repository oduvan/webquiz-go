package checker

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

// The error texts of distance and direction_angle, copied from
// webquiz/checker.py. Students see them as the checker error.
const (
	distanceErrorText = "Неверный формат ответа. Ожидается число, например: '2000', '2000м', '2км'."
	angleErrorText    = "Неверный формат ответа. Ожидается число в градусах, например: '2000' или '20-00'."
)

// ToInt converts a student's answer to an integer, like to_int in Python:
// int(answer.strip()).
func ToInt(s string) (int, error) {
	n, err := toInt(s)
	if err != nil {
		return 0, err
	}
	return bigToInt(n, s)
}

// Distance reads a distance in meters: "2000", "2000m", "2000м", "2km",
// "2км", "0.5km" and "2км." all work. It is distance from webquiz/checker.py.
func Distance(s string) (int, error) {
	n, err := distance(s)
	if err != nil {
		return 0, err
	}
	if v, ok := fitsInt(n); ok {
		return v, nil
	}
	return 0, errors.New(distanceErrorText)
}

// DirectionAngle reads a direction angle: "20" is 2000 and "20-30" is 2030.
// It is direction_angle from webquiz/checker.py.
func DirectionAngle(s string) (int, error) {
	n, err := directionAngle(s)
	if err != nil {
		return 0, err
	}
	return bigToInt(n, s)
}

func toInt(s string) (*big.Int, error) {
	return pyInt(pyStrip(s))
}

func distance(s string) (*big.Int, error) {
	s = strings.TrimRight(pyStrip(s), ".")
	s = strings.ReplaceAll(strings.ToLower(s), "км", "km")
	s = strings.ReplaceAll(s, "м", "m")
	var value float64
	var ok bool
	switch {
	case strings.HasSuffix(s, "km"):
		value, ok = parsePyFloat(pyStrip(s[:len(s)-2]))
		value *= 1000
	case strings.HasSuffix(s, "m"):
		value, ok = parsePyFloat(pyStrip(s[:len(s)-1]))
	default:
		value, ok = parsePyFloat(s)
	}
	if !ok || math.IsInf(value, 0) || math.IsNaN(value) {
		return nil, errors.New(distanceErrorText)
	}
	// Python's int() of a float drops the fraction.
	n, _ := big.NewFloat(value).Int(nil)
	return n, nil
}

func directionAngle(s string) (*big.Int, error) {
	parts := strings.Split(pyStrip(s), "-")
	if len(parts) > 2 {
		return nil, errors.New(angleErrorText)
	}
	degrees, err := pyInt(pyStrip(parts[0]))
	if err != nil {
		return nil, err
	}
	minutes := big.NewInt(0)
	if len(parts) == 2 {
		if minutes, err = pyInt(pyStrip(parts[1])); err != nil {
			return nil, err
		}
	}
	result := new(big.Int).Mul(degrees, big.NewInt(100))
	return result.Add(result, minutes), nil
}

// bigToInt returns n as an int. Python has no size limit for integers;
// a Go int has, so a bigger number is an error here.
func bigToInt(n *big.Int, s string) (int, error) {
	if v, ok := fitsInt(n); ok {
		return v, nil
	}
	text, err := pyReprString(s)
	if err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("number is too large: %s", text)
}

func fitsInt(n *big.Int) (int, bool) {
	if !n.IsInt64() {
		return 0, false
	}
	v := n.Int64()
	if int64(int(v)) != v {
		return 0, false
	}
	return int(v), true
}

// pyIsSpace tells whether Python's str.isspace() is true for r.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// pyStrip removes whitespace at both ends, like Python's str.strip().
func pyStrip(s string) string {
	return strings.TrimFunc(s, pyIsSpace)
}

// maxIntDigits is the longest decimal text that int() reads, as in Python
// 3.11 and later (sys.get_int_max_str_digits()). Longer text would also take
// Go a long time to read.
const maxIntDigits = 4300

// asciiSpaces are the spaces that int() and float() remove around a number.
const asciiSpaces = " \t\n\v\f\r"

// pyInt reads text the way Python's int(text) does with base 10: spaces
// around the number, an optional sign, and digits (any Unicode decimal
// digit) with single underscores between them. It returns Python's errors.
func pyInt(s string) (*big.Int, error) {
	if body, ok := pyNumberText(s); ok {
		body = strings.Trim(body, asciiSpaces)
		sign := ""
		if body != "" && (body[0] == '+' || body[0] == '-') {
			sign, body = body[:1], body[1:]
		}
		if digits, ok := digitPart(body); ok {
			if len(digits) > maxIntDigits {
				return nil, fmt.Errorf("Exceeds the limit (%d digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", maxIntDigits, len(digits))
			}
			n, _ := new(big.Int).SetString(sign+digits, 10)
			return n, nil
		}
	}
	return nil, fmt.Errorf("invalid literal for int() with base 10: %s", shortRepr(s))
}

// pyIntBase reads text the way Python's int(text, base) does. A prefix 0x,
// 0o or 0b may name the base when base is 0 or that base.
func pyIntBase(s string, base int) (*big.Int, error) {
	if base != 0 && (base < 2 || base > 36) {
		return nil, errors.New("int() base must be >= 2 and <= 36, or 0")
	}
	if base == 10 {
		return pyInt(s)
	}
	invalid := fmt.Errorf("invalid literal for int() with base %d: %s", base, shortRepr(s))
	body, ok := pyNumberText(s)
	if !ok {
		return nil, invalid
	}
	body = strings.Trim(body, asciiSpaces)
	sign := ""
	if body != "" && (body[0] == '+' || body[0] == '-') {
		sign, body = body[:1], body[1:]
	}
	actual := base
	if len(body) >= 2 && body[0] == '0' {
		prefixBase := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[body[1]]
		if prefixBase != 0 && (base == 0 || base == prefixBase) {
			actual = prefixBase
			body = strings.TrimPrefix(body[2:], "_")
		}
	}
	if actual == 0 {
		// Base 0 reads decimal digits, but no leading zero except in zero.
		actual = 10
		if len(body) > 1 && body[0] == '0' && strings.Trim(body, "0_") != "" {
			return nil, invalid
		}
	}
	digits, ok := baseDigits(body, actual)
	if !ok {
		return nil, invalid
	}
	// Python limits the digits only where reading them takes long: for
	// bases that are not a power of two. Go is slow there too.
	if actual&(actual-1) != 0 && len(digits) > maxIntDigits {
		return nil, fmt.Errorf("Exceeds the limit (%d digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", maxIntDigits, len(digits))
	}
	n, ok := new(big.Int).SetString(sign+digits, actual)
	if !ok {
		return nil, invalid
	}
	return n, nil
}

// shortRepr is Python's "%.200R": repr() cut after 200 characters. Only
// the start of the text is made: 200 characters take at most 800 bytes.
func shortRepr(s string) string {
	var b strings.Builder
	_ = writeReprString(&b, s, 1000)
	r := b.String()
	n := 0
	for i := range r {
		if n == 200 {
			return r[:i]
		}
		n++
	}
	return r
}

// pyNumberText prepares text for int() and float() like Python does:
// spaces that are not ASCII become " " and decimal digits that are not
// ASCII (like "４" or "٤") become ASCII digits. It returns false when the
// text has any other character that is not ASCII.
func pyNumberText(s string) (string, bool) {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x7f {
			ascii = false
			break
		}
	}
	if ascii {
		return s, true
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x7f:
			b.WriteRune(r)
		case pyIsSpace(r):
			b.WriteByte(' ')
		default:
			d, ok := decimalValue(r)
			if !ok {
				return "", false
			}
			b.WriteByte(byte('0' + d))
		}
	}
	return b.String(), true
}

// decimalValue returns the value of a decimal digit of any script
// (Unicode category Nd). Unicode keeps each set of decimal digits in a row
// from 0 to 9, so the value is the place in its row.
func decimalValue(r rune) (int, bool) {
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	for _, rg := range unicode.Nd.R16 {
		if r >= rune(rg.Lo) && r <= rune(rg.Hi) && (r-rune(rg.Lo))%rune(rg.Stride) == 0 {
			return int((r-rune(rg.Lo))/rune(rg.Stride)) % 10, true
		}
	}
	for _, rg := range unicode.Nd.R32 {
		if r >= rune(rg.Lo) && r <= rune(rg.Hi) && (r-rune(rg.Lo))%rune(rg.Stride) == 0 {
			return int((r-rune(rg.Lo))/rune(rg.Stride)) % 10, true
		}
	}
	return 0, false
}

// digitPart checks that s is decimal digits with single underscores between
// them and returns the digits without the underscores.
func digitPart(s string) (string, bool) {
	return baseDigits(s, 10)
}

// baseDigits checks that s is digits of the base (0-9, then a-z in any
// case) with single underscores between them, and returns the digits
// without the underscores.
func baseDigits(s string, base int) (string, bool) {
	if s == "" || s[0] == '_' || s[len(s)-1] == '_' || strings.Contains(s, "__") {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		value := 99
		switch {
		case c == '_':
			continue
		case c >= '0' && c <= '9':
			value = int(c - '0')
		case c >= 'a' && c <= 'z':
			value = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			value = int(c-'A') + 10
		}
		if value >= base {
			return "", false
		}
	}
	return strings.ReplaceAll(s, "_", ""), true
}

// parsePyFloat reads a number the way Python's float() reads text: spaces
// around it, an optional sign, "inf", "infinity" and "nan" in any case, and
// decimal numbers (any Unicode decimal digit) with an exponent and
// underscores between digits. A number too large for a float becomes
// infinity, as in Python.
func parsePyFloat(s string) (float64, bool) {
	body, ok := pyNumberText(s)
	if !ok {
		return 0, false
	}
	body = strings.Trim(body, asciiSpaces)
	sign := ""
	if body != "" && (body[0] == '+' || body[0] == '-') {
		sign, body = body[:1], body[1:]
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		if sign == "-" {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case "nan":
		return math.NaN(), true
	}
	mantissa, exponent := body, ""
	if i := strings.IndexAny(body, "eE"); i >= 0 {
		mantissa, exponent = body[:i], body[i+1:]
		if exponent != "" && (exponent[0] == '+' || exponent[0] == '-') {
			exponent = exponent[1:]
		}
		if _, ok := digitPart(exponent); !ok {
			return 0, false
		}
	}
	whole, fraction, hasPoint := strings.Cut(mantissa, ".")
	if whole == "" && fraction == "" {
		return 0, false
	}
	if whole != "" {
		if _, ok := digitPart(whole); !ok {
			return 0, false
		}
	}
	if hasPoint && fraction != "" {
		if _, ok := digitPart(fraction); !ok {
			return 0, false
		}
	}
	value, err := strconv.ParseFloat(sign+strings.ReplaceAll(body, "_", ""), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return value, true
}
