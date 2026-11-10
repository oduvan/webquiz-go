package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oduvan/webquiz-go/internal/explanations"
	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
)

// The admin quiz API. Their names follow the Python methods
// (admin_approve_user, admin_list_quizzes, ...). All of them are behind
// adminAuthRequired. approve-user and switch-quiz belong to the student flow
// (Task 7); Task 8 builds the others.

// adminApproveUser is PUT /api/admin/approve-user (admin_approve_user): the
// student may start. The question clock starts later, when the student's
// page shows the question (after "Check status").
func (s *Server) adminApproveUser(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	rawUserID, err := pyGet(data, "user_id", nil)
	if err != nil {
		return err
	}
	if !pyval.Truthy(rawUserID) {
		return writeError(w, http.StatusBadRequest, "User ID is required")
	}
	status, reply, err := s.approveUser(rawUserID)
	if err != nil {
		return err
	}
	return writeJSON(w, status, reply)
}

// approveUser approves a student who waits: the live stats pages get the
// student, the admin pages hear "user_approved".
func (s *Server) approveUser(rawUserID any) (int, map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userID, _, err := pyStrKey(rawUserID)
	if err != nil {
		return 0, nil, err
	}
	user, ok := s.users[userID]
	if !ok {
		return http.StatusNotFound, errorReply("User not found"), nil
	}
	if user.Approved {
		return http.StatusOK, map[string]any{"success": true, "message": "User already approved"}, nil
	}
	user.Approved = true
	if s.quiz.RandomizeQuestions && user.QuestionOrder == nil {
		user.QuestionOrder = s.randomQuestionOrderLocked()
		s.log.Info(fmt.Sprintf("Generated random question order for approved user %s: %s", userID, pyval.Str(user.QuestionOrder)))
	}
	s.questionReadyTimes[userID] = s.now()
	s.startLiveStatsLocked(user)
	s.broadcastAdmin(map[string]any{"type": "user_approved", "user_id": userID})
	s.log.Info(fmt.Sprintf("Approved user: %s", userID))
	return http.StatusOK, map[string]any{"success": true, "message": "User approved successfully", "user_id": userID}, nil
}

// adminSwitchQuiz is POST /api/admin/switch-quiz (admin_switch_quiz): it
// starts a quiz as a new run, with the optional start settings of the start
// pop-up ("settings"). Bad settings answer 400 with the list of problems,
// and the running quiz stays.
func (s *Server) adminSwitchQuiz(w http.ResponseWriter, r *http.Request) error {
	body, err := readBody(r)
	if err != nil {
		return err
	}
	data, err := decodeJSON(body)
	if err != nil {
		return err
	}
	rawFilename, err := pyGetItem(data, "quiz_filename")
	if err != nil {
		return err
	}
	settings := data.(map[string]any)["settings"]
	var startSettings map[string]any
	if settings != nil {
		if problems := validateStartSettings(settings, jsonKeyOrder(body, "settings")); len(problems) > 0 {
			return writeJSON(w, http.StatusBadRequest, map[string]any{"error": strings.Join(problems, "; "), "errors": problems})
		}
		startSettings = settings.(map[string]any)
	}
	filename, ok := rawFilename.(string)
	if !ok {
		return fmt.Errorf("join() argument must be str, bytes, or os.PathLike object, not '%s'", pyval.KindOf(rawFilename))
	}
	result, err := s.switchQuiz(filename, startSettings)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"message":      "Switched to quiz: " + filename,
		"current_quiz": filename,
		"csv_file":     filepath.Base(result.csvFile),
		"settings":     result.settings,
	})
}

// invalidQuizStructure is the answer for quiz data that fails the checks.
// The details are not sent here; validate-quiz sends them.
const invalidQuizStructure = "Неправильна структура даних квізу"

// quizPath is os.path.join(quizzes_dir, name) for a name that stays inside
// quizzes_dir. A name with a folder part ("../x", "a/b", an absolute path)
// gives false. Python joined such names as they were and so reached files
// outside the quiz folder; the Go version does not.
func (s *Server) quizPath(name string) (string, bool) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") ||
		(os.PathSeparator == '\\' && strings.ContainsRune(name, '\\')) {
		return "", false
	}
	return filepath.Join(s.quizzesDir, name), true
}

// activeQuiz is os.path.basename(current_quiz_file), or "" before a quiz runs.
func (s *Server) activeQuiz() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentQuizName()
}

