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
		value, ok = parsePyFloat(s[:len(s)-2])
		value *= 1000
	case strings.HasSuffix(s, "m"):
		value, ok = parsePyFloat(s[:len(s)-1])
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
	return 0, fmt.Errorf("number is too large: %s", pyReprString(s))
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

// pyInt parses s like Python's int(s) with base 10 and returns Python's
// error when it cannot.
func pyInt(s string) (*big.Int, error) {
	if n, ok := parsePyInt(s); ok {
		return n, nil
	}
	return nil, fmt.Errorf("invalid literal for int() with base 10: %s", pyReprString(s))
}

// parsePyInt reads a base 10 integer the way Python's int() reads a string:
// whitespace around it, an optional sign, and digits that may have single
// underscores between them.
func parsePyInt(s string) (*big.Int, bool) {
	body := pyStrip(s)
	sign := ""
	if body != "" && (body[0] == '+' || body[0] == '-') {
		sign, body = body[:1], body[1:]
	}
	digits, ok := digitPart(body)
	if !ok || len(digits) != len(body)-strings.Count(body, "_") {
		return nil, false
	}
	n, ok := new(big.Int).SetString(sign+digits, 10)
	return n, ok
}

// digitPart checks that s is digits with single underscores between them
// and returns the digits without the underscores.
func digitPart(s string) (string, bool) {
	if s == "" || s[0] == '_' || s[len(s)-1] == '_' || strings.Contains(s, "__") {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if (s[i] < '0' || s[i] > '9') && s[i] != '_' {
			return "", false
		}
	}
	return strings.ReplaceAll(s, "_", ""), true
}

// parsePyFloat reads a number the way Python's float() reads a string:
// whitespace around it, an optional sign, "inf", "infinity" and "nan" in
// any case, decimal numbers with an exponent and underscores between digits.
// A number too large for a float becomes infinity, as in Python.
func parsePyFloat(s string) (float64, bool) {
	body := pyStrip(s)
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
