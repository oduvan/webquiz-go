package quiz

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// RunChecker runs checker code (Python-like, see internal/checker) for an
// answer: correct is true when the code finishes without an error, and
// errorMessage is the error text otherwise. The server sets it to
// internal/checker once at start. While it is nil, every answer to a text
// question with checker code is wrong.
var RunChecker func(code, answer string) (correct bool, errorMessage string)

// checkerNotAvailable is the error text while RunChecker is not set.
const checkerNotAvailable = "the checker is not available"

// CheckAnswer tells whether an answer to a question is correct, like
// _validate_answer:
//
//   - a single choice question (correct_answer is a number) needs the same
//     number (compared with Python's ==, so 1, 1.0 and true are equal);
//   - a multiple choice question (correct_answer is a list) needs a list of
//     valid option indexes without wrong ones, with at least min_correct
//     right ones (all of them when min_correct is not set);
//   - a text question (it has "checker") runs the checker code with
//     RunChecker; without code, the answer must equal correct_value, apart
//     from spaces around it. checkerError is the checker's message.
//
// Where Python raised an error (a list inside the answer list, a
// min_correct that is not a number), the answer is wrong.
func CheckAnswer(q Question, answer any) (correct bool, checkerError string) {
	if q.IsText() {
		return checkText(q, answer)
	}
	expected := q["correct_answer"]
	if pyval.IsInt(expected) {
		return pyval.Equal(answer, expected), ""
	}
	expectedList, ok := pyval.List(expected)
	if !ok {
		return false, ""
	}
	selectedList, ok := pyval.List(answer)
	if !ok {
		return false, ""
	}
	selected, ok := newPySet(selectedList)
	if !ok {
		return false, ""
	}
	correctSet, ok := newPySet(expectedList)
	if !ok {
		return false, ""
	}
	optionCount, ok := pyval.Len(q["options"])
	if !ok {
		return false, ""
	}
	// Every selected value must be one of the option indexes 0 .. count-1.
	for _, key := range selected.keys {
		if !isIndexKey(key, optionCount) {
			return false, ""
		}
	}
	// Any wrong option makes the answer wrong.
	for _, key := range selected.keys {
		if !correctSet.has(key) {
			return false, ""
		}
	}
	minCorrect, hasMin := q["min_correct"]
	if !hasMin {
		minCorrect = len(expectedList)
	}
	result, comparable := pyval.Compare(selected.len(), minCorrect)
	return comparable && result >= 0, ""
}

// checkText is _execute_checker.
func checkText(q Question, answer any) (bool, string) {
	text := pyval.Str(answer) // the quiz page always sends text
	code := q["checker"]
	if !pyval.Truthy(code) {
		correctValue, ok := q["correct_value"]
		if !ok {
			correctValue = ""
		}
		return pyval.Strip(text) == pyval.Strip(pyval.Str(correctValue)), ""
	}
	codeText, ok := code.(string)
	if !ok {
		return false, "exec() arg 1 must be a string, bytes or code object"
	}
	if RunChecker == nil {
		return false, checkerNotAvailable
	}
	return RunChecker(codeText, text)
}

// FormatAnswer writes an answer for the CSV files, like _format_answer_text:
// a text answer as it is, an option index as the option text, a list of
// indexes as the option texts in index order joined with "|". An index
// without an option becomes "Invalid index: N". Other values are written
// with Python's str().
func FormatAnswer(answer any, options any, isText bool) string {
	if isText || pyval.IsStr(answer) {
		return pyval.Str(answer)
	}
	optionList, _ := pyval.List(options)
	if pyval.IsInt(answer) {
		return optionText(answer, optionList)
	}
	items, ok := pyval.List(answer)
	if !ok {
		return pyval.Str(answer)
	}
	sorted := slices.Clone(items)
	if allNumbers(sorted) {
		slices.SortStableFunc(sorted, func(a, b any) int {
			result, _ := pyval.Compare(a, b)
			return result
		})
	}
	texts := make([]string, len(sorted))
	for i, item := range sorted {
		texts[i] = optionText(item, optionList)
	}
	return strings.Join(texts, "|")
}

// optionText gives the option an index points to, or "Invalid index: N".
func optionText(index any, options []any) string {
	if pyval.IsInt(index) {
		if i, ok := pyval.Int(index); ok && i >= 0 && i < int64(len(options)) {
			return pyval.Str(options[i])
		}
	}
	return "Invalid index: " + pyval.Str(index)
}

func allNumbers(items []any) bool {
	for _, item := range items {
		if !pyval.IsNumber(item) {
			return false
		}
		if f, ok := pyval.Number(item); ok && math.IsNaN(f) {
			return false
		}
	}
	return true
}

// pySet is a Python set of hashable values: equal numbers (1, 1.0, True) are
// one member, as in Python.
type pySet struct {
	keys    []string // in the order first seen
	members map[string]bool
}

// newPySet builds a set; ok is false when a value cannot be in a Python set
// (a list or a dict).
func newPySet(items []any) (pySet, bool) {
	set := pySet{members: map[string]bool{}}
	for i, item := range items {
		key, ok := setKey(item, i)
		if !ok {
			return pySet{}, false
		}
		if !set.members[key] {
			set.members[key] = true
			set.keys = append(set.keys, key)
		}
	}
	return set, true
}

func (s pySet) has(key string) bool { return s.members[key] }

func (s pySet) len() int { return len(s.keys) }

// setKey gives the key under which Python would find a value in a set.
// position makes every NaN a member of its own, as in Python.
func setKey(v any, position int) (string, bool) {
	switch pyval.KindOf(v) {
	case pyval.KindNone:
		return "none", true
	case pyval.KindStr:
		return "s:" + v.(string), true
	case pyval.KindList, pyval.KindDict:
		return "", false
	case pyval.KindBool, pyval.KindInt, pyval.KindFloat:
		f, _ := pyval.Number(v)
		if math.IsNaN(f) {
			return "nan:" + strconv.Itoa(position), true
		}
		if pyval.IsInt(v) {
			i, ok := pyval.Int(v)
			if ok {
				return "n:" + strconv.FormatInt(i, 10), true
			}
			return "n:" + pyval.Str(v), true // a huge int
		}
		if !math.IsInf(f, 0) && f == math.Trunc(f) {
			whole, _ := new(big.Float).SetFloat64(f).Int(nil)
			return "n:" + whole.String(), true // 2.0 is the same member as 2
		}
		return "f:" + pyval.FloatRepr(f), true
	}
	return "o:" + pyval.Str(v), true
}

// isIndexKey tells whether a set key is a whole number from 0 to count-1.
func isIndexKey(key string, count int) bool {
	number, ok := strings.CutPrefix(key, "n:")
	if !ok {
		return false
	}
	i, err := strconv.Atoi(number)
	return err == nil && i >= 0 && i < count
}
