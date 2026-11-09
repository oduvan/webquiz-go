package quiz

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/oduvan/webquiz-go/internal/explanations"
	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/timelimits"
)

// DefaultTitle is the title of a quiz whose file has none.
const DefaultTitle = "Система Тестування"

// Question is one question as written in the quiz file, plus its "id".
// Values come from pyval.LoadYAML, so use the pyval functions to read keys
// that the methods below do not cover.
type Question map[string]any

// ID is the number of the question in its quiz (1 for the first question).
func (q Question) ID() int {
	id, _ := pyval.Int(q["id"])
	return int(id)
}

// IsText tells whether this is a text input question: it has a "checker" key.
func (q Question) IsText() bool {
	_, ok := q["checker"]
	return ok
}

// IsMultipleChoice tells whether correct_answer is a list.
func (q Question) IsMultipleChoice() bool {
	return pyval.IsList(q["correct_answer"])
}

// Points is the question's points, rounded like normalize_points; 1 when not set.
func (q Question) Points() float64 {
	value, ok := q["points"]
	if !ok {
		return 1
	}
	points, ok := pyval.ToFloat(value)
	if !ok || math.IsNaN(points) || math.IsInf(points, 0) {
		return 1 // Load rejects such quizzes; this only guards hand-made questions
	}
	return NormalizePoints(points)
}

// Sticky tells whether the question stays right after the one before it when
// questions are shuffled (stick_to_the_previous, read with Python's truth rules).
func (q Question) Sticky() bool {
	return pyval.Truthy(q["stick_to_the_previous"])
}

// File is the download address of the question's attachment ("/attach/name"),
// or "" when it has none.
func (q Question) File() string {
	file, ok := q["file"].(string)
	if !ok || file == "" {
		return ""
	}
	if !strings.HasPrefix(file, "/attach/") {
		file = "/attach/" + file
	}
	return file
}

// Quiz is a loaded quiz file.
type Quiz struct {
	// Data is the whole file as read; the question maps in it carry their "id".
	Data      map[string]any
	Questions []Question
	Title     string

	// The quiz settings, read with Python's truth rules and these defaults:
	// show_right_answer true, randomize_questions false,
	// show_answers_on_completion false, show_final_list true,
	// show_explanation false.
	ShowRightAnswer         bool
	RandomizeQuestions      bool
	ShowAnswersOnCompletion bool
	ShowFinalList           bool
	ShowExplanation         bool

	// TimeLimit is the quiz-level limit. TimeLimitWarning is not empty when
	// the file had a broken time_limit (the quiz then runs without limits).
	TimeLimit          timelimits.Settings
	TimeLimitWarning   string
	QuestionTimeLimits map[int]int // question id -> seconds, 0 = no limit

	// ExplanationsHTML holds the rendered explanation of every question that
	// has one (question id -> HTML for the quiz page).
	ExplanationsHTML map[int]string
}

// QuestionByID finds a question by its id. The id may come from a JSON body:
// it is compared with Python's ==, so 2, 2.0 and json.Number("2") find
// question 2, and "2" finds nothing.
func (q *Quiz) QuestionByID(id any) (Question, bool) {
	for _, question := range q.Questions {
		if pyval.Equal(question["id"], id) {
			return question, true
		}
	}
	return nil, false
}

// Load reads a quiz file. See Parse.
func Load(path string) (*Quiz, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	quiz, err := Parse(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return quiz, nil
}

// Parse reads quiz file content. It does not run the save checks (Validate);
// like the Python version it fails only where a quiz cannot be started.
func Parse(content []byte) (*Quiz, error) {
	value, err := pyval.LoadYAML(content)
	if err != nil {
		return nil, err
	}
	data, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("the quiz file must be a mapping with a 'questions' list")
	}
	rawQuestions, ok := data["questions"]
	if !ok {
		return nil, errors.New("the quiz file has no 'questions'")
	}
	items, ok := rawQuestions.([]any)
	if !ok {
		return nil, errors.New("'questions' must be a list")
	}
	questions := make([]Question, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Question %d must be a mapping", i+1)
		}
		m["id"] = i + 1
		question := Question(m)
		if err := checkStartable(question, i+1); err != nil {
			return nil, err
		}
		questions[i] = question
	}

	title := DefaultTitle
	if value, ok := data["title"]; ok {
		text, isText := value.(string)
		if !isText {
			return nil, errors.New("'title' must be a string")
		}
		title = text
	}

	quiz := &Quiz{
		Data:                    data,
		Questions:               questions,
		Title:                   title,
		ShowRightAnswer:         setting(data, "show_right_answer", true),
		RandomizeQuestions:      setting(data, "randomize_questions", false),
		ShowAnswersOnCompletion: setting(data, "show_answers_on_completion", false),
		ShowFinalList:           setting(data, "show_final_list", true),
		ShowExplanation:         setting(data, "show_explanation", false),
		ExplanationsHTML:        map[int]string{},
	}
	quiz.TimeLimit, quiz.TimeLimitWarning = timelimits.Parse(data)
	quiz.QuestionTimeLimits = timelimits.QuestionLimits(quiz.TimeLimit, questions)
	for _, question := range questions {
		if explanations.Has(question) {
			quiz.ExplanationsHTML[question.ID()] = explanations.Render(question["explanation"].(string), explanations.StudentImagesURL)
		}
	}
	return quiz, nil
}

// setting reads a quiz setting like data.get(key, fallback) used in an "if".
func setting(data map[string]any, key string, fallback bool) bool {
	value, ok := data[key]
	if !ok {
		return fallback
	}
	return pyval.Truthy(value)
}

// checkStartable reports what made starting the quiz fail in Python: the
// quiz page was built from options, correct_answer, points and file.
func checkStartable(q Question, number int) error {
	if !q.IsText() {
		for _, key := range []string{"options", "correct_answer"} {
			if _, ok := q[key]; !ok {
				return fmt.Errorf("Question %d missing required field: %s", number, key)
			}
		}
	}
	if value, ok := q["points"]; ok {
		points, isNumber := pyval.ToFloat(value)
		if !isNumber || math.IsNaN(points) || math.IsInf(points, 0) {
			return fmt.Errorf("Question %d points must be a number, got %s", number, pyval.Repr(value))
		}
	}
	if value, ok := q["file"]; ok && pyval.Truthy(value) && !pyval.IsStr(value) {
		return fmt.Errorf("Question %d file must be a string", number)
	}
	return nil
}