// readTextFile reads a file like open(path, encoding="utf-8").read(): line
// ends \r\n and \r become \n.
func readTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n"), nil
}

// copyFile is shutil.copy2: the content, the permissions and the times.
func copyFile(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}

// adminForceShowAnswers is POST /api/admin/force-show-answers
// (admin_force_show_answers): the answers open for everyone, until the next
// quiz switch.
func (s *Server) adminForceShowAnswers(w http.ResponseWriter, r *http.Request) error {
	var completed, total int
	s.withLock(func() {
		s.forceAllCompleted = true
		completed, total = len(s.userStats), len(s.users)
	})
	s.log.Info(fmt.Sprintf("Admin force-enabled answer visibility. Completed students: %d, Total students: %d", completed, total))
	s.broadcastAdmin(map[string]any{"type": "answers_forced", "forced": true, "message": "Відповіді тепер доступні для всіх учнів"})
	return writeJSON(w, http.StatusOK, map[string]any{
		"success":         true,
		"forced":          true,
		"message":         "Відповіді успішно відкриті для всіх учнів",
		"completed_count": completed,
		"total_count":     total,
	})
}

// adminListQuizzes is GET /api/admin/list-quizzes (admin_list_quizzes).
func (s *Server) adminListQuizzes(w http.ResponseWriter, r *http.Request) error {
	quizzes := s.listAvailableQuizzes()
	var current any
	var forced, onCompletion bool
	s.withLock(func() {
		if name := s.currentQuizName(); name != "" {
			current = name
		}
		forced, onCompletion = s.forceAllCompleted, s.quiz.ShowAnswersOnCompletion
	})
	return writeJSON(w, http.StatusOK, map[string]any{
		"quizzes":                    quizzes,
		"current_quiz":               current,
		"force_all_completed":        forced,
		"show_answers_on_completion": onCompletion,
	})
}

// adminGetQuiz is GET /api/admin/quiz/{filename} (admin_get_quiz): the file
// as text and as parsed data, for the editor.
func (s *Server) adminGetQuiz(w http.ResponseWriter, r *http.Request) error {
	filename := r.PathValue("filename")
	path, ok := s.quizPath(filename)
	if !ok || !pathExists(path) {
		return writeError(w, http.StatusNotFound, "Quiz file not found")
	}
	content, err := readTextFile(path)
	if err != nil {
		return err
	}
	parsed, err := pyval.LoadYAML([]byte(content))
	if err != nil {
		return writeJSON(w, http.StatusOK, map[string]any{"filename": filename, "content": content, "parsed": nil, "yaml_error": err.Error()})
	}
	return writeJSON(w, http.StatusOK, map[string]any{"filename": filename, "content": content, "parsed": jsonSafe(parsed)})
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// adminPreviewExplanation is POST /api/admin/preview-explanation
// (admin_preview_explanation): the same renderer that students get.
func (s *Server) adminPreviewExplanation(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return writeError(w, http.StatusBadRequest, "Invalid JSON")
	}
	var markdown any
	if object, ok := data.(map[string]any); ok {
		markdown = object["markdown"]
	}
	text, ok := markdown.(string)
	if !ok {
		return writeError(w, http.StatusBadRequest, "'markdown' must be text")
	}
	return writeJSON(w, http.StatusOK, map[string]any{"html": explanations.Render(text, explanations.AdminImagesURL)})
}

// quizContent makes the file text for a create or update request from its
// mode: "wizard" dumps data["quiz_data"], any other mode takes
// data["content"] as YAML text. A non-empty reply means the request is bad;
// it was written already.
func quizContent(w http.ResponseWriter, data any) (content string, replied bool, err error) {
	mode, err := pyGet(data, "mode", "wizard")
	if err != nil {
		return "", false, err
	}
	if mode == "wizard" {
		quizData, err := pyGet(data, "quiz_data", map[string]any{})
		if err != nil {
			return "", false, err
		}
		if len(quiz.Validate(quizData)) > 0 {
			return "", true, writeError(w, http.StatusBadRequest, invalidQuizStructure)
		}
		dumped, err := dumpYAML(quizData)
		if err != nil {
			return "", false, err
		}
		return string(dumped), false, nil
	}
	rawContent, err := pyGet(data, "content", "")
	if err != nil {
		return "", false, err
	}
	content, err = pyStrip(rawContent)
	if err != nil {
		return "", false, err
	}
	if content == "" {
		return "", true, writeError(w, http.StatusBadRequest, "Quiz content is required")
	}
	parsed, yamlErr := pyval.LoadYAML([]byte(content))
	if yamlErr != nil {
		return "", true, writeError(w, http.StatusBadRequest, "Неправильний YAML: "+yamlErr.Error())
	}
	if len(quiz.Validate(parsed)) > 0 {
		return "", true, writeError(w, http.StatusBadRequest, invalidQuizStructure)
	}
	return content, false, nil
}

