package server

import (
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
	"github.com/oduvan/webquiz-go/internal/timelimits"
)

// The student API: server.py register_user, update_registration,
// verify_user_id, question_start, submit_answer with _record_answer and
// _end_whole_quiz.
//
// Python handled one request at a time; Go runs handlers in parallel. Each
// handler does its state work in one locked step, in Python's order, so a
// request sent twice at the same moment (a retry, a double click) gets the
// same reply as a retry sent later: one stored user per registration token,
// one stored answer per question. Only a checker runs without the lock (it
// may take 2 seconds); see submitAnswer.

// data is the student as Python's user_data dict: user_id, username,
// registered_at, approved, the registration fields and, for a shuffled quiz,
// question_order.
func (u *User) data() map[string]any {
	data := map[string]any{
		"user_id":       u.ID,
		"username":      u.Username,
		"registered_at": u.RegisteredAt,
		"approved":      u.Approved,
	}
	for name, value := range u.Fields {
		data[name] = value
	}
	if u.QuestionOrder != nil {
		data["question_order"] = slices.Clone(u.QuestionOrder)
	}
	return data
}

// addUserLocked adds a registered student. The caller holds mu.
func (s *Server) addUserLocked(user *User) {
	if user.Fields == nil {
		user.Fields = map[string]string{}
	}
	s.users[user.ID] = user
	s.userOrder = append(s.userOrder, user.ID)
}

// errorReply is the {"error": text} body of a refused request.
func errorReply(text string) map[string]any {
	return map[string]any{"error": text}
}

// randomQuestionOrderLocked is generate_random_question_order: the question
// ids shuffled, with sticky questions kept right after the one before them.
// The caller holds mu.
func (s *Server) randomQuestionOrderLocked() []int {
	order := quiz.RandomOrder(s.quiz.Questions, nil)
	s.log.Info(fmt.Sprintf("Generated random question order with sticky groups: %s", pyval.Str(order)))
	return order
}

// registerUser is POST /api/register.
func (s *Server) registerUser(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	status, reply, err := s.register(data)
	if err != nil {
		return err
	}
	return writeJSON(w, status, reply)
}

// register is register_user. A repeated registration with the same
// registration_token (the page retries when the connection drops) gets the
// student the first one made.
func (s *Server) register(data any) (int, map[string]any, error) {
	rawUsername, err := pyGetItem(data, "username")
	if err != nil {
		return 0, nil, err
	}
	username, err := pyStrip(rawUsername)
	if err != nil {
		return 0, nil, err
	}
	request := data.(map[string]any) // data["username"] worked, so data is an object
	token, _ := request["registration_token"].(string)

	s.mu.Lock()
	defer s.mu.Unlock()
	if userID, ok := s.registrationTokens[token]; ok && token != "" {
		if _, exists := s.users[userID]; exists {
			s.log.Info(fmt.Sprintf("Repeated registration for user %s, returning the existing user", userID))
			return http.StatusOK, s.registrationReplyLocked(userID), nil
		}
	}
	if username == "" {
		return http.StatusBadRequest, errorReply(s.translations["server_username_empty"]), nil
	}
	for _, existing := range s.users {
		if existing.Username == username {
			return http.StatusBadRequest, errorReply(s.translations["server_username_exists"]), nil
		}
	}
	userID := ""
	for range 100 {
		candidate := strconv.Itoa(100000 + rand.IntN(900000))
		if _, taken := s.users[candidate]; !taken {
			userID = candidate
			break
		}
	}
	if userID == "" {
		return http.StatusInternalServerError, errorReply(s.translations["server_user_id_generation_failed"]), nil
	}

	requiresApproval := s.cfg.Registration.Approve
	user := &User{
		ID:           userID,
		Username:     username,
		RegisteredAt: isoformat(s.now()),
		Approved:     !requiresApproval,
		Fields:       map[string]string{},
	}
	for _, label := range s.cfg.Registration.Fields {
		name := registrationFieldName(label)
		value, err := pyStrip(valueOr(request, name, ""))
		if err != nil {
			return 0, nil, err
		}
		if value == "" {
			text := strings.ReplaceAll(s.translations["server_field_empty"], "{field}", label)
			return http.StatusBadRequest, errorReply(text), nil
		}
		user.Fields[name] = value
	}
	if s.quiz.RandomizeQuestions {
		user.QuestionOrder = s.randomQuestionOrderLocked()
		s.log.Info(fmt.Sprintf("Generated random question order for user %s: %s", userID, pyval.Str(user.QuestionOrder)))
	}
	s.addUserLocked(user)
	if token != "" {
		s.registrationTokens[token] = userID
	}

	if requiresApproval {
		s.broadcastAdmin(map[string]any{
			"type":                "new_registration",
			"user_id":             userID,
			"username":            username,
			"registration_fields": s.registrationFieldsLocked(user),
		})
	} else {
		// The question clock starts later, when the page shows the question.
		s.questionReadyTimes[userID] = s.now()
		s.startLiveStatsLocked(user)
	}
	s.log.Info(fmt.Sprintf("Registered user: %s with ID: %s, requires_approval: %s", username, userID, pyBool(requiresApproval)))
	return http.StatusOK, s.registrationReplyLocked(userID), nil
}

