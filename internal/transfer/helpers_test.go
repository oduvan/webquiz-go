package transfer

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// The test setup of tests/test_transfer.py: two (or more) managers act as
// two servers, each with its own folders, and a fake tunnel server routes
// "/start/{name}/api/transfer/..." to the manager with that socket name. It
// answers 502 for an unknown or offline name, as nginx does when the socket
// file is missing.

// machine is one WebQuiz server with its own folders (Machine in Python).
type machine struct {
	name    string
	tunnel  *fakeTunnel
	dirs    map[string]string
	manager *Manager

	mu         sync.Mutex
	online     bool // the tunnel server reaches it
	connected  bool // its own tunnel is connected (Endpoint)
	messages   []map[string]any
	filesInUse []string
}

func newMachine(t *testing.T, root, name string, tunnel *fakeTunnel) *machine {
	t.Helper()
	m := &machine{name: name, tunnel: tunnel, dirs: map[string]string{}, online: true, connected: true}
	for _, kind := range []string{"quizzes", "logs", "csv", "images", "attachments"} {
		m.dirs[kind] = filepath.Join(root, name, kind)
		if err := os.MkdirAll(m.dirs[kind], 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m.manager = New(Deps{
		Directories: func() map[string]string { return m.dirs },
		Endpoint: func() (string, string, bool) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if !m.connected {
				return "", "", false
			}
			return tunnel.baseURL, m.name, true
		},
		Notify: func(message map[string]any) {
			// Keep the message as JSON, like the admin page gets it.
			data, err := json.Marshal(message)
			if err != nil {
				t.Errorf("notify: %v", err)
				return
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			m.messages = append(m.messages, decodeJSONMap(t, data))
		},
		FilesInUse: func() []string {
			m.mu.Lock()
			defer m.mu.Unlock()
			return slices.Clone(m.filesInUse)
		},
		Logger: testLogger(t),
	})
	return m
}

// setOnline is Python's machine.online: the tunnel reaches the machine and
// its own tunnel is connected, or neither.
func (m *machine) setOnline(online bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.online, m.connected = online, online
}

func (m *machine) setConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = connected
}

func (m *machine) isOnline() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online
}

func (m *machine) setFilesInUse(paths ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filesInUse = paths
}

func (m *machine) lastMessage(t *testing.T) map[string]any {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		t.Fatalf("%s got no messages", m.name)
	}
	return m.messages[len(m.messages)-1]
}

func (m *machine) path(kind, name string) string { return filepath.Join(m.dirs[kind], name) }

func (m *machine) write(t *testing.T, kind, name, content string) {
	t.Helper()
	if err := os.WriteFile(m.path(kind, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (m *machine) read(t *testing.T, kind, name string) string {
	t.Helper()
	data, err := os.ReadFile(m.path(kind, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// files lists the file names of a folder, sorted.
func (m *machine) files(t *testing.T, kind string) []string {
	t.Helper()
	entries, err := os.ReadDir(m.dirs[kind])
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

// fakeTunnel is FakeTunnelServer: it routes requests to the managers.
type fakeTunnel struct {
	server  *httptest.Server
	baseURL string // "<server>/start"

	mu             sync.Mutex
	machines       map[string]*machine
	beforeDownload func() // runs before a download is served
	recording      bool
	log            []recorded
}

// recorded is one request the tunnel routed, with the answer.
type recorded struct {
	Method              string `json:"method"`
	Path                string `json:"path"`
	ContentType         any    `json:"content_type"`
	Body                any    `json:"body"`
	Status              int    `json:"status"`
	ResponseContentType any    `json:"response_content_type"`
	Response            any    `json:"response,omitempty"`
}

func newFakeTunnel(t *testing.T) *fakeTunnel {
	t.Helper()
	f := &fakeTunnel{machines: map[string]*machine{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /start/{name}/api/transfer/offer", f.route(func(m *Manager) handler { return m.HandleOffer }))
	mux.HandleFunc("GET /start/{name}/api/transfer/download/{token}/{type}/{filename}", f.route(func(m *Manager) handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			f.mu.Lock()
			hook := f.beforeDownload
			f.mu.Unlock()
			if hook != nil {
				hook()
			}
			return m.HandleDownload(w, r)
		}
	}))
	mux.HandleFunc("POST /start/{name}/api/transfer/result", f.route(func(m *Manager) handler { return m.HandleResult }))
	f.server = httptest.NewServer(f.record(mux))
	t.Cleanup(f.server.Close)
	f.baseURL = f.server.URL + "/start"
	return f
}

type handler func(w http.ResponseWriter, r *http.Request) error

// route sends a request to the manager of the machine named in the path. A
// handler error becomes 500 {"error": ...}, as in the server.
func (f *fakeTunnel) route(pick func(*Manager) handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		m := f.machines[r.PathValue("name")]
		f.mu.Unlock()
		if m == nil || !m.isOnline() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "502: Bad Gateway")
			return
		}
		if err := pick(m.manager)(w, r); err != nil {
			data, _ := json.Marshal(map[string]any{"error": err.Error()})
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(data)
		}
	}
}

// add makes a machine with this name (tunnel.add in Python).
func (f *fakeTunnel) add(t *testing.T, root, name string) *machine {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.machines[name]; ok {
		return m
	}
	m := newMachine(t, root, name, f)
	f.machines[name] = m
	return m
}

func (f *fakeTunnel) setBeforeDownload(hook func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beforeDownload = hook
}

// record keeps every request with its answer while recording is on.
func (f *fakeTunnel) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		capture := &captureWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		entry := recorded{
			Method:              r.Method,
			Path:                r.RequestURI,
			ContentType:         headerOrNil(r.Header, "Content-Type"),
			Body:                decodeRecorded(body),
			Status:              capture.status,
			ResponseContentType: headerOrNil(capture.Header(), "Content-Type"),
		}
		if capture.Header().Get("Content-Type") != "application/octet-stream" {
			entry.Response = decodeRecorded(capture.body.Bytes())
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.recording {
			f.log = append(f.log, entry)
		}
	})
}

// logSince returns the requests recorded after the first mark ones.
func (f *fakeTunnel) logSince(mark int) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log[mark:])
}

