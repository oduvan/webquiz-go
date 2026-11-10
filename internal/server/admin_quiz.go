package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/oduvan/webquiz-go/internal/pyval"
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

// adminForceShowAnswers is POST /api/admin/force-show-answers.
func (s *Server) adminForceShowAnswers(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminListQuizzes is GET /api/admin/list-quizzes.
func (s *Server) adminListQuizzes(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
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

// adminGetQuiz is GET /api/admin/quiz/{filename}.
func (s *Server) adminGetQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminPreviewExplanation is POST /api/admin/preview-explanation.
func (s *Server) adminPreviewExplanation(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminCreateQuiz is POST /api/admin/create-quiz.
func (s *Server) adminCreateQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminUpdateQuiz is PUT /api/admin/quiz/{filename}.
func (s *Server) adminUpdateQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminDeleteQuiz is DELETE /api/admin/quiz/{filename}.
func (s *Server) adminDeleteQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminUniteQuizzes is POST /api/admin/unite-quizzes.
func (s *Server) adminUniteQuizzes(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminValidateQuiz is POST /api/admin/validate-quiz.
func (s *Server) adminValidateQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminListImages is GET /api/admin/list-images.
func (s *Server) adminListImages(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminListFiles is GET /api/admin/list-files.
func (s *Server) adminListFiles(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminListCheckerTemplates is GET /api/admin/list-checker-templates.
func (s *Server) adminListCheckerTemplates(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}