// registrationReplyLocked is _registration_response. The caller holds mu.
func (s *Server) registrationReplyLocked(userID string) map[string]any {
	user := s.users[userID]
	reply := map[string]any{
		"username":          user.Username,
		"user_id":           userID,
		"message":           "User registered successfully",
		"requires_approval": s.cfg.Registration.Approve,
		"approved":          user.Approved,
	}
	if s.quiz.RandomizeQuestions && user.QuestionOrder != nil {
		reply["question_order"] = slices.Clone(user.QuestionOrder)
	}
	return reply
}

// valueOr is Python's d.get(key, fallback).
func valueOr(d map[string]any, key string, fallback any) any {
	if value, ok := d[key]; ok {
		return value
	}
	return fallback
}

// updateRegistration is PUT /api/update-registration.
func (s *Server) updateRegistration(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	status, reply, err := s.updateUser(data)
	if err != nil {
		return err
	}
	return writeJSON(w, status, reply)
}

// updateUser is update_registration: a student who waits for approval may
// change the name and the registration fields.
func (s *Server) updateUser(data any) (int, map[string]any, error) {
	rawUserID, err := pyGet(data, "user_id", nil)
	if err != nil {
		return 0, nil, err
	}
	if !pyval.Truthy(rawUserID) {
		return http.StatusBadRequest, errorReply("User ID is required"), nil
	}
	request := data.(map[string]any)

	s.mu.Lock()
	defer s.mu.Unlock()
	userID, _, err := pyStrKey(rawUserID)
	if err != nil {
		return 0, nil, err
	}
	user, ok := s.users[userID]
	if !ok {
		return http.StatusNotFound, errorReply(s.translations["server_update_not_found"]), nil
	}
	if user.Approved {
		return http.StatusBadRequest, errorReply(s.translations["server_update_after_approval"]), nil
	}
	username, err := pyStrip(valueOr(request, "username", ""))
	if err != nil {
		return 0, nil, err
	}
	if username != "" {
		for otherID, other := range s.users {
			if otherID != userID && other.Username == username {
				return http.StatusBadRequest, errorReply(s.translations["server_username_exists"]), nil
			}
		}
		user.Username = username
	}
	for _, label := range s.cfg.Registration.Fields {
		name := registrationFieldName(label)
		value, err := pyStrip(valueOr(request, name, ""))
		if err != nil {
			return 0, nil, err // like Python, the changes made so far stay
		}
		if value != "" {
			user.Fields[name] = value
		}
	}
	s.broadcastAdmin(map[string]any{
		"type":                "registration_updated",
		"user_id":             userID,
		"username":            user.Username,
		"registration_fields": s.registrationFieldsLocked(user),
	})
	s.log.Info(fmt.Sprintf("Updated registration data for user: %s", userID))
	return http.StatusOK, map[string]any{
		"success":   true,
		"message":   "Registration data updated successfully",
		"user_data": user.data(),
	}, nil
}

// verifyUserID is GET /api/verify-user/{user_id}.
func (s *Server) verifyUserID(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, http.StatusOK, s.verifyUser(r.PathValue("user_id")))
}

