package transfer

import (
	"sync"
	"testing"
)

// Python handled one request at a time; Go runs them in parallel. Two
// clicks on the same request at once (two admin pages, a double click)
// must still act once: the first one takes the request, the other gets
// "Request not found or expired" (404), as the second click got in Python.
func TestSameRequestAnsweredTwiceAtOnce(t *testing.T) {
	type answer func(m *machine, id string) error
	accept := func(m *machine, id string) error {
		_, err := m.manager.Accept(t.Context(), id, nil)
		return err
	}
	reject := func(m *machine, id string) error { return m.manager.Reject(t.Context(), id) }
	cases := map[string][2]answer{
		"two accepts":       {accept, accept},
		"accept and reject": {accept, reject},
		"two rejects":       {reject, reject},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			a, b, tunnel := twoMachines(t)
			a.write(t, "quizzes", "math.yaml", "title: Math\n")
			request := sendFiles(t, a, b, files(file("quizzes", "math.yaml")))
			tunnel.startRecording()

			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i, answer := range answers {
				wg.Go(func() { errs[i] = answer(b, request.ID) })
			}
			wg.Wait()

			failed := 0
			for _, err := range errs {
				if err == nil {
					continue
				}
				failed++
				if te := transferErr(t, err); te.Status != 404 || te.Message != "Request not found or expired" {
					t.Errorf("the second answer: %d %q", te.Status, te.Message)
				}
			}
			if failed != 1 {
				t.Fatalf("%d of 2 answers failed, want 1: %v", failed, errs)
			}
			downloads, results := 0, 0
			for _, entry := range tunnel.logSince(0) {
				switch entry.Method {
				case "GET":
					downloads++
				case "POST":
					results++
				}
			}
			if downloads > 1 || results != 1 {
				t.Errorf("%d downloads and %d results sent to the sender", downloads, results)
			}
			if got := listIncoming(t, b); len(got) != 0 {
				t.Errorf("incoming = %+v", got)
			}
			if status := state(t, a).Outgoing[0].Status; status != "accepted" && status != "rejected" {
				t.Errorf("offer status %q", status)
			}
		})
	}
}

// Offers that arrive at the same time are all kept, each with its own id,
// up to the limit of waiting requests.
func TestOffersArriveAtOnce(t *testing.T) {
	_, b, _ := twoMachines(t)
	data := map[string]any{"from": "room-a", "token": "t1234567890123456789012345678901234567890", "files": []any{map[string]any{"type": "logs", "name": "a.log"}}}
	errs := make([]error, maxIncomingRequests+5)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = b.manager.addIncoming(data) })
	}
	wg.Wait()
	refused := 0
	for _, err := range errs {
		if err != nil {
			if te := transferErr(t, err); te.Status != 429 {
				t.Errorf("error %d %q", te.Status, te.Message)
			}
			refused++
		}
	}
	requests := listIncoming(t, b)
	ids := map[string]bool{}
	for _, r := range requests {
		ids[r.ID] = true
	}
	if refused != 5 || len(requests) != maxIncomingRequests || len(ids) != maxIncomingRequests {
		t.Errorf("%d refused, %d requests, %d ids", refused, len(requests), len(ids))
	}
}
