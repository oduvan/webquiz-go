package checker

import (
	"math"
	"math/big"
	"testing"

	"go.starlark.net/starlark"
)

func pyDict(t *testing.T, pairs ...starlark.Value) *starlark.Dict {
	t.Helper()
	d := starlark.NewDict(len(pairs) / 2)
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := d.SetKey(pairs[i], pairs[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// The expected texts come from Python 3.13: format(value, spec).
func TestFormatValueMatchesPython(t *testing.T) {
	i := func(n int64) starlark.Value { return starlark.MakeInt64(n) }
	f := func(x float64) starlark.Value { return starlark.Float(x) }
	s := func(x string) starlark.Value { return starlark.String(x) }
	big70 := starlark.MakeBigInt(new(big.Int).Lsh(big.NewInt(1), 70))
	cases := []struct {
		value starlark.Value
		spec  string
		want  string
	}{
		{f(3.14159), ".2f", "3.14"},
		{f(3.14159), "", "3.14159"},
		{f(2.675), ".2f", "2.67"},
		{f(0.125), ".2f", "0.12"},
		{i(1234567), ",", "1,234,567"},
		{f(1234567.891), ",.2f", "1,234,567.89"},
		{f(0.256), ".1%", "25.6%"},
		{f(0.5), "%", "50.000000%"},
		{i(42), ">8", "      42"},
		{i(42), "<8", "42      "},
		{i(42), "^8", "   42   "},
		{i(42), "*^9", "***42****"},
		{i(42), "08.3f", "0042.000"},
		{i(-42), "=+8", "-     42"},
		{i(42), "+", "+42"},
		{i(42), " ", " 42"},
		{i(-42), " ", "-42"},
		{i(255), "#x", "0xff"},
		{i(255), "#X", "0XFF"},
		{i(255), "#o", "0o377"},
		{i(5), "b", "101"},
		{i(5), "#b", "0b101"},
		{f(12345.678), "e", "1.234568e+04"},
		{f(12345.678), "E", "1.234568E+04"},
		{f(12345.678), "g", "12345.7"},
		{f(12345.678), ".3g", "1.23e+04"},
		{f(0.0001234), "g", "0.0001234"},
		{f(1e-5), "g", "1e-05"},
		{f(100000.0), "g", "100000"},
		{f(1e6), "g", "1e+06"},
		{f(1.0), ".2", "1.0"},
		{f(1234.5), ".2", "1.2e+03"},
		{f(0.5), ".2", "0.5"},
		{f(12.0), ".3", "12.0"},
		{f(123.0), ".3", "1.23e+02"},
		{f(1e16), "", "1e+16"},
		{f(1e15), "", "1000000000000000.0"},
		{f(123456789.123), "", "123456789.123"},
		{f(1e-4), "", "0.0001"},
		{f(1e-5), "", "1e-05"},
		{f(math.Inf(1)), "", "inf"},
		{f(math.Inf(-1)), ".2f", "-inf"},
		{f(math.NaN()), "F", "NAN"},
		{f(math.Inf(1)), "%", "inf%"},
		{f(math.Copysign(0, -1)), "", "-0.0"},
		{f(math.Copysign(0, -1)), "z.1f", "0.0"},
		{f(-0.001), "z.1f", "0.0"},
		{f(-0.001), ".1f", "-0.0"},
		{i(1234), "010,", "00,001,234"},
		{i(1234), "09,", "0,001,234"},
		{i(1234), "08,", "0,001,234"},
		{i(1234567), "_", "1_234_567"},
		{i(0xdeadbeef), "_x", "dead_beef"},
		{f(-1234.5), "015,.2f", "-000,001,234.50"},
		{i(5), "05", "00005"},
		{i(-5), "05", "-0005"},
		{i(5), ".2f", "5.00"},
		{big70, ",", "1,180,591,620,717,411,303,424"},
		{i(7), "c", "\a"},
		{i(97), "c", "a"},
		{starlark.True, "", "True"},
		{starlark.True, "d", "1"},
		{starlark.True, ">5", "    1"},
		{starlark.False, "05", "00000"},
		{s("ab"), ">4", "  ab"},
		{s("ab"), "<4", "ab  "},
		{s("ab"), "^5", " ab  "},
		{s("ab"), "05", "ab000"},
		{s("abcdef"), ".3", "abc"},
		{s("abcdef"), "*>8.2", "******ab"},
		{s("Київ"), "^8", "  Київ  "},
		{s("ab"), "s", "ab"},
		{s(""), "", ""},
		{starlark.None, "", "None"},
		{starlark.NewList([]starlark.Value{i(1), s("a")}), "", "[1, 'a']"},
		{starlark.Tuple{i(1)}, "", "(1,)"},
		{pyDict(t, s("a"), i(1)), "", "{'a': 1}"},
		{f(1.5), "#.0f", "2."},
		{f(1.5), ".0f", "2"},
		{f(2.5), ".0f", "2"},
		{f(1e100), ".2f", "10000000000000000159028911097599180468360808563945281389781327557747838772170381060813469985856815104.00"},
		{f(12.5), "#g", "12.5000"},
		{f(12.5), "#.3g", "12.5"},
		{f(0.0), "g", "0"},
		{f(0.0), ".0e", "0e+00"},
		{f(1.5), "#.0e", "2.e+00"},
		{f(123456.0), "n", "123456"},
		{i(1234), "n", "1234"},
		{f(3.0), "", "3.0"},
		{f(3.0), "g", "3"},
		{f(1e22), "", "1e+22"},
		{f(1.5e300), "", "1.5e+300"},
		{f(5e-324), "", "5e-324"},
		{f(1234.5678), "10.2f", "   1234.57"},
		{f(1234.5678), "<10.2f", "1234.57   "},
		{f(-1234.5678), "^12.1f", "  -1234.6   "},
		{f(0.000012345), ".3e", "1.234e-05"},
		{f(99.995), ".2f", "100.00"},
		{f(1.005), ".2f", "1.00"},
		{i(42), "#", "42"},
		{i(42), "=5", "   42"},
	}
	for _, c := range cases {
		got, err := formatValue(c.value, c.spec)
		if err != nil {
			t.Errorf("format(%s, %q) failed: %v", c.value, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("format(%s, %q) = %q, want %q", c.value, c.spec, got, c.want)
		}
	}
}

// The expected errors are Python 3.13's texts.
func TestFormatValueErrorsMatchPython(t *testing.T) {
	huge := starlark.MakeBigInt(new(big.Int).Lsh(big.NewInt(1), 1100))
	cases := []struct {
		value starlark.Value
		spec  string
		want  string
	}{
		{starlark.MakeInt(42), ".2", "Precision not allowed in integer format specifier"},
		{starlark.MakeInt(42), "s", "Unknown format code 's' for object of type 'int'"},
		{starlark.String("ab"), "d", "Unknown format code 'd' for object of type 'str'"},
		{starlark.String("ab"), "+", "Sign not allowed in string format specifier"},
		{starlark.String("ab"), "=", "'=' alignment not allowed in string format specifier"},
		{starlark.String("ab"), ",", "Cannot specify ',' with 's'."},
		{starlark.String("ab"), "#", "Alternate form (#) not allowed in string format specifier"},
		{starlark.Float(3.5), "d", "Unknown format code 'd' for object of type 'float'"},
		{starlark.Float(3.5), "x", "Unknown format code 'x' for object of type 'float'"},
		{starlark.None, ">5", "unsupported format string passed to NoneType.__format__"},
		{starlark.NewList(nil), "s", "unsupported format string passed to list.__format__"},
		{starlark.MakeInt(42), ",n", "Cannot specify ',' with 'n'."},
		{starlark.MakeInt(42), ",_", "Cannot specify both ',' and '_'."},
		{starlark.MakeInt(255), ",x", "Cannot specify ',' with 'x'."},
		{starlark.MakeInt(42), "z", "Negative zero coercion (z) not allowed in integer format specifier"},
		{starlark.Float(1.5), "abc", "Invalid format specifier 'abc' for object of type 'float'"},
		{starlark.MakeInt(42), ".f", "Format specifier missing precision"},
		{starlark.MakeInt(-1), "c", "%c arg not in range(0x110000)"},
		{starlark.MakeInt(0x110000), "c", "%c arg not in range(0x110000)"},
		{starlark.MakeInt(65), "+c", "Sign not allowed with integer format specifier 'c'"},
		{huge, ".2f", "int too large to convert to float"},
	}
	for _, c := range cases {
		got, err := formatValue(c.value, c.spec)
		if err == nil {
			t.Errorf("format(%s, %q) = %q, want error %q", c.value, c.spec, got, c.want)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("format(%s, %q) error = %q, want %q", c.value, c.spec, err, c.want)
		}
	}
}

// The expected texts come from Python 3.13: str(value) and repr(value).
func TestPythonStrAndRepr(t *testing.T) {
	// Variables, so that Go does not add the constants exactly.
	pointOne, pointTwo := 0.1, 0.2
	list := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.MakeInt(1), starlark.Float(2.5), starlark.None, starlark.True})
	set := starlark.NewSet(2)
	_ = set.Insert(starlark.MakeInt(1))
	_ = set.Insert(starlark.MakeInt(2))
	cyclic := starlark.NewList(nil)
	_ = cyclic.Append(cyclic)
	cases := []struct {
		value     starlark.Value
		str, repr string
	}{
		{starlark.String("a'b"), "a'b", `"a'b"`},
		{starlark.String(`a"b`), `a"b`, `'a"b'`},
		{starlark.String("é\n\t\x00\u200b"), "é\n\t\x00\u200b", `'é\n\t\x00\u200b'`},
		{starlark.Float(pointOne + pointTwo), "0.30000000000000004", "0.30000000000000004"},
		{starlark.Float(1e15), "1000000000000000.0", "1000000000000000.0"},
		{starlark.Float(1e16), "1e+16", "1e+16"},
		{starlark.Float(1e-5), "1e-05", "1e-05"},
		{starlark.Float(123456789.123), "123456789.123", "123456789.123"},
		{starlark.Float(math.Inf(1)), "inf", "inf"},
		{starlark.Float(math.NaN()), "nan", "nan"},
		{starlark.MakeInt(-7), "-7", "-7"},
		{list, "['a', 1, 2.5, None, True]", "['a', 1, 2.5, None, True]"},
		{starlark.Tuple{starlark.String("a")}, "('a',)", "('a',)"},
		{starlark.Tuple{}, "()", "()"},
		{pyDict(t, starlark.String("a"), starlark.MakeInt(1)), "{'a': 1}", "{'a': 1}"},
		{set, "{1, 2}", "{1, 2}"},
		{starlark.NewSet(0), "set()", "set()"},
		{cyclic, "[[...]]", "[[...]]"},
	}
	for _, c := range cases {
		if got := pyStr(c.value); got != c.str {
			t.Errorf("str(%s) = %q, want %q", c.value, got, c.str)
		}
		if got := pyRepr(c.value); got != c.repr {
			t.Errorf("repr(%s) = %q, want %q", c.value, got, c.repr)
		}
	}
}