// verifyUser is verify_user_id: the page asks where the student is (after a
// reload, or to check the approval): waiting, the next question, or the
// final results.
func (s *Server) verifyUser(userID string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[userID]
	if !ok {
		return map[string]any{"valid": false, "message": "User ID not found"}
	}
	shuffled := s.quiz.RandomizeQuestions && user.QuestionOrder != nil
	if s.cfg.Registration.Approve && !user.Approved {
		reply := map[string]any{
			"valid":             true,
			"user_id":           userID,
			"username":          user.Username,
			"approved":          false,
			"requires_approval": true,
			"user_data":         user.data(),
			"message":           "User waiting for approval",
		}
		if shuffled {
			reply["question_order"] = slices.Clone(user.QuestionOrder)
		}
		return reply
	}

	lastAnswered := s.userProgress[userID]
	next := 0
	if lastAnswered > 0 {
		if shuffled {
			if index := slices.Index(user.QuestionOrder, lastAnswered); index >= 0 {
				next = index + 1
			}
		} else {
			for i, question := range s.quiz.Questions {
				if question.ID() == lastAnswered {
					next = i + 1
					break
				}
			}
		}
	}
	total := len(s.quiz.Questions)
	next = min(next, total)
	completed := next >= total
	reply := map[string]any{
		"valid":                     true,
		"user_id":                   userID,
		"username":                  user.Username,
		"approved":                  user.Approved,
		"next_question_index":       next,
		"total_questions":           total,
		"last_answered_question_id": lastAnswered,
		"test_completed":            completed,
	}
	if shuffled {
		reply["question_order"] = slices.Clone(user.QuestionOrder)
	}
	if completed {
		reply["final_results"] = s.userFinalResultsLocked(userID)
		s.log.Info(fmt.Sprintf("User %s verification: test completed, returning final results", userID))
	} else {
		s.log.Info(fmt.Sprintf("User %s verification: last_answered=%d, next_index=%d", userID, lastAnswered, next))
	}
	return reply
}

// unansweredQuestionIDsLocked is _unanswered_question_ids: the questions the
// student has not answered yet, in the student's order. The caller holds mu
// and knows that the student exists.
func (s *Server) unansweredQuestionIDsLocked(userID string) []int {
	order := s.users[userID].QuestionOrder
	if !s.quiz.RandomizeQuestions || order == nil {
		order = make([]int, 0, len(s.quiz.Questions))
		for _, question := range s.quiz.Questions {
			order = append(order, question.ID())
		}
	}
	answered := s.answerResults[userID]
	unanswered := []int{}
	for _, questionID := range order {
		if _, done := answered[questionID]; !done {
			unanswered = append(unanswered, questionID)
		}
	}
	return unanswered
}

// questionStart is POST /api/question-start: the page shows a question.
func (s *Server) questionStart(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	rawUserID, err := pyGetItem(data, "user_id")
	if err != nil {
		return err
	}
	rawQuestionID, err := pyGetItem(data, "question_id")
	if err != nil {
		return err
	}
	status, reply, err := s.startQuestion(rawUserID, rawQuestionID)
	if err != nil {
		return err
	}
	return writeJSON(w, status, reply)
}

// startQuestion is question_start: it starts the question clock (and the
// whole-quiz clock once) and answers with the remaining times. A notice for
// an answered question, for a question that is not the student's next one,
// or a repeated notice (a page reload) does not restart any clock.
func (s *Server) startQuestion(rawUserID, rawQuestionID any) (int, map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userID, _, err := pyStrKey(rawUserID)
	if err != nil {
		return 0, nil, err
	}
	user, ok := s.users[userID]
	if !ok {
		return http.StatusNotFound, errorReply(s.translations["server_user_not_found"]), nil
	}
	question, ok := s.quiz.QuestionByID(rawQuestionID)
	if !ok {
		return http.StatusNotFound, errorReply(s.translations["server_question_not_found"]), nil
	}
	questionID := question.ID()
	if _, answered := s.answerResults[userID][questionID]; answered {
		return http.StatusOK, map[string]any{"status": "success"}, nil
	}
	if unanswered := s.unansweredQuestionIDsLocked(userID); len(unanswered) == 0 || unanswered[0] != questionID {
		return http.StatusOK, map[string]any{"status": "success"}, nil
	}

	now := s.now()
	if _, running := s.questionStartTimes[userID]; !running {
		s.questionStartTimes[userID] = now
		s.currentQuestionIDs[userID] = questionID
	}
	if _, started := s.quizStartTimes[userID]; !started {
		s.quizStartTimes[userID] = now
	}
	s.updateLiveStatsLocked(userID, questionID, "think", nil)
	s.broadcastLiveStats(map[string]any{
		"type":            "state_update",
		"user_id":         userID,
		"username":        user.Username,
		"question_id":     questionID,
		"state":           "think",
		"time_taken":      nil,
		"total_questions": len(s.quiz.Questions),
		"total_points":    s.totalPointsLocked(),
		"question_points": points(question.Points()),
	})
	reply := map[string]any{"status": "success"}
	maps.Copy(reply, s.remainingTimesLocked(userID, questionID, now))
	return http.StatusOK, reply, nil
}

