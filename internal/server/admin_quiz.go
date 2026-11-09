package server

import "net/http"

// The admin quiz API. Task 8 builds these handlers; their names follow the
// Python methods (admin_approve_user, admin_list_quizzes, ...). All of them
// are behind adminAuthRequired.

// adminApproveUser is PUT /api/admin/approve-user.
func (s *Server) adminApproveUser(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminForceShowAnswers is POST /api/admin/force-show-answers.
func (s *Server) adminForceShowAnswers(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminListQuizzes is GET /api/admin/list-quizzes.
func (s *Server) adminListQuizzes(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminSwitchQuiz is POST /api/admin/switch-quiz.
func (s *Server) adminSwitchQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
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