// adminCreateQuiz is POST /api/admin/create-quiz (admin_create_quiz).
func (s *Server) adminCreateQuiz(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	rawFilename, err := pyGet(data, "filename", "")
	if err != nil {
		return err
	}
	filename, err := pyStrip(rawFilename)
	if err != nil {
		return err
	}
	if filename == "" {
		return writeError(w, http.StatusBadRequest, "Filename is required")
	}
	if !strings.HasSuffix(filename, ".yaml") {
		filename += ".yaml"
	}
	path, ok := s.quizPath(filename)
	if !ok {
		return writeError(w, http.StatusBadRequest, "Invalid filename")
	}
	if pathExists(path) {
		return writeError(w, http.StatusConflict, "Quiz file already exists")
	}
	content, replied, err := quizContent(w, data)
	if replied || err != nil {
		return err
	}
	if err := writeSynced(path, content, os.O_CREATE|os.O_TRUNC); err != nil {
		return err
	}
	s.log.Info("Created new quiz: " + filename)
	return writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"message":  fmt.Sprintf("Quiz \"%s\" created successfully", filename),
		"filename": filename,
	})
}

// adminUpdateQuiz is PUT /api/admin/quiz/{filename} (admin_update_quiz). It
// keeps a .backup of the old file, can rename a quiz that is not running,
// and restarts the quiz when the running one was changed.
func (s *Server) adminUpdateQuiz(w http.ResponseWriter, r *http.Request) error {
	oldFilename := r.PathValue("filename")
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	if _, err := pyGet(data, "mode", "wizard"); err != nil {
		return err
	}
	rawFilename, err := pyGet(data, "filename", "")
	if err != nil {
		return err
	}
	newFilename, err := pyStrip(rawFilename)
	if err != nil {
		return err
	}
	if newFilename == "" {
		newFilename = strings.ReplaceAll(strings.ReplaceAll(oldFilename, ".yaml", ""), ".yml", "")
	}
	if !strings.HasSuffix(newFilename, ".yaml") && !strings.HasSuffix(newFilename, ".yml") {
		newFilename += ".yaml"
	}
	oldPath, oldOK := s.quizPath(oldFilename)
	newPath, newOK := s.quizPath(newFilename)
	if !oldOK || !pathExists(oldPath) {
		return writeError(w, http.StatusNotFound, "Quiz file not found")
	}
	if !newOK {
		return writeError(w, http.StatusBadRequest, "Invalid filename")
	}

	renamed := oldFilename != newFilename
	if renamed {
		if s.activeQuiz() == oldFilename {
			return writeError(w, http.StatusConflict, "Cannot rename active quiz. Please switch to a different quiz first.")
		}
		if pathExists(newPath) {
			return writeError(w, http.StatusConflict, "Quiz file already exists: "+newFilename)
		}
	}

	backupPath := oldPath + ".backup"
	if err := copyFile(oldPath, backupPath); err != nil {
		return err
	}
	content, replied, err := quizContent(w, data)
	if replied || err != nil {
		return err
	}
	if err := writeSynced(oldPath, content, os.O_CREATE|os.O_TRUNC); err != nil {
		return err
	}
	if renamed {
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		s.log.Info(fmt.Sprintf("Renamed quiz: %s -> %s", oldFilename, newFilename))
	}
	finalFilename := oldFilename
	if renamed {
		finalFilename = newFilename
	}
	if s.activeQuiz() == finalFilename {
		if err := s.SwitchQuiz(finalFilename, nil); err != nil {
			return err
		}
	}
	s.log.Info("Updated quiz: " + finalFilename)
	return writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"message":        fmt.Sprintf("Quiz \"%s\" updated successfully", finalFilename),
		"backup_created": filepath.Base(backupPath),
		"filename":       finalFilename,
		"renamed":        renamed,
	})
}