// submission is one POST /api/submit-answer while it is handled.
type submission struct {
	rawUserID     any // the request values as sent
	rawQuestionID any
	answer        any

	now     time.Time // when the answer arrived; set by the first step
	arrived bool
	grade   *grade // the checker's result, once it ran without the lock
	// checking is the user_id whose answersBeingChecked count this request
	// holds while its checker runs ("" when it holds none).
	checking string
}

// grade is the result of checking one answer.
type grade struct {
	quiz         *quiz.Quiz // the quiz of the question: a switch makes the result useless
	questionID   int
	correct      bool
	checkerError string
}

// gradeAnswer is _validate_answer for one answer. For a question with
// checker code it runs the checker, which may take up to 2 seconds.
func gradeAnswer(q *quiz.Quiz, question quiz.Question, answer any) *grade {
	correct, checkerError := quiz.CheckAnswer(question, answer)
	return &grade{quiz: q, questionID: question.ID(), correct: correct, checkerError: checkerError}
}

// runsChecker tells whether checking an answer to the question runs checker
// code (a text question with a non-empty checker).
func runsChecker(question quiz.Question) bool {
	code, ok := question["checker"].(string)
	return ok && code != ""
}

// checkJob is a checker run that a submission needs before its answer can
// be stored.
type checkJob struct {
	quiz     *quiz.Quiz
	question quiz.Question
	answer   any
}

// submitAnswer is POST /api/submit-answer.
//
// The answer is handled in steps under the lock (submitStep), in the order
// of the Python code. When the answer must run checker code, the step
// stores nothing and asks for the checker; the checker runs without the
// lock, and the next step runs the whole order again with the checker's
// result and the arrival time of the answer. So a retry that came in
// meanwhile, a quiz switch or a timeout is seen as it would have been in
// Python. While its checker runs the student is left alone by the time
// limit watcher.
func (s *Server) submitAnswer(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	sub := &submission{}
	if sub.rawUserID, err = pyGetItem(data, "user_id"); err != nil {
		return err
	}
	if sub.rawQuestionID, err = pyGetItem(data, "question_id"); err != nil {
		return err
	}
	if sub.answer, err = pyGetItem(data, "selected_answer"); err != nil {
		return err
	}
	defer s.stopChecking(sub)
	for {
		status, reply, job, err := s.submitStep(sub)
		if err != nil {
			return err
		}
		if job == nil {
			return writeJSON(w, status, reply)
		}
		sub.grade = gradeAnswer(job.quiz, job.question, job.answer)
	}
}

// submitStep runs one locked step of submitAnswer. It returns the reply (a
// copy that may be used without the lock), or a checker job.
func (s *Server) submitStep(sub *submission) (int, map[string]any, *checkJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCheckLocked(sub)
	status, reply, job, err := s.submitLocked(sub)
	if job != nil {
		sub.checking, _, _ = pyStrKey(sub.rawUserID)
		s.answersBeingChecked[sub.checking]++
	}
	return status, maps.Clone(reply), job, err
}

// stopChecking gives back the answersBeingChecked count of a request that
// ends while it holds one (when the checker panicked, say).
func (s *Server) stopChecking(sub *submission) {
	if sub.checking == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCheckLocked(sub)
}

// releaseCheckLocked gives back the answersBeingChecked count of sub, if it
// holds one. The caller holds mu.
func (s *Server) releaseCheckLocked(sub *submission) {
	if sub.checking == "" {
		return
	}
	if s.answersBeingChecked[sub.checking]--; s.answersBeingChecked[sub.checking] <= 0 {
		delete(s.answersBeingChecked, sub.checking)
	}
	sub.checking = ""
}

