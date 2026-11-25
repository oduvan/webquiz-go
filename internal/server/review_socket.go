package server

import (
	"fmt"
	"net/http"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
)

// The manual check of answers (manual_check: true). An answer to such a
// question waits in s.reviewQueue (internal/review) until a teacher marks it
// right or wrong on the review page (/review/), which is fed over the
// WebSocket /ws/review:
//
//   - the server sends {"type": "review_state", "item": {...} | null,
//     "pending_count": N, "has_manual": bool, "already_decided": bool} on
//     connect, after each verdict of this page, and whenever the count or
//     this page's answer changes. already_decided is true once, after a
//     verdict for an answer that another teacher had already decided;
//   - the page sends {"type": "verdict", "review_id": "7", "is_correct":
//     true}, {"type": "take_shared"} and {"type": "ping"} (answered with
//     {"type": "pong"}). A verdict whose is_correct is not a boolean is
//     ignored: the page gets its state again;
//   - the admin pages hear {"type": "review_status", "has_manual",
//     "pending_count", "reviewers"} after every change, and get the same
//     values as "review" in their initial_state.
//
// Every page gets its own state, so the messages are queued per client (not
// broadcast), always under mu right after the change, like the broadcasts.

// reviewRefs are the stored answer and the CSV row that a verdict changes.
type reviewRefs struct {
	record *answerRecord
	row    *answerRow
}

// addToReviewLocked puts a stored answer to a manual_check question into the
// review queue. The review page gets the question, the answer and the sample
// answer, never the student's name. The caller holds mu.
func (s *Server) addToReviewLocked(userID string, question quiz.Question, answer string, record *answerRecord, row *answerRow) {
	var image any
	if value := question["image"]; pyval.Truthy(value) {
		image = value // question.get("image") or None
	}
	s.reviewQueue.Add(userID, question.ID(), map[string]any{
		"question":      valueOr(question, "question", ""),
		"image":         image,
		"answer":        answer,
		"correct_value": valueOr(question, "correct_value", ""),
		"points":        points(question.Points()),
	}, reviewRefs{record: record, row: row})
}

// hasManualQuestionsLocked tells whether the running quiz has questions with
// manual_check: true. The caller holds mu.
func (s *Server) hasManualQuestionsLocked() bool {
	for _, question := range s.quiz.Questions {
		if question.IsManual() {
			return true
		}
	}
	return false
}

// reviewStatusLocked is review_status: the summary for the admin panel. The
// caller holds mu.
func (s *Server) reviewStatusLocked() map[string]any {
	return map[string]any{
		"has_manual":    s.hasManualQuestionsLocked(),
		"pending_count": s.reviewQueue.PendingCount(),
		"reviewers":     s.reviewQueue.Reviewers(),
	}
}

// reviewStateLocked is the review_state message for one review page. The
// caller holds mu.
func (s *Server) reviewStateLocked(c *wsClient, alreadyDecided bool) map[string]any {
	var item any
	if shown := s.reviewQueue.ItemFor(c); shown != nil {
		copied := make(map[string]any, len(shown.Public))
		for key, value := range shown.Public {
			copied[key] = value
		}
		item = copied
	}
	return map[string]any{
		"type":            "review_state",
		"item":            item,
		"pending_count":   s.reviewQueue.PendingCount(),
		"has_manual":      s.hasManualQuestionsLocked(),
		"already_decided": alreadyDecided,
	}
}

// sendReviewStateLocked queues the page's review_state. A page whose queue
// is full is disconnected (it connects again and gets a fresh state). The
// caller holds mu.
func (s *Server) sendReviewStateLocked(c *wsClient, alreadyDecided bool) {
	if !s.reviewQueue.Connected(c) {
		return
	}
	data, err := encodeJSON(s.reviewStateLocked(c, alreadyDecided))
	if err != nil {
		s.log.Error(fmt.Sprintf("Failed to encode a review WebSocket message: %v", err))
		return
	}
	if !c.queue(data) {
		s.log.Warn("Failed to send to review WebSocket client: too many messages are waiting")
		c.stop()
		_ = c.conn.CloseNow()
	}
}

// reviewChangedLocked is review_changed: it hands answers to review pages
// that have none, then tells every page and the admin panel. alreadyDecidedFor
// is the page that sent a verdict for an answer decided elsewhere (it gets
// already_decided: true once), or nil. The caller holds mu.
func (s *Server) reviewChangedLocked(alreadyDecidedFor *wsClient) {
	s.reviewQueue.AssignIdle()
	for _, c := range s.reviewQueue.Clients() {
		s.sendReviewStateLocked(c, c == alreadyDecidedFor)
	}
	status := s.reviewStatusLocked()
	status["type"] = "review_status"
	s.broadcastAdmin(status)
}

