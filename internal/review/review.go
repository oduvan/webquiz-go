// Package review is the queue of answers that wait for the teacher's check
// (questions with manual_check: true), a port of webquiz/manual_review.py.
//
// Teachers open the review page (/review/); each open page is a client here.
// The queue hands every client at most one answer at a time:
//
//   - a client without an answer gets the oldest answer that no client
//     shows, so teachers working together get different answers;
//   - when every waiting answer is already open on another client, the
//     client waits. The teacher may ask to help (TakeShared): the client then
//     gets the answer open on the fewest clients (the oldest of those). Two
//     teachers then see the same answer and the first verdict counts;
//   - a client keeps its answer until it gives a verdict, the answer is
//     decided on another client, or it disconnects. An answer is never
//     swapped under the teacher's finger.
//
// This package only keeps the order and the assignment. The server stores
// the verdict and sends the messages. A Queue is not safe for use by several
// goroutines at once: the server uses it under its own lock.
package review

import "strconv"

// Item is one answer that waits for a verdict. R is the server's own data
// for the verdict (the stored answer, the CSV row).
type Item[R any] struct {
	ReviewID   string
	UserID     string
	QuestionID int
	// Public is what the review page shows (never the student's name). It
	// carries "review_id" too.
	Public map[string]any
	Refs   R
}

// Queue keeps the waiting answers in the order they came and the clients in
// the order they connected. C is the client: anything that can be a map key.
type Queue[C comparable, R any] struct {
	items   map[string]*Item[R]
	order   []string // review ids, oldest first
	clients map[C]string
	joined  []C // clients, in connection order
	nextID  int
}

// New makes an empty queue.
func New[C comparable, R any]() *Queue[C, R] {
	return &Queue[C, R]{items: map[string]*Item[R]{}, clients: map[C]string{}, nextID: 1}
}

// PendingCount is the number of waiting answers.
func (q *Queue[C, R]) PendingCount() int { return len(q.order) }

// Reviewers is the number of connected clients.
func (q *Queue[C, R]) Reviewers() int { return len(q.joined) }

// Add puts an answer at the end of the queue and returns its item. Review
// ids count up from "1" and are never used twice, also after Clear.
func (q *Queue[C, R]) Add(userID string, questionID int, public map[string]any, refs R) *Item[R] {
	reviewID := strconv.Itoa(q.nextID)
	q.nextID++
	shown := make(map[string]any, len(public)+1)
	for key, value := range public {
		shown[key] = value
	}
	shown["review_id"] = reviewID
	item := &Item[R]{ReviewID: reviewID, UserID: userID, QuestionID: questionID, Public: shown, Refs: refs}
	q.items[reviewID] = item
	q.order = append(q.order, reviewID)
	return item
}

// Connect adds a client without an answer. A client that is connected
// already keeps its place and its answer.
func (q *Queue[C, R]) Connect(client C) {
	if _, ok := q.clients[client]; ok {
		return
	}
	q.clients[client] = ""
	q.joined = append(q.joined, client)
}

// Disconnect forgets a client. Its answer is free again for the others.
func (q *Queue[C, R]) Disconnect(client C) {
	if _, ok := q.clients[client]; !ok {
		return
	}
	delete(q.clients, client)
	for i, c := range q.joined {
		if c == client {
			q.joined = append(q.joined[:i], q.joined[i+1:]...)
			break
		}
	}
}

// Connected tells whether the client is connected.
func (q *Queue[C, R]) Connected(client C) bool {
	_, ok := q.clients[client]
	return ok
}

// Clients are the connected clients, in connection order.
func (q *Queue[C, R]) Clients() []C {
	return append([]C(nil), q.joined...)
}

// ItemFor is the answer the client shows, or nil.
func (q *Queue[C, R]) ItemFor(client C) *Item[R] {
	reviewID, ok := q.clients[client]
	if !ok || reviewID == "" {
		return nil
	}
	return q.items[reviewID]
}

// Decide takes a decided answer out of the queue, and frees every client
// that showed it. It returns nil when the answer is not waiting any more
// (decided on another page, or the quiz was restarted).
func (q *Queue[C, R]) Decide(reviewID string) *Item[R] {
	item, ok := q.items[reviewID]
	if !ok {
		return nil
	}
	delete(q.items, reviewID)
	for i, id := range q.order {
		if id == reviewID {
			q.order = append(q.order[:i], q.order[i+1:]...)
			break
		}
	}
	for client, shown := range q.clients {
		if shown == reviewID {
			q.clients[client] = ""
		}
	}
	return item
}

// Clear forgets every waiting answer (quiz switch or restart). Clients stay
// connected, without an answer.
func (q *Queue[C, R]) Clear() {
	q.items = map[string]*Item[R]{}
	q.order = nil
	for client := range q.clients {
		q.clients[client] = ""
	}
}

// AssignIdle gives an answer to every client that has none, in connection
// order: the oldest answer that no client shows. It returns the clients
// that got an answer.
func (q *Queue[C, R]) AssignIdle() []C {
	var changed []C
	for _, client := range q.joined {
		if shown := q.clients[client]; shown != "" {
			if _, waiting := q.items[shown]; waiting {
				continue
			}
		}
		viewers := q.viewers()
		reviewID := ""
		for _, id := range q.order {
			if viewers[id] == 0 {
				reviewID = id
				break
			}
		}
		q.clients[client] = reviewID
		if reviewID != "" {
			changed = append(changed, client)
		}
	}
	return changed
}

// TakeShared gives a waiting client an answer that is open on other
// clients (the teacher asked to help): the one open on the fewest clients,
// the oldest of those. It is false when the client is not connected, shows
// an answer already, or nothing waits.
func (q *Queue[C, R]) TakeShared(client C) bool {
	if !q.Connected(client) || q.ItemFor(client) != nil {
		return false
	}
	viewers := q.viewers()
	best := ""
	for _, id := range q.order { // oldest first, so ties go to the oldest
		if best == "" || viewers[id] < viewers[best] {
			best = id
		}
	}
	q.clients[client] = best
	return best != ""
}

// viewers is the number of clients that show each waiting answer.
func (q *Queue[C, R]) viewers() map[string]int {
	viewers := make(map[string]int, len(q.order))
	for _, id := range q.order {
		viewers[id] = 0
	}
	for _, shown := range q.clients {
		if _, ok := viewers[shown]; ok {
			viewers[shown]++
		}
	}
	return viewers
}