// submitLocked is submit_answer: the checks of the Python code in their
// order, then the answer is stored (or a timeout, or the end of the whole
// quiz). For an answer that runs checker code without a usable sub.grade it
// stores nothing and returns the checker job. The caller holds mu.
func (s *Server) submitLocked(sub *submission) (int, map[string]any, *checkJob, error) {
	userID, _, err := pyStrKey(sub.rawUserID)
	if err != nil {
		return 0, nil, nil, err
	}
	user, ok := s.users[userID]
	if !ok {
		return http.StatusNotFound, errorReply(s.translations["server_user_not_found"]), nil, nil
	}
	if kind := pyval.KindOf(sub.rawQuestionID); kind == pyval.KindList || kind == pyval.KindDict {
		return 0, nil, nil, fmt.Errorf("unhashable type: '%s'", kind) // answer_results[...].get(question_id)
	}
	question, known := s.quiz.QuestionByID(sub.rawQuestionID)

	// A repeated request gets the stored reply: the same answer again, or
	// any answer after a timeout (so a late retry lets the page go on).
	var previous *answerResult
	if known {
		previous = s.answerResults[userID][question.ID()]
	}
	if previous != nil && (previous.TimedOut || pyval.Equal(previous.SelectedAnswer, sub.answer)) {
		s.log.Info(fmt.Sprintf("Repeated answer from user %s for question %s, returning stored result", userID, pyval.Str(sub.rawQuestionID)))
		return http.StatusOK, previous.Response, nil, nil
	}

	// A shuffled quiz accepts only the student's next question.
	if s.quiz.RandomizeQuestions && user.QuestionOrder != nil {
		order := user.QuestionOrder
		var expected int
		if last := s.userProgress[userID]; last == 0 {
			if len(order) == 0 {
				return 0, nil, nil, errors.New("list index out of range")
			}
			expected = order[0]
		} else {
			index := slices.Index(order, last)
			if index < 0 {
				s.log.Error(fmt.Sprintf("User %s last_answered_id %d not found in question_order", userID, last))
				return http.StatusInternalServerError, errorReply(s.translations["server_question_order_error"]), nil, nil
			}
			if index+1 >= len(order) {
				return http.StatusBadRequest, errorReply(s.translations["server_all_answered"]), nil, nil
			}
			expected = order[index+1]
		}
		if !pyval.Equal(sub.rawQuestionID, expected) {
			s.log.Warn(fmt.Sprintf("User %s attempted to answer question %s but expected question %d", userID, pyval.Str(sub.rawQuestionID), expected))
			return http.StatusForbidden, map[string]any{
				"error":                s.translations["server_only_current_question"],
				"expected_question_id": expected,
			}, nil, nil
		}
	}

	if previous != nil {
		return http.StatusConflict, errorReply(s.translations["server_already_answered"]), nil, nil
	}
	if !known {
		return http.StatusNotFound, errorReply(s.translations["server_question_not_found"]), nil, nil
	}
	questionID := question.ID()

	if !sub.arrived {
		sub.now, sub.arrived = s.now(), true
	}
	now := sub.now
	empty := timelimits.IsEmptyAnswer(sub.answer)

	// The page always sends question-start before an answer. An answer
	// without it was sent by hand: the whole-quiz clock then counts from when
	// the first question became available.
	if _, started := s.quizStartTimes[userID]; !started {
		if ready, ok := s.questionReadyTimes[userID]; ok {
			s.quizStartTimes[userID] = ready
		}
	}

	// Whole-quiz limit: the page sends an empty answer when the quiz countdown
	// reaches zero. That, or any answer after the time and the grace, ends
	// the whole quiz.
	if wholeQuizSeconds(s.quiz) > 0 {
		start, started := s.quizStartTimes[userID]
		if empty || (started && s.wholeQuizOverLocked(start, now)) {
			reply := s.endWholeQuizLocked(userID, now, questionID)
			if reply == nil {
				return http.StatusBadRequest, errorReply(s.translations["server_all_answered"]), nil, nil
			}
			return http.StatusOK, reply, nil, nil
		}
	}

	// An empty answer means "time ran out": only a question with a limit takes it.
	if empty {
		if !s.questionIsLimitedLocked(questionID) {
			return http.StatusBadRequest, errorReply(s.translations["server_empty_answer"]), nil, nil
		}
		elapsed := s.questionElapsedLocked(userID, questionID, now)
		return http.StatusOK, s.recordAnswerLocked(userID, question, sub.answer, elapsed, now, recordTimeout, nil), nil, nil
	}

	// Per-question limit: an answer after the limit and the grace is a timeout.
	if limit := s.quiz.QuestionTimeLimits[questionID]; limit > 0 {
		if elapsed := s.answerElapsedLocked(userID, questionID, now); elapsed.later(limit, s.grace) {
			s.log.Info(fmt.Sprintf("Answer from user %s for question %d arrived after the time limit", userID, questionID))
			return http.StatusOK, s.recordAnswerLocked(userID, question, sub.answer, elapsed, now, recordTimeout, nil), nil, nil
		}
	}

	timeTaken := s.questionElapsedLocked(userID, questionID, now)
	g := sub.grade
	if g == nil || g.quiz != s.quiz || g.questionID != questionID {
		if runsChecker(question) {
			return 0, nil, &checkJob{quiz: s.quiz, question: question, answer: sub.answer}, nil
		}
		g = gradeAnswer(s.quiz, question, sub.answer)
	}
	return http.StatusOK, s.recordAnswerLocked(userID, question, sub.answer, timeTaken, now, recordAnswer, g), nil, nil
}