// applyReviewVerdictLocked is apply_review_verdict: it stores the teacher's
// verdict for a waiting answer. It updates the stored answer, the CSV row
// (the files are written again at the next flush), the final results of a
// student who has finished (keeping the time they finished) and the live
// stats. It is false when the answer is not waiting any more (decided on
// another page, or the quiz restarted). The caller holds mu.
func (s *Server) applyReviewVerdictLocked(reviewID string, isCorrect bool) bool {
	item := s.reviewQueue.Decide(reviewID)
	if item == nil {
		return false
	}
	record, row := item.Refs.record, item.Refs.row
	record.IsCorrect = isCorrect
	record.PendingReview = false
	record.EarnedPoints = 0
	if isCorrect {
		record.EarnedPoints = record.Points
	}
	row.IsCorrect = isCorrect
	row.PendingReview = false
	s.csvRewriteNeeded = true

	userID := item.UserID
	var completedAt any
	if stats, ok := s.userStats[userID]; ok {
		finished := stats.CompletedAt
		s.calculateAndStoreUserStatsLocked(userID)
		if again, ok := s.userStats[userID]; ok {
			again.CompletedAt = finished
		}
		completedAt = finished
	}

	state := "fail"
	if isCorrect {
		state = "ok"
	}
	taken := record.TimeTaken
	s.updateLiveStatsLocked(userID, item.QuestionID, state, &taken)
	outcome := "incorrect"
	if isCorrect {
		outcome = "correct"
	}
	s.log.Info(fmt.Sprintf("Teacher marked the answer of user %s for question %d as %s", userID, item.QuestionID, outcome))

	username := ""
	if user, ok := s.users[userID]; ok {
		username = user.Username
	}
	s.broadcastLiveStats(map[string]any{
		"type":            "state_update",
		"user_id":         userID,
		"username":        username,
		"question_id":     item.QuestionID,
		"state":           state,
		"time_taken":      record.TimeTaken,
		"total_questions": len(s.quiz.Questions),
		"total_points":    s.totalPointsLocked(),
		"earned_points":   points(record.EarnedPoints),
		"question_points": points(record.Points),
		"completed":       len(s.userAnswers[userID]) == len(s.quiz.Questions),
		"completed_at":    completedAt,
	})
	return true
}

// websocketReview is /ws/review: a review page (admin session or trusted IP,
// local network only; the route's guard refuses others before the upgrade).
// The server pings the page every s.heartbeat, so a phone that went to sleep
// is dropped and its answer goes to another teacher.
func (s *Server) websocketReview(w http.ResponseWriter, r *http.Request) error {
	return s.serveWebSocketWith(w, r, s.reviewClients, s.joinReview, socketHooks{
		message:   s.reviewMessage,
		leave:     s.leaveReview,
		heartbeat: s.heartbeat,
	})
}

// joinReview adds a review page: it joins the hub and the queue, and every
// page and the admin panel hear about it.
func (s *Server) joinReview(c *wsClient) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.reviewClients.add(c, nil) {
		return false
	}
	s.reviewQueue.Connect(c)
	s.log.Info(fmt.Sprintf("Review page connected. Open review pages: %d", s.reviewQueue.Reviewers()))
	s.reviewChangedLocked(nil)
	return true
}

// leaveReview forgets a review page; its answer goes to a waiting page.
func (s *Server) leaveReview(c *wsClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reviewQueue.Disconnect(c)
	s.log.Info(fmt.Sprintf("Review page disconnected. Open review pages: %d", s.reviewQueue.Reviewers()))
	s.reviewChangedLocked(nil)
}

// reviewMessage handles one message of a review page. Text that is not a
// JSON object is ignored, as in Python.
func (s *Server) reviewMessage(c *wsClient, data []byte) {
	value, err := decodeJSON(data)
	if err != nil {
		return
	}
	message, ok := value.(map[string]any)
	if !ok {
		return
	}
	switch message["type"] {
	case "ping":
		c.queue([]byte(`{"type":"pong"}`))
	case "verdict":
		s.withLock(func() {
			isCorrect, ok := message["is_correct"].(bool)
			if !ok {
				s.sendReviewStateLocked(c, false)
				return
			}
			// str(data.get("review_id")): a number 7 finds review "7"
			applied := s.applyReviewVerdictLocked(pyval.Str(message["review_id"]), isCorrect)
			if applied {
				s.reviewChangedLocked(nil)
			} else {
				s.reviewChangedLocked(c)
			}
		})
	case "take_shared":
		// The teacher helps with an answer that is open on another teacher's page
		s.withLock(func() {
			if s.reviewQueue.TakeShared(c) {
				s.reviewChangedLocked(nil)
			} else {
				s.sendReviewStateLocked(c, false)
			}
		})
	}
}
