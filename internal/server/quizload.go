package server

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oduvan/webquiz-go/internal/checker"
	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
	"github.com/oduvan/webquiz-go/internal/timelimits"
)

// quizFile is a quiz file in quizzes_dir with its title (nil when the file
// has none or cannot be read), like an entry of list_available_quizzes.
type quizFile struct {
	Filename string `json:"filename"`
	Title    any    `json:"title"`
}

// listAvailableQuizzes is list_available_quizzes: every .yaml/.yml entry of
// quizzes_dir with its title, sorted by name.
func (s *Server) listAvailableQuizzes() []quizFile {
	entries, err := os.ReadDir(s.quizzesDir)
	if err != nil {
		return []quizFile{}
	}
	files := []quizFile{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		files = append(files, quizFile{Filename: name, Title: readQuizTitle(filepath.Join(s.quizzesDir, name))})
	}
	slices.SortFunc(files, func(a, b quizFile) int { return strings.Compare(a.Filename, b.Filename) })
	return files
}

// readQuizTitle returns the "title" of a quiz file as written, or nil.
func readQuizTitle(path string) any {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	data, err := pyval.LoadYAML(content)
	if err != nil {
		return nil
	}
	if m, ok := data.(map[string]any); ok {
		return m["title"]
	}
	return nil
}

// defaultQuizYAML is the quiz the server writes when quizzes_dir has no
// quiz: what create_default_config_yaml wrote with yaml.dump(...,
// default_flow_style=False, allow_unicode=True).
const defaultQuizYAML = `questions:
- correct_answer: 1
  options:
  - '3'
  - '4'
  - '5'
  - '6'
  question: Скільки буде 2 + 2?
- correct_answer: 2
  options:
  - Харків
  - Львів
  - Київ
  - Одеса
  question: Яка столиця України?
show_right_answer: true
title: Тестовий Quiz
`

// loadQuestions is load_questions, run once at start: no quiz files gives a
// new default.yaml, one file is started, several files start default.yaml
// (or default.yml); otherwise the quiz page asks the admin to choose.
func (s *Server) loadQuestions() error {
	if _, err := os.Stat(s.quizzesDir); os.IsNotExist(err) {
		if err := os.MkdirAll(s.quizzesDir, 0o755); err != nil {
			return err
		}
		s.log.Info(fmt.Sprintf("Created quizzes directory: %s", s.quizzesDir))
	}
	files := s.listAvailableQuizzes()
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Filename
	}
	switch {
	case len(files) == 0:
		path := filepath.Join(s.quizzesDir, "default.yaml")
		if err := os.WriteFile(path, []byte(defaultQuizYAML), 0o644); err != nil {
			return err
		}
		s.log.Info(fmt.Sprintf("Created default config file: %s", path))
		return s.SwitchQuiz("default.yaml", nil)
	case len(files) == 1:
		if err := s.SwitchQuiz(names[0], nil); err != nil {
			return err
		}
		s.log.Info(fmt.Sprintf("Using single quiz file as default: %s", names[0]))
	case slices.Contains(names, "default.yaml") || slices.Contains(names, "default.yml"):
		name := "default.yaml"
		if !slices.Contains(names, name) {
			name = "default.yml"
		}
		if err := s.SwitchQuiz(name, nil); err != nil {
			return err
		}
		s.log.Info(fmt.Sprintf("Using explicit default file: %s", name))
	default:
		s.log.Info("Multiple quiz files found but no default.yaml - admin must select quiz first")
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.writeSelectionPageLocked(files)
	}
	return nil
}

// SwitchQuiz is switch_quiz: it starts the quiz file filename (a name in
// quizzes_dir) as a new run. Every student of the previous run is
// forgotten, new CSV files are chosen, the quiz page (static_dir/index.html)
// is written again and the live stats pages hear "quiz_switched".
//
// settings are the admin's start settings (already checked): they override
// the quiz file for this run and are never written to the file. With nil
// settings a restart of the same file keeps its earlier start settings, and
// another file runs with its own values.
//
// Unlike the Python version, a quiz file that cannot be loaded changes
// nothing: the old quiz keeps running and the error is returned. Like the
// Python version, answers that still wait for the 5-second CSV flush are
// dropped with the old run.
func (s *Server) SwitchQuiz(filename string, settings map[string]any) error {
	_, err := s.switchQuiz(filename, settings)
	return err
}

// switchResult is what the admin hears about a new run.
type switchResult struct {
	csvFile  string         // the answers CSV of the run
	settings map[string]any // current_quiz_settings()
}