// recordKind says what recordAnswerLocked stores.
type recordKind int

const (
	recordAnswer  recordKind = iota // an answer, checked by its grade
	recordTimeout                   // time ran out for the question
	recordQuizEnd                   // time ran out for the whole quiz: the reply has no explanation
)

// recordAnswerLocked is _record_answer: it stores an answer or a timeout,
// moves the student on, updates the live stats and the final results, and
// returns the reply for the page. A timeout is an empty, wrong answer: no
// checker runs and no points are earned. now is when the answer arrived (or
// the time ran out): the next question becomes available then, as Python
// set question_ready_times before the checker ran. The caller holds mu.
func (s *Server) recordAnswerLocked(userID string, question quiz.Question, selected any, timeTaken seconds, now time.Time, kind recordKind, g *grade) map[string]any {
	user := s.users[userID]
	questionID := question.ID()
	timedOut := kind != recordAnswer

	// This question's clock is finished; an answer to another question
	// leaves a running clock alone.
	if current, ok := s.currentQuestionIDs[userID]; ok && current == questionID {
		delete(s.questionStartTimes, userID)
		delete(s.currentQuestionIDs, userID)
	}
	s.questionReadyTimes[userID] = now

	isText := question.IsText()
	correct, checkerError := false, ""
	if !timedOut {
		correct = g.correct
		if isText {
			checkerError = g.checkerError
		}
	}
	var correctAnswer any
	if isText {
		correctAnswer = valueOr(question, "correct_value", "")
	} else {
		correctAnswer = quiz.FormatAnswer(question["correct_answer"], question["options"], false)
	}
	selectedText := "" // an empty answer marks "time ran out"
	if !timedOut {
		selectedText = quiz.FormatAnswer(selected, question["options"], isText)
	}
	questionText := valueOr(question, "question", "")

	s.userResponses = append(s.userResponses, &answerRow{
		UserID:         userID,
		Username:       user.Username,
		Question:       questionText,
		SelectedAnswer: selectedText,
		CorrectAnswer:  correctAnswer,
		IsCorrect:      correct,
		TimeTaken:      timeTaken,
	})

	questionPoints := question.Points()
	earnedPoints := 0.0
	if correct {
		earnedPoints = questionPoints
	}
	s.userAnswers[userID] = append(s.userAnswers[userID], &answerRecord{
		Question:       questionText,
		Image:          question["image"],
		File:           attachmentValue(question),
		SelectedAnswer: selectedText,
		CorrectAnswer:  correctAnswer,
		IsCorrect:      correct,
		TimeTaken:      timeTaken,
		Points:         questionPoints,
		EarnedPoints:   earnedPoints,
		CheckerError:   checkerError,
		TimedOut:       timedOut,
	})
	s.userProgress[userID] = questionID

	state := "fail"
	switch {
	case timedOut:
		state = "timeout"
	case correct:
		state = "ok"
	}
	taken := timeTaken
	s.updateLiveStatsLocked(userID, questionID, state, &taken)

	// The number of answers (not the question id) tells when the student is
	// done, so a shuffled order works too.
	completed := len(s.userAnswers[userID]) == len(s.quiz.Questions)
	var completedAt any
	if completed {
		s.calculateAndStoreUserStatsLocked(userID)
		if stats, ok := s.userStats[userID]; ok {
			completedAt = stats.CompletedAt
		}
		s.log.Info(fmt.Sprintf("Test completed for user %s - final stats calculated", userID))
	}

	reply := map[string]any{
		"time_taken":     timeTaken,
		"message":        "Answer submitted successfully",
		"test_completed": completed,
	}
	if timedOut {
		reply["timed_out"] = true // so the page can say "time is up" even when correct answers are hidden
	}
	if s.quiz.ShowRightAnswer {
		reply["is_correct"] = correct
		if isText {
			reply["is_text_question"] = true
			reply["correct_value"] = valueOr(question, "correct_value", "")
			if checkerError != "" {
				reply["checker_error"] = checkerError
			}
		} else {
			reply["correct_answer"] = question["correct_answer"]
			reply["is_multiple_choice"] = question.IsMultipleChoice()
		}
		if html, ok := s.quiz.ExplanationsHTML[questionID]; ok && kind != recordQuizEnd && s.quiz.ShowExplanation {
			reply["explanation_html"] = html
		}
	}

	// Stored before the live stats hear about it, as in Python: a retry gets this reply.
	results, ok := s.answerResults[userID]
	if !ok {
		results = map[int]*answerResult{}
		s.answerResults[userID] = results
	}
	results[questionID] = &answerResult{SelectedAnswer: selected, Response: reply, TimedOut: timedOut}

	s.broadcastLiveStats(map[string]any{
		"type":            "state_update",
		"user_id":         userID,
		"username":        user.Username,
		"question_id":     questionID,
		"state":           state,
		"time_taken":      timeTaken,
		"total_questions": len(s.quiz.Questions),
		"total_points":    s.totalPointsLocked(),
		"earned_points":   points(earnedPoints),
		"question_points": points(questionPoints),
		"completed":       completed,
		"completed_at":    completedAt,
	})

	outcome := "Incorrect"
	switch {
	case timedOut:
		outcome = "Time ran out"
	case correct:
		outcome = "Correct"
	}
	s.log.Info(fmt.Sprintf("Answer submitted by %s (ID: %s) for question %d: %s (took %ss)", user.Username, userID, questionID, outcome, timeTaken))
	s.log.Info(fmt.Sprintf("Updated progress for user %s: last answered question = %d", userID, questionID))
	return reply
}

