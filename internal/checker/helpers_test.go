package checker

import (
	"strings"
	"testing"
)

// The helpers to_int, distance and direction_angle come from
// webquiz/checker.py. These tests port tests/test_checker.py one to one.

type helperCase struct {
	py      string
	inputs  []string
	want    []int
	wantErr string // a part of the error text; "" means any error
	isError bool
}

func runHelperCases(t *testing.T, name string, fn func(string) (int, error), cases []helperCase) {
	t.Helper()
	for _, c := range cases {
		for i, input := range c.inputs {
			got, err := fn(input)
			if c.isError {
				if err == nil {
					t.Errorf("%s: %s(%q) = %d, want an error", c.py, name, input, got)
				} else if !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("%s: %s(%q) error = %q, want it to contain %q", c.py, name, input, err, c.wantErr)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: %s(%q) failed: %v", c.py, name, input, err)
				continue
			}
			if got != c.want[i] {
				t.Errorf("%s: %s(%q) = %d, want %d", c.py, name, input, got, c.want[i])
			}
		}
	}
}

func TestToInt(t *testing.T) {
	runHelperCases(t, "ToInt", ToInt, []helperCase{
		{py: "tests/test_checker.py::TestToInt::test_simple_integer", inputs: []string{"42"}, want: []int{42}},
		{py: "tests/test_checker.py::TestToInt::test_integer_with_whitespace", inputs: []string{"  42  ", "\t42\n"}, want: []int{42, 42}},
		{py: "tests/test_checker.py::TestToInt::test_negative_integer", inputs: []string{"-10", "  -5  "}, want: []int{-10, -5}},
		{py: "tests/test_checker.py::TestToInt::test_zero", inputs: []string{"0", "  0  "}, want: []int{0, 0}},
		{py: "tests/test_checker.py::TestToInt::test_invalid_input_float", inputs: []string{"3.14"}, isError: true},
		{py: "tests/test_checker.py::TestToInt::test_invalid_input_text", inputs: []string{"abc"}, isError: true},
		{py: "tests/test_checker.py::TestToInt::test_empty_string", inputs: []string{""}, isError: true},
		{py: "tests/test_checker.py::TestToInt::test_whitespace_only", inputs: []string{"   "}, isError: true},
	})
}

func TestDistance(t *testing.T) {
	runHelperCases(t, "Distance", Distance, []helperCase{
		{py: "tests/test_checker.py::TestDistance::test_plain_number", inputs: []string{"2000"}, want: []int{2000}},
		{py: "tests/test_checker.py::TestDistance::test_number_with_whitespace", inputs: []string{"  2000  "}, want: []int{2000}},
		{py: "tests/test_checker.py::TestDistance::test_meters_latin", inputs: []string{"2000m", "500m"}, want: []int{2000, 500}},
		{py: "tests/test_checker.py::TestDistance::test_meters_cyrillic", inputs: []string{"2000м", "500м"}, want: []int{2000, 500}},
		{py: "tests/test_checker.py::TestDistance::test_kilometers_latin", inputs: []string{"2km", "5km"}, want: []int{2000, 5000}},
		{py: "tests/test_checker.py::TestDistance::test_kilometers_cyrillic", inputs: []string{"2км", "5км"}, want: []int{2000, 5000}},
		{py: "tests/test_checker.py::TestDistance::test_kilometers_float", inputs: []string{"0.5km", "1.5км", "2.5km"}, want: []int{500, 1500, 2500}},
		{py: "tests/test_checker.py::TestDistance::test_trailing_dot", inputs: []string{"2км.", "500м."}, want: []int{2000, 500}},
		{py: "tests/test_checker.py::TestDistance::test_case_insensitive", inputs: []string{"2KM", "500M"}, want: []int{2000, 500}},
		{py: "tests/test_checker.py::TestDistance::test_float_meters", inputs: []string{"1500.0m", "2500.5"}, want: []int{1500, 2500}},
		{py: "tests/test_checker.py::TestDistance::test_invalid_format", inputs: []string{"abc"}, isError: true, wantErr: "Неверный формат"},
		{py: "tests/test_checker.py::TestDistance::test_empty_string", inputs: []string{""}, isError: true},
		{py: "tests/test_checker.py::TestDistance::test_mixed_invalid", inputs: []string{"10xyz"}, isError: true},
	})
}

