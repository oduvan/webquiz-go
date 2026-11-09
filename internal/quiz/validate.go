package quiz

import (
	"fmt"

	"github.com/oduvan/webquiz-go/internal/explanations"
	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/timelimits"
)

// ValidateChecker checks that checker code compiles. ok is false when it does
// not; msg and line say why. The server sets it to internal/checker once at
// start. When it is nil, Validate does not check checker code.
var ValidateChecker func(code string) (msg string, line int, ok bool)

// The quiz settings that must be true or false, in the order Python checks them.
var booleanSettings = []string{"show_right_answer", "randomize_questions", "show_answers_on_completion", "show_final_list"}

// Validate checks quiz data before it is saved, like _validate_quiz_data. It
// returns error messages (none when the quiz is valid). data is what the quiz
// file holds (pyval.LoadYAML) or what a JSON body holds (decoded with
// UseNumber). Unlike Python, a non-list options value never stops the checks
// with a crash: its error is reported like the others.
func Validate(data any) []string {
	m, ok := pyval.Dict(data)
	if !ok {
		return []string{"Дані квізу повинні бути словником"}
	}
	rawQuestions, ok := m["questions"]
	if !ok {
		return []string{"Квіз повинен містити поле 'questions'"}
	}
	questions, ok := pyval.List(rawQuestions)
	if !ok {
		return []string{"'questions' повинно бути списком"}
	}
	if len(questions) == 0 {
		return []string{"Квіз повинен містити принаймні одне питання"}
	}

	var errors []string
	add := func(format string, args ...any) {
		errors = append(errors, fmt.Sprintf(format, args...))
	}
	for i, item := range questions {
		n := i + 1
		q, ok := pyval.Dict(item)
		if !ok {
			add("Question %d must be a dictionary", n)
			continue
		}
		_, isText := q["checker"]

		// Either question text or an image must be given, for every question type.
		if !pyval.Truthy(q["question"]) && !pyval.Truthy(q["image"]) {
			add("Question %d must have either question text or image", n)
		}

		if isText {
			validateTextQuestion(q, n, add)
		} else {
			validateChoiceQuestion(q, n, add)
		}

		if points, ok := q["points"]; ok {
			validatePoints(points, n, add)
		}

		if stick, ok := q["stick_to_the_previous"]; ok {
			if !pyval.IsBool(stick) {
				add("Question %d 'stick_to_the_previous' must be a boolean (true or false)", n)
			} else if n == 1 && stick.(bool) {
				add("Question 1 cannot have 'stick_to_the_previous: true' (no previous question to stick to)")
			}
		}
	}

	for _, key := range booleanSettings {
		if value, ok := m[key]; ok && !pyval.IsBool(value) {
			add("'%s' must be a boolean (true or false)", key)
		}
	}
	errors = append(errors, explanations.Validate(m)...)
	if title, ok := m["title"]; ok && !pyval.IsStr(title) {
		add("'title' must be a string")
	}
	errors = append(errors, timelimits.Validate(m)...)
	return errors
}

func validateTextQuestion(q map[string]any, n int, add func(string, ...any)) {
	for _, key := range []string{"default_value", "correct_value", "checker"} {
		if value, ok := q[key]; ok && !pyval.IsStr(value) {
			add("Question %d %s must be a string", n, key)
		}
	}
	code, isString := q["checker"].(string)
	if !isString || code == "" || ValidateChecker == nil {
		return
	}
	if msg, line, ok := ValidateChecker(code); !ok {
		add("Question %d checker has invalid Python syntax: %s (line %d)", n, msg, line)
	}
}

func validateChoiceQuestion(q map[string]any, n int, add func(string, ...any)) {
	for _, key := range []string{"options", "correct_answer"} {
		if _, ok := q[key]; !ok {
			add("Question %d missing required field: %s", n, key)
		}
	}
	options, hasOptions := q["options"]
	if hasOptions {
		if items, isList := pyval.List(options); !isList {
			add("Question %d options must be a list", n)
		} else if len(items) < 2 {
			add("Question %d must have at least 2 options", n)
		} else {
			for _, option := range items {
				if !pyval.IsStr(option) {
					add("Question %d all options must be strings", n)
					break
				}
			}
		}
	}

	correct, hasCorrect := q["correct_answer"]
	if hasCorrect && hasOptions {
		// len(options): text and dicts have a length too. Python crashed for
		// other values; here the "options must be a list" error is enough.
		if count, ok := pyval.Len(options); ok {
			validateCorrectAnswer(correct, count, n, add)
		}
	}

	if minCorrect, ok := q["min_correct"]; ok {
		correctList, isList := pyval.List(correct)
		switch {
		case !hasCorrect:
			add("Question %d has min_correct but no correct_answer", n)
		case !isList:
			add("Question %d min_correct is only valid for multiple answer questions", n)
		case !pyval.IsInt(minCorrect):
			add("Question %d min_correct must be an integer", n)
		case lessThan(minCorrect, 1):
			add("Question %d min_correct must be at least 1", n)
		case greaterThan(minCorrect, len(correctList)):
			add("Question %d min_correct cannot exceed number of correct answers", n)
		}
	}
}

func validateCorrectAnswer(correct any, optionsCount int, n int, add func(string, ...any)) {
	if pyval.IsInt(correct) { // True and False count, as in Python
		if lessThan(correct, 0) || !lessThan(correct, optionsCount) {
			add("Question %d correct_answer index out of range", n)
		}
		return
	}
	items, isList := pyval.List(correct)
	if !isList {
		add("Question %d correct_answer must be an integer or array of integers", n)
		return
	}
	if len(items) == 0 {
		add("Question %d correct_answer array cannot be empty", n)
		return
	}
	for _, item := range items {
		if !pyval.IsInt(item) {
			add("Question %d correct_answer array must contain only integers", n)
			return
		}
	}
	for _, item := range items {
		if lessThan(item, 0) || !lessThan(item, optionsCount) {
			add("Question %d correct_answer array contains index out of range", n)
			return
		}
	}
	if set, ok := newPySet(items); !ok || set.len() != len(items) {
		add("Question %d correct_answer array contains duplicate indices", n)
	}
}

func validatePoints(points any, n int, add func(string, ...any)) {
	if pyval.IsBool(points) || !pyval.IsNumber(points) {
		add("Question %d points must be a positive number", n)
		return
	}
	// "points <= 0" is false for NaN in Python; NaN then fails the decimals check.
	if result, ok := pyval.Compare(points, 0); ok && result <= 0 {
		add("Question %d points must be greater than 0", n)
		return
	}
	if pyval.IsIntNotBool(points) {
		return // round(int, 2) gives the same int
	}
	value, _ := pyval.Number(points)
	if pyval.Round(value, PointsDecimalPlaces) != value {
		add("Question %d points can have at most %d decimal places", n, PointsDecimalPlaces)
	}
}

// lessThan is Python's a < b for numbers; false when they cannot be compared.
func lessThan(a any, b int) bool {
	result, ok := pyval.Compare(a, b)
	return ok && result < 0
}

// greaterThan is Python's a > b for numbers; false when they cannot be compared.
func greaterThan(a any, b int) bool {
	result, ok := pyval.Compare(a, b)
	return ok && result > 0
}