func (f *fakeTunnel) logLength() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.log)
}

func (f *fakeTunnel) startRecording() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recording = true
}

type captureWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *captureWriter) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.body.Write(p)
	return c.ResponseWriter.Write(p)
}

func headerOrNil(h http.Header, name string) any {
	if value := h.Get(name); value != "" {
		return value
	}
	return nil
}

// decodeRecorded is decode() of the Python script: {"json": value}, else
// {"text": text}, else nil for no body.
func decodeRecorded(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err == nil && !decoder.More() {
		return map[string]any{"json": value}
	}
	return map[string]any{"text": string(body)}
}

func decodeJSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return value
}

// asJSON turns any value into the generic JSON values a client would read
// (numbers as json.Number).
func asJSON(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

// twoMachines is the machines fixture: room-a and room-b behind one tunnel.
func twoMachines(t *testing.T) (*machine, *machine, *fakeTunnel) {
	t.Helper()
	tunnel := newFakeTunnel(t)
	root := t.TempDir()
	return tunnel.add(t, root, "room-a"), tunnel.add(t, root, "room-b"), tunnel
}

// sendFiles is send() of the Python tests: a offers files to b, and b has
// exactly one request.
func sendFiles(t *testing.T, sender, receiver *machine, files []any) RequestView {
	t.Helper()
	if _, err := sender.manager.Send(t.Context(), receiver.name, files); err != nil {
		t.Fatalf("send: %v", err)
	}
	requests := listIncoming(t, receiver)
	if len(requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(requests))
	}
	return requests[0]
}

func listIncoming(t *testing.T, m *machine) []RequestView {
	t.Helper()
	requests, err := m.manager.ListIncoming()
	if err != nil {
		t.Fatal(err)
	}
	return requests
}

func state(t *testing.T, m *machine) State {
	t.Helper()
	s, err := m.manager.State()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// file is {"type": kind, "name": name} as the admin page sends it.
func file(kind, name string) map[string]any { return map[string]any{"type": kind, "name": name} }

func files(items ...map[string]any) []any {
	list := make([]any, len(items))
	for i, item := range items {
		list[i] = item
	}
	return list
}

func planOf(t *testing.T, request RequestView, name string) FilePlan {
	t.Helper()
	for _, f := range request.Files {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no file %q in the request", name)
	return FilePlan{}
}

func transferErr(t *testing.T, err error) *Error {
	t.Helper()
	te, ok := err.(*Error)
	if !ok {
		t.Fatalf("got error %v (%T), want a *transfer.Error", err, err)
	}
	return te
}
