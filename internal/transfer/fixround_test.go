package transfer

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// requireUnlocked fails when the manager's lock is still held.
func requireUnlocked(t *testing.T, m *Manager, after string) {
	t.Helper()
	if !m.mu.TryLock() {
		t.Fatalf("the manager stays locked after a panic in %s", after)
	}
	m.mu.Unlock()
}

// callAndRecover runs fn and returns the value it panicked with (nil when
// it did not panic).
func callAndRecover(fn func()) (value any) {
	defer func() { value = recover() }()
	fn()
	return nil
}

// A panic in a callback of the server (here FilesInUse) fails only the call
// that hit it: the manager's lock is free afterwards, so the transfer routes
// and the admin pages keep working. A request whose download panicked is
// removed, as Python's finally did.
func TestPanicInCallbackLeavesManagerUnlocked(t *testing.T) {
	a, b, tunnel := twoMachines(t)
	a.write(t, "quizzes", "diff.yaml", "from a")
	b.write(t, "quizzes", "diff.yaml", "b") // an existing file: its plan asks FilesInUse
	request := sendFiles(t, a, b, files(file("quizzes", "diff.yaml")))
	var panicking sync.Mutex
	inUsePanics := false
	setPanics := func(on bool) {
		panicking.Lock()
		defer panicking.Unlock()
		inUsePanics = on
	}
	b.manager.deps.FilesInUse = func() []string {
		panicking.Lock()
		defer panicking.Unlock()
		if inUsePanics {
			panic("files in use failed")
		}
		return nil
	}

	setPanics(true)
	offer := `{"from": "room-a", "token": "` + strings.Repeat("t", 43) + `", "files": [{"type": "quizzes", "name": "diff.yaml"}]}`
	for name, call := range map[string]func(){
		"ListIncoming": func() { _, _ = b.manager.ListIncoming() },
		"State":        func() { _, _ = b.manager.State() },
		"WithIncoming": func() { b.manager.WithIncoming(func([]RequestView, error) {}) },
		"Accept":       func() { _, _ = b.manager.Accept(t.Context(), request.ID, nil) },
		"HandleOffer": func() {
			_ = b.manager.HandleOffer(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/transfer/offer", strings.NewReader(offer)))
		},
	} {
		if value := callAndRecover(call); value != "files in use failed" {
			t.Errorf("%s: panic %v, want the callback's panic", name, value)
		}
		requireUnlocked(t, b.manager, name)
	}

	// The plan works again; the panic comes while the file is saved.
	setPanics(false)
	tunnel.setBeforeDownload(func() { setPanics(true) })
	value := callAndRecover(func() {
		_, _ = b.manager.Accept(t.Context(), request.ID, []any{map[string]any{"type": "quizzes", "name": "diff.yaml", "action": "replace"}})
	})
	if value != "files in use failed" {
		t.Fatalf("accept: panic %v", value)
	}
	requireUnlocked(t, b.manager, "Accept during the download")
	setPanics(false)
	b.manager.mu.Lock()
	left := len(b.manager.incoming)
	b.manager.mu.Unlock()
	if left != 1 { // the request of the HandleOffer call above stays, as in Python
		t.Errorf("%d requests left, want only the one of HandleOffer", left)
	}
	if message := b.lastMessage(t); message["type"] != "transfer_request_removed" || message["id"] != request.ID {
		t.Errorf("last message %v", message)
	}
	if got := b.files(t, "quizzes"); strings.Join(got, ",") != "diff.yaml" || b.read(t, "quizzes", "diff.yaml") != "b" {
		t.Errorf("files %v (no temporary file may stay)", got)
	}
}

// Two accepted requests that save a file under the same new name at the
// same moment: the first one saves it, the second one reports that the
// file appeared meanwhile. Nobody chose "replace", so no file may be
// replaced without notice.
func TestTwoRequestsSaveTheSameNameAtOnce(t *testing.T) {
	tunnel := newFakeTunnel(t)
	root := t.TempDir()
	a, b, c := tunnel.add(t, root, "room-a"), tunnel.add(t, root, "room-b"), tunnel.add(t, root, "room-c")
	a.write(t, "quizzes", "math.yaml", "from a")
	c.write(t, "quizzes", "math.yaml", "from c")
	if _, err := a.manager.Send(t.Context(), "room-b", files(file("quizzes", "math.yaml"))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.manager.Send(t.Context(), "room-b", files(file("quizzes", "math.yaml"))); err != nil {
		t.Fatal(err)
	}
	requests := listIncoming(t, b)
	if len(requests) != 2 || requests[0].Files[0].Case != "new" || requests[1].Files[0].Case != "new" {
		t.Fatalf("requests %+v", requests)
	}

	// Both downloads finish before either one saves.
	var arrivedMu sync.Mutex
	arrived := 0
	release := make(chan struct{})
	b.manager.beforeSave = func() {
		arrivedMu.Lock()
		arrived++
		if arrived == 2 {
			close(release)
		}
		arrivedMu.Unlock()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}
	results := make([]AcceptResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, r := range requests {
		wg.Go(func() { results[i], errs[i] = b.manager.Accept(t.Context(), r.ID, nil) })
	}
	wg.Wait()

	saved := map[string]bool{}
	failed := 0
	for i, result := range results {
		if errs[i] != nil {
			t.Fatalf("accept %d: %v", i, errs[i])
		}
		if len(result.Saved) == 1 {
			saved[result.From] = true
		}
		for _, f := range result.Failed {
			if f.Error != "A file named 'math.yaml' appeared meanwhile" {
				t.Errorf("failed: %+v", f)
			}
			failed++
		}
	}
	if len(saved) != 1 || failed != 1 {
		t.Fatalf("saved by %v, %d failed: both saved means one file replaced the other", saved, failed)
	}
	for from := range saved {
		if want := "from " + strings.TrimPrefix(from, "room-"); b.read(t, "quizzes", "math.yaml") != want {
			t.Errorf("math.yaml = %q, want %q (saved by %s)", b.read(t, "quizzes", "math.yaml"), want, from)
		}
	}
	if got := b.files(t, "quizzes"); strings.Join(got, ",") != "math.yaml" {
		t.Errorf("files %v (no temporary file may stay)", got)
	}
}

// The other server's answer to an offer is only read for its error text:
// at most 1 MiB of it is read. A bigger answer is cut there, so it is not
// valid JSON any more and the status is shown instead.
func TestSendReadsAtMostOneMiBOfTheAnswer(t *testing.T) {
	huge := `{"error": "` + strings.Repeat("x", 2<<20) + `"}`
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, huge)
	}))
	defer receiver.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "math.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Deps{
		Directories: func() map[string]string { return map[string]string{"quizzes": dir} },
		Endpoint:    func() (string, string, bool) { return receiver.URL + "/start", "room-a", true },
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	_, err := m.Send(t.Context(), "room-b", files(file("quizzes", "math.yaml")))
	if te := transferErr(t, err); te.Message != "Server 'room-b' refused the request: HTTP 400" {
		t.Errorf("%d %s", te.Status, fmt.Sprintf("%.80q", te.Message))
	}
}