// switchQuiz does SwitchQuiz and also returns the new run's CSV file and
// settings, read under the same lock.
func (s *Server) switchQuiz(filename string, settings map[string]any) (switchResult, error) {
	path := filepath.Join(s.quizzesDir, filename)
	if _, err := os.Stat(path); err != nil {
		return switchResult{}, fmt.Errorf("Quiz file not found: %s", filename)
	}
	loaded, err := quiz.Load(path)
	if err != nil {
		return switchResult{}, err
	}
	checkerWarnings := badCheckers(path, loaded) // compiled before the lock is taken

	s.csvMu.Lock() // a flush that writes files of the old run ends first
	defer s.csvMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	files, err := s.newCSVFilesLocked(filename)
	if err != nil {
		return switchResult{}, err
	}
	s.resetStateLocked()
	s.log.Info("Server state reset for new quiz")
	if settings != nil {
		s.startSettings = maps.Clone(settings)
		s.startSettingsQuiz = filename
	} else if filename != s.startSettingsQuiz {
		s.startSettings = map[string]any{}
		s.startSettingsQuiz = ""
	}
	s.currentQuizFile = path
	s.csvFile, s.userCSVFile, s.answersWithUsersCSVFile = files.answers, files.users, files.answersWithUsers

	if loaded.TimeLimitWarning != "" {
		s.log.Warn(fmt.Sprintf("%s: %s", path, loaded.TimeLimitWarning))
	}
	s.log.Info(fmt.Sprintf("Loaded %d questions from %s, show_right_answer: %s, randomize_questions: %s, "+
		"show_answers_on_completion: %s, show_final_list: %s, show_explanation: %s, explanations: %d, time_limit: %s %ds",
		len(loaded.Questions), path, pyBool(loaded.ShowRightAnswer), pyBool(loaded.RandomizeQuestions),
		pyBool(loaded.ShowAnswersOnCompletion), pyBool(loaded.ShowFinalList), pyBool(loaded.ShowExplanation),
		len(loaded.ExplanationsHTML), loaded.TimeLimit.Mode, loaded.TimeLimit.Seconds))
	for _, warning := range checkerWarnings {
		s.log.Warn(warning)
	}
	applyStartSettings(loaded, s.startSettings)
	if len(s.startSettings) > 0 {
		s.log.Info(fmt.Sprintf("Quiz runs with start settings (quiz file not changed): %s", pyval.Str(s.startSettings)))
	}
	s.quiz = loaded

	if err := s.writeQuizPageLocked(); err != nil {
		return switchResult{}, err
	}
	s.broadcastLiveStats(map[string]any{
		"type":            "quiz_switched",
		"current_quiz":    filename,
		"questions":       loaded.Questions,
		"total_questions": len(loaded.Questions),
		"message":         "Quiz switched to: " + filename,
	})
	s.log.Info(fmt.Sprintf("Switched to quiz: %s, CSV: %s", filename, s.csvFile))
	return switchResult{csvFile: s.csvFile, settings: s.currentQuizSettingsLocked()}, nil
}

// badCheckers compiles the checker code of every question and returns a
// log warning for each checker the Starlark checker cannot run. Python
// checked checker code only when a quiz was saved (and would have run such
// code); answers to these questions are marked wrong with the compile error.
func badCheckers(path string, q *quiz.Quiz) []string {
	var warnings []string
	for _, question := range q.Questions {
		code, ok := question["checker"].(string)
		if !ok || code == "" {
			continue
		}
		if err := checker.Validate(code); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: Question %d checker does not compile, its answers will be marked wrong: %s",
				path, question.ID(), err.Error()))
		}
	}
	return warnings
}

// startSettingKeys are the quiz settings the admin may change when starting
// a quiz (START_SETTING_KEYS).
var startSettingKeys = []string{
	"show_right_answer",
	"randomize_questions",
	"show_answers_on_completion",
	"show_final_list",
	"show_explanation",
	"time_limit",
}

// validateStartSettings is _validate_start_settings: the problems with the
// start settings of a switch-quiz request. keys are the keys of settings in
// the order of the request (Python went through them in that order).
func validateStartSettings(settings any, keys []string) []string {
	object, ok := settings.(map[string]any)
	if !ok {
		return []string{"'settings' must be an object"}
	}
	problems := []string{}
	for _, key := range keys {
		value := object[key]
		switch {
		case !slices.Contains(startSettingKeys, key):
			problems = append(problems, fmt.Sprintf("'%s' cannot be changed when starting a quiz", key))
		case key == "time_limit":
			if value != nil { // null turns the time limit off
				problems = append(problems, timelimits.Validate(map[string]any{"time_limit": value})...)
			}
		case !pyval.IsBool(value):
			problems = append(problems, fmt.Sprintf("'%s' must be a boolean (true or false)", key))
		}
	}
	return problems
}

// currentQuizSettingsLocked is current_quiz_settings: the settings the
// running quiz uses (quiz file values with the start settings applied). The
// caller holds mu.
func (s *Server) currentQuizSettingsLocked() map[string]any {
	var timeLimit any
	if s.quiz.TimeLimit.Mode != timelimits.ModeNone {
		timeLimit = map[string]any{s.quiz.TimeLimit.Mode: s.quiz.TimeLimit.Seconds}
	}
	return map[string]any{
		"show_right_answer":          s.quiz.ShowRightAnswer,
		"randomize_questions":        s.quiz.RandomizeQuestions,
		"show_answers_on_completion": s.quiz.ShowAnswersOnCompletion,
		"show_final_list":            s.quiz.ShowFinalList,
		"show_explanation":           s.quiz.ShowExplanation,
		"time_limit":                 timeLimit,
	}
}

// applyStartSettings is _apply_start_settings: the admin's start settings
// replace the quiz file values of the loaded quiz (q is not shared yet).
func applyStartSettings(q *quiz.Quiz, settings map[string]any) {
	flags := map[string]*bool{
		"show_right_answer":          &q.ShowRightAnswer,
		"randomize_questions":        &q.RandomizeQuestions,
		"show_answers_on_completion": &q.ShowAnswersOnCompletion,
		"show_final_list":            &q.ShowFinalList,
		"show_explanation":           &q.ShowExplanation,
	}
	for key, target := range flags {
		if value, ok := settings[key]; ok {
			*target = pyval.Truthy(value)
		}
	}
	if value, ok := settings["time_limit"]; ok {
		if value == nil {
			q.TimeLimit = timelimits.NoLimit
		} else {
			q.TimeLimit, _ = timelimits.Parse(map[string]any{"time_limit": value})
		}
		q.QuestionTimeLimits = timelimits.QuestionLimits(q.TimeLimit, q.Questions)
	}
}

// currentQuizName is the file name of the running quiz, or "" (Python None).
// The caller holds mu.
func (s *Server) currentQuizName() string {
	if s.currentQuizFile == "" {
		return ""
	}
	return filepath.Base(s.currentQuizFile)
}
