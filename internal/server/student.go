package server

import (
	"net/http"
	"time"
)

// The student API. Task 7 builds these handlers (server.py register_user,
// update_registration, submit_answer, question_start, verify_user_id) and
// the time limit check.

// registerUser is POST /api/register.
func (s *Server) registerUser(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// updateRegistration is PUT /api/update-registration.
func (s *Server) updateRegistration(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// submitAnswer is POST /api/submit-answer.
func (s *Server) submitAnswer(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// questionStart is POST /api/question-start.
func (s *Server) questionStart(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// verifyUserID is GET /api/verify-user/{user_id}.
func (s *Server) verifyUserID(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// checkTimeLimits is check_time_limits: the time limit watcher calls it
// every Options.CheckInterval with the current time (s.now()). It stores
// timeouts for students whose page went quiet.
func (s *Server) checkTimeLimits(now time.Time) error {
	_ = now
	return nil
}