// attachmentValue is the "file" of a stored answer: the download address
// "/attach/<name>" for a file name, or the value as written when it is
// empty (nil when the question has no file).
func attachmentValue(question quiz.Question) any {
	value := question["file"]
	if name, ok := value.(string); ok && name != "" && !strings.HasPrefix(name, "/attach/") {
		return "/attach/" + name
	}
	return value
}

// endWholeQuizLocked is _end_whole_quiz: the whole-quiz time is up, so every
// question the student has not answered is stored as timed out, in the
// student's order. The open question keeps its real time; questions never
// opened get 0. questionID is the question the page sent its answer for, or
// 0 for the background check. It returns the stored reply for that question
// (for the first timed-out question when questionID is 0), or nil when
// nothing is stored for it. The caller holds mu.
//
// Python had to check the state again before each question, because each
// store waited for the live stats send; here nothing else runs meanwhile.
func (s *Server) endWholeQuizLocked(userID string, now time.Time, questionID int) map[string]any {
	if _, ok := s.users[userID]; !ok {
		return nil
	}
	unanswered := s.unansweredQuestionIDsLocked(userID)
	// The times first: storing the first timeout stops the running clock.
	elapsed := make(map[int]seconds, len(unanswered))
	for _, id := range unanswered {
		elapsed[id] = s.questionElapsedLocked(userID, id, now)
	}
	s.log.Info(fmt.Sprintf("Whole-quiz time is up for user %s, %d questions left without an answer", userID, len(unanswered)))

	firstStored := 0
	for _, id := range unanswered {
		question, ok := s.quiz.QuestionByID(id)
		if !ok {
			continue
		}
		s.recordAnswerLocked(userID, question, nil, elapsed[id], now, recordQuizEnd, nil)
		if firstStored == 0 {
			firstStored = id
		}
	}

	wanted := questionID
	if wanted == 0 {
		wanted = firstStored
	}
	stored := s.answerResults[userID][wanted]
	if stored == nil {
		return nil
	}
	// The same map is stored in answerResults, so a retry also learns that the test is over.
	_, done := s.userStats[userID]
	stored.Response["test_completed"] = done
	return stored.Response
}
