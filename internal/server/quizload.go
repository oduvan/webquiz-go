package server

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
// nothing: the old quiz keeps running and the error is returned.
func (s *Server) SwitchQuiz(filename string, settings map[string]any) error {
	path := filepath.Join(s.quizzesDir, filename)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("Quiz file not found: %s", filename)
	}
	loaded, err := quiz.Load(path)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.startCSVFilesLocked(filename)

	if loaded.TimeLimitWarning != "" {
		s.log.Warn(fmt.Sprintf("%s: %s", path, loaded.TimeLimitWarning))
	}
	s.log.Info(fmt.Sprintf("Loaded %d questions from %s, show_right_answer: %s, randomize_questions: %s, "+
		"show_answers_on_completion: %s, show_final_list: %s, show_explanation: %s, explanations: %d, time_limit: %s %ds",
		len(loaded.Questions), path, pyBool(loaded.ShowRightAnswer), pyBool(loaded.RandomizeQuestions),
		pyBool(loaded.ShowAnswersOnCompletion), pyBool(loaded.ShowFinalList), pyBool(loaded.ShowExplanation),
		len(loaded.ExplanationsHTML), loaded.TimeLimit.Mode, loaded.TimeLimit.Seconds))
	applyStartSettings(loaded, s.startSettings)
	if len(s.startSettings) > 0 {
		s.log.Info(fmt.Sprintf("Quiz runs with start settings (quiz file not changed): %s", pyval.Str(s.startSettings)))
	}
	s.quiz = loaded

	if err := s.writeQuizPageLocked(); err != nil {
		return err
	}
	s.broadcastLiveStats(map[string]any{
		"type":            "quiz_switched",
		"current_quiz":    filename,
		"questions":       loaded.Questions,
		"total_questions": len(loaded.Questions),
		"message":         "Quiz switched to: " + filename,
	})
	s.log.Info(fmt.Sprintf("Switched to quiz: %s, CSV: %s", filename, s.csvFile))
	return nil
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