func TestDirectionAngle(t *testing.T) {
	runHelperCases(t, "DirectionAngle", DirectionAngle, []helperCase{
		{py: "tests/test_checker.py::TestDirectionAngle::test_simple_angle", inputs: []string{"20", "15"}, want: []int{2000, 1500}},
		{py: "tests/test_checker.py::TestDirectionAngle::test_angle_with_whitespace", inputs: []string{"  20  "}, want: []int{2000}},
		{py: "tests/test_checker.py::TestDirectionAngle::test_dash_format_whole", inputs: []string{"20-00", "15-30", "0-45"}, want: []int{2000, 1530, 45}},
		{py: "tests/test_checker.py::TestDirectionAngle::test_dash_format_with_whitespace", inputs: []string{"20 - 00", " 15 - 30 "}, want: []int{2000, 1530}},
		{py: "tests/test_checker.py::TestDirectionAngle::test_single_digit_degrees", inputs: []string{"5-00", "1-45"}, want: []int{500, 145}},
		{py: "tests/test_checker.py::TestDirectionAngle::test_two_digit_minutes", inputs: []string{"10-15", "35-59"}, want: []int{1015, 3559}},
		{py: "tests/test_checker.py::TestDirectionAngle::test_zero_angle", inputs: []string{"0", "0-00"}, want: []int{0, 0}},
		{py: "tests/test_checker.py::TestDirectionAngle::test_too_many_dashes", inputs: []string{"20-30-40"}, isError: true, wantErr: "Неверный формат"},
		{py: "tests/test_checker.py::TestDirectionAngle::test_invalid_number", inputs: []string{"abc"}, isError: true},
		{py: "tests/test_checker.py::TestDirectionAngle::test_empty_string", inputs: []string{""}, isError: true},
	})
}

// TestHelperErrorTexts checks that the error texts are the same as in
// Python 1.19, because students see them as the checker error.
func TestHelperErrorTexts(t *testing.T) {
	const distanceError = "Неверный формат ответа. Ожидается число, например: '2000', '2000м', '2км'."
	const angleError = "Неверный формат ответа. Ожидается число в градусах, например: '2000' или '20-00'."
	cases := []struct {
		name  string
		fn    func(string) (int, error)
		input string
		want  string
	}{
		{"ToInt", ToInt, "abc", "invalid literal for int() with base 10: 'abc'"},
		{"ToInt", ToInt, "  3.14 ", "invalid literal for int() with base 10: '3.14'"},
		{"ToInt", ToInt, "   ", "invalid literal for int() with base 10: ''"},
		{"ToInt", ToInt, "it's", `invalid literal for int() with base 10: "it's"`},
		{"Distance", Distance, "abc", distanceError},
		{"Distance", Distance, "", distanceError},
		{"Distance", Distance, "inf", distanceError},
		{"DirectionAngle", DirectionAngle, "20-30-40", angleError},
		{"DirectionAngle", DirectionAngle, "abc", "invalid literal for int() with base 10: 'abc'"},
		{"DirectionAngle", DirectionAngle, "-5", "invalid literal for int() with base 10: ''"},
	}
	for _, c := range cases {
		_, err := c.fn(c.input)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s(%q) error = %v, want %q", c.name, c.input, err, c.want)
		}
	}
}

// TestHelperPythonDetails checks inputs where Python's int() and float()
// rules matter: signs, underscores and float rounding.
func TestHelperPythonDetails(t *testing.T) {
	cases := []struct {
		name  string
		fn    func(string) (int, error)
		input string
		want  int
	}{
		{"ToInt", ToInt, "+5", 5},
		{"ToInt", ToInt, "1_000", 1000},
		{"Distance", Distance, "2.3km", 2300},
		{"Distance", Distance, "1e3m", 1000},
		{"Distance", Distance, "-1.5km", -1500},
		{"DirectionAngle", DirectionAngle, "20-+5", 2005},
	}
	for _, c := range cases {
		got, err := c.fn(c.input)
		if err != nil || got != c.want {
			t.Errorf("%s(%q) = %d, %v; want %d", c.name, c.input, got, err, c.want)
		}
	}
}
