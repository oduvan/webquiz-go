package review

import (
	"reflect"
	"testing"
)

// The tests of tests/test_manual_review_unit.py (WebQuiz 1.x).

type queue = Queue[string, struct{}]

func add(q *queue, answer string) *Item[struct{}] {
	return q.Add("100001", 1, map[string]any{"answer": answer}, struct{}{})
}

// shown is the answer the client shows, or "" for none.
func shown(q *queue, client string) string {
	item := q.ItemFor(client)
	if item == nil {
		return ""
	}
	return item.Public["answer"].(string)
}

func TestReviewIDsCountUpAndPublicDataCarriesTheID(t *testing.T) {
	q := New[string, struct{}]()
	first := add(q, "a")
	second := add(q, "b")
	if first.ReviewID != "1" || second.ReviewID != "2" {
		t.Errorf("review ids %q %q, want 1 2", first.ReviewID, second.ReviewID)
	}
	if want := map[string]any{"answer": "a", "review_id": "1"}; !reflect.DeepEqual(first.Public, want) {
		t.Errorf("public = %v, want %v", first.Public, want)
	}
	if q.PendingCount() != 2 {
		t.Errorf("pending = %d, want 2", q.PendingCount())
	}
}

func TestAddCopiesThePublicData(t *testing.T) {
	q := New[string, struct{}]()
	public := map[string]any{"answer": "a"}
	q.Add("100001", 1, public, struct{}{})
	if _, ok := public["review_id"]; ok {
		t.Error("Add must not change the caller's map")
	}
}

func TestEachClientGetsADifferentAnswerWhileThereAreEnough(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	q.Connect("p2")
	add(q, "a")
	add(q, "b")
	add(q, "c")
	q.AssignIdle()
	if shown(q, "p1") != "a" || shown(q, "p2") != "b" {
		t.Errorf("p1 %q p2 %q, want a b", shown(q, "p1"), shown(q, "p2"))
	}
}

func TestClientWaitsWhenEveryAnswerIsOpenElsewhere(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	q.Connect("p2")
	q.Connect("p3")
	add(q, "a")
	add(q, "b")
	q.AssignIdle()
	got := []string{shown(q, "p1"), shown(q, "p2"), shown(q, "p3")}
	if want := []string{"a", "b", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("shown = %q, want %q", got, want)
	}
}

func TestAnswersArrivingOneByOneGoToDifferentClients(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	q.Connect("p2")
	add(q, "a")
	q.AssignIdle()
	add(q, "b")
	q.AssignIdle()
	if shown(q, "p1") != "a" || shown(q, "p2") != "b" {
		t.Errorf("p1 %q p2 %q, want a b", shown(q, "p1"), shown(q, "p2"))
	}
}

func TestTakeSharedGivesTheAnswerOpenOnFewestClients(t *testing.T) {
	q := New[string, struct{}]()
	for _, client := range []string{"p1", "p2", "p3", "p4"} {
		q.Connect(client)
	}
	add(q, "a")
	add(q, "b")
	q.AssignIdle()
	if !q.TakeShared("p3") || shown(q, "p3") != "a" {
		t.Errorf("p3 shows %q, want a", shown(q, "p3"))
	}
	if !q.TakeShared("p4") || shown(q, "p4") != "b" {
		t.Errorf("p4 shows %q, want b", shown(q, "p4"))
	}
	// A client that already shows an answer keeps it
	if q.TakeShared("p1") || shown(q, "p1") != "a" {
		t.Errorf("p1 shows %q, want a and no change", shown(q, "p1"))
	}
}

func TestTakeSharedWithNothingWaiting(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	if q.TakeShared("p1") || shown(q, "p1") != "" {
		t.Errorf("TakeShared with nothing waiting: p1 shows %q", shown(q, "p1"))
	}
	if q.TakeShared("not connected") {
		t.Error("TakeShared for a client that is not connected must be false")
	}
}

func TestAShownAnswerIsNeverSwapped(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	add(q, "a")
	q.AssignIdle()
	add(q, "b")
	if changed := q.AssignIdle(); len(changed) != 0 {
		t.Errorf("AssignIdle changed %v, want none", changed)
	}
	if shown(q, "p1") != "a" {
		t.Errorf("p1 shows %q, want a", shown(q, "p1"))
	}
}

func TestDecideFreesEveryClientShowingIt(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	q.Connect("p2")
	item := add(q, "a")
	q.AssignIdle()
	q.TakeShared("p2")
	if shown(q, "p2") != "a" {
		t.Fatalf("p2 shows %q, want a", shown(q, "p2"))
	}
	if got := q.Decide(item.ReviewID); got != item {
		t.Errorf("Decide = %v, want the item", got)
	}
	if shown(q, "p1") != "" || shown(q, "p2") != "" {
		t.Errorf("p1 %q p2 %q, want both free", shown(q, "p1"), shown(q, "p2"))
	}
	if q.Decide(item.ReviewID) != nil {
		t.Error("a second Decide must give nil")
	}
	if q.PendingCount() != 0 {
		t.Errorf("pending = %d, want 0", q.PendingCount())
	}
}

func TestAnswerOfAClosedClientGoesToAWaitingOne(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	add(q, "a")
	q.AssignIdle()
	q.Connect("p2")
	q.Disconnect("p1")
	if changed := q.AssignIdle(); !reflect.DeepEqual(changed, []string{"p2"}) {
		t.Errorf("AssignIdle = %v, want [p2]", changed)
	}
	if shown(q, "p2") != "a" {
		t.Errorf("p2 shows %q, want a", shown(q, "p2"))
	}
	if q.Reviewers() != 1 {
		t.Errorf("reviewers = %d, want 1", q.Reviewers())
	}
}

func TestClearKeepsClientsAndDropsAnswers(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	add(q, "a")
	q.AssignIdle()
	q.Clear()
	if q.PendingCount() != 0 || q.Reviewers() != 1 || shown(q, "p1") != "" {
		t.Errorf("after Clear: pending %d reviewers %d p1 %q", q.PendingCount(), q.Reviewers(), shown(q, "p1"))
	}
	if id := add(q, "b").ReviewID; id != "2" {
		t.Errorf("review id after Clear = %q, want 2", id)
	}
}

func TestClientsKeepTheirConnectionOrder(t *testing.T) {
	q := New[string, struct{}]()
	q.Connect("p1")
	q.Connect("p2")
	q.Connect("p3")
	q.Connect("p1") // connected already: keeps its place
	q.Disconnect("p2")
	if got := q.Clients(); !reflect.DeepEqual(got, []string{"p1", "p3"}) {
		t.Errorf("Clients = %v, want [p1 p3]", got)
	}
}