// adminDeleteQuiz is DELETE /api/admin/quiz/{filename} (admin_delete_quiz):
// the file is kept as .deleted_backup.
func (s *Server) adminDeleteQuiz(w http.ResponseWriter, r *http.Request) error {
	filename := r.PathValue("filename")
	path, ok := s.quizPath(filename)
	if !ok || !pathExists(path) {
		return writeError(w, http.StatusNotFound, "Quiz file not found")
	}
	if s.activeQuiz() == filename {
		return writeError(w, http.StatusBadRequest, "Cannot delete the currently active quiz")
	}
	backupPath := path + ".deleted_backup"
	if err := copyFile(path, backupPath); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("Deleted quiz: %s (backup: %s)", filename, backupPath))
	return writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"message":        fmt.Sprintf("Quiz \"%s\" deleted successfully", filename),
		"backup_created": filepath.Base(backupPath),
	})
}

// uniteOptionalFields are copied from the first quiz. min_correct is not
// here: it belongs to its question.
var uniteOptionalFields = []string{
	"description", "show_right_answer", "randomize_questions", "show_answers_on_completion",
	"show_final_list", "show_explanation", "time_limit",
}

// adminUniteQuizzes is POST /api/admin/unite-quizzes (admin_unite_quizzes):
// one new quiz with the settings of the first and all questions in order.
func (s *Server) adminUniteQuizzes(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	rawNames, err := pyGet(data, "quiz_filenames", []any{})
	if err != nil {
		return err
	}
	rawNewName, err := pyGet(data, "new_name", "")
	if err != nil {
		return err
	}
	newName, err := pyStrip(rawNewName)
	if err != nil {
		return err
	}

	var filenames []any
	switch names := rawNames.(type) {
	case []any:
		filenames = names
	case string:
		for _, char := range names { // Python goes through the letters of a text
			filenames = append(filenames, string(char))
		}
	default:
		if !pyval.Truthy(rawNames) {
			break
		}
		if _, ok := pyval.Len(rawNames); !ok {
			return fmt.Errorf("object of type '%s' has no len()", pyval.KindOf(rawNames))
		}
		return fmt.Errorf("'%s' object is not iterable", pyval.KindOf(rawNames))
	}
	if len(filenames) < 2 {
		return writeError(w, http.StatusBadRequest, "Потрібно вибрати принаймні 2 quiz для об'єднання")
	}
	if newName == "" {
		return writeError(w, http.StatusBadRequest, "Ім'я нового quiz обов'язкове")
	}
	if !strings.HasSuffix(newName, ".yaml") && !strings.HasSuffix(newName, ".yml") {
		newName += ".yaml"
	}
	newPath, ok := s.quizPath(newName)
	if !ok {
		return writeError(w, http.StatusBadRequest, "Invalid filename")
	}
	if pathExists(newPath) {
		return writeError(w, http.StatusConflict, fmt.Sprintf("Quiz '%s' вже існує", newName))
	}

	var quizzesData []map[string]any
	for _, rawName := range filenames {
		name, ok := rawName.(string)
		if !ok {
			return fmt.Errorf("join() argument must be str, bytes, or os.PathLike object, not '%s'", pyval.KindOf(rawName))
		}
		path, ok := s.quizPath(name)
		if !ok || !pathExists(path) {
			return writeError(w, http.StatusNotFound, fmt.Sprintf("Quiz '%s' не знайдено", name))
		}
		content, err := readTextFile(path)
		if err != nil {
			return err
		}
		parsed, yamlErr := pyval.LoadYAML([]byte(content))
		if yamlErr != nil {
			return writeError(w, http.StatusBadRequest, fmt.Sprintf("Помилка читання quiz '%s': %s", name, yamlErr))
		}
		if problems := quiz.Validate(parsed); len(problems) > 0 {
			return writeError(w, http.StatusBadRequest, fmt.Sprintf("Quiz '%s' має неправильну структуру: %s", name, problems[0]))
		}
		quizzesData = append(quizzesData, parsed.(map[string]any))
	}

	united := map[string]any{"title": "United Quiz"}
	if title, ok := quizzesData[0]["title"]; ok {
		united["title"] = title
	}
	for _, field := range uniteOptionalFields {
		if value, ok := quizzesData[0][field]; ok {
			united[field] = value
		}
	}
	questions := []any{}
	seen := map[string]bool{}
	duplicates := 0
	for _, quizData := range quizzesData {
		items, _ := quizData["questions"].([]any)
		for _, item := range items {
			question := item.(map[string]any)
			text := question["question"]
			if !pyval.Truthy(text) {
				text = question["image"]
				if text == nil {
					text = ""
				}
			}
			file := question["file"]
			if file == nil {
				if _, has := question["file"]; !has {
					file = ""
				}
			}
			key := pyval.Repr(text) + "\x00" + pyval.Repr(file)
			if seen[key] {
				duplicates++
			} else {
				seen[key] = true
			}
			questions = append(questions, question)
		}
	}
	united["questions"] = questions

	dumped, err := dumpYAML(united)
	if err != nil {
		return err
	}
	if err := writeSynced(newPath, string(dumped), os.O_CREATE|os.O_TRUNC); err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("United %d quizzes into '%s': %d questions", len(filenames), newName, len(questions)))

	reply := map[string]any{
		"success":         true,
		"message":         "Quiz успішно об'єднано",
		"filename":        newName,
		"total_questions": len(questions),
		"source_quizzes":  rawNames,
	}
	if duplicates > 0 {
		reply["warning"] = fmt.Sprintf("Знайдено %d можливих дублікатів питань", duplicates)
	}
	return writeJSON(w, http.StatusOK, reply)
}

// adminValidateQuiz is POST /api/admin/validate-quiz (admin_validate_quiz).
func (s *Server) adminValidateQuiz(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	rawContent, err := pyGet(data, "content", "")
	if err != nil {
		return err
	}
	content, err := pyStrip(rawContent)
	if err != nil {
		return err
	}
	if content == "" {
		return writeJSON(w, http.StatusOK, map[string]any{"valid": false, "errors": []string{"Content is empty"}})
	}
	parsed, yamlErr := pyval.LoadYAML([]byte(content))
	if yamlErr != nil {
		return writeJSON(w, http.StatusOK, map[string]any{"valid": false, "errors": []string{"YAML syntax error: " + yamlErr.Error()}})
	}
	if problems := quiz.Validate(parsed); len(problems) > 0 {
		return writeJSON(w, http.StatusOK, map[string]any{"valid": false, "errors": problems})
	}
	questions, _ := parsed.(map[string]any)["questions"].([]any)
	return writeJSON(w, http.StatusOK, map[string]any{"valid": true, "parsed": jsonSafe(parsed), "question_count": len(questions)})
}

// adminListImages is GET /api/admin/list-images (admin_list_images).
func (s *Server) adminListImages(w http.ResponseWriter, r *http.Request) error {
	images := []map[string]any{}
	entries, err := os.ReadDir(filepath.Join(s.quizzesDir, "imgs"))
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			lower := strings.ToLower(name)
			for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".bmp", ".svg"} {
				if strings.HasSuffix(lower, ext) {
					images = append(images, map[string]any{"filename": name, "path": "/imgs/" + name})
					break
				}
			}
		}
	}
	slices.SortStableFunc(images, func(a, b map[string]any) int {
		return strings.Compare(strings.ToLower(a["filename"].(string)), strings.ToLower(b["filename"].(string)))
	})
	return writeJSON(w, http.StatusOK, map[string]any{"images": images})
}

// adminListFiles is GET /api/admin/list-files (admin_list_files): the
// attachments in quizzes/attach.
func (s *Server) adminListFiles(w http.ResponseWriter, r *http.Request) error {
	files := []map[string]any{}
	dir := filepath.Join(s.quizzesDir, "attach")
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			info, err := os.Stat(filepath.Join(dir, entry.Name()))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			files = append(files, map[string]any{"filename": entry.Name(), "path": "/attach/" + entry.Name(), "size": info.Size()})
		}
	}
	slices.SortStableFunc(files, func(a, b map[string]any) int {
		return strings.Compare(strings.ToLower(a["filename"].(string)), strings.ToLower(b["filename"].(string)))
	})
	return writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// adminListCheckerTemplates is GET /api/admin/list-checker-templates.
func (s *Server) adminListCheckerTemplates(w http.ResponseWriter, r *http.Request) error {
	templates := []map[string]any{}
	s.withLock(func() {
		for _, template := range s.cfg.CheckerTemplates {
			templates = append(templates, map[string]any{"name": template.Name, "code": template.Code})
		}
	})
	return writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}
