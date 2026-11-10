package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of tests/test_transfer_api.py: a real server without a tunnel,
// access rules and errors. The full flow between two managers is in
// internal/transfer; TestTransferBetweenTwoServers runs it through two
// whole servers.

var transferFiles = []any{map[string]any{"type": "quizzes", "name": "default.yaml"}}

// py: tests/test_transfer_api.py::test_transfer_admin_endpoints_require_session
func TestTransferAdminEndpointsRequireSession(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	base := "/api/admin/transfer"
	for _, res := range []*testutil.Response{
		ts.GetJSON(t, base),
		ts.PostJSON(t, base+"/send", map[string]any{"to": "room-b", "files": transferFiles}),
		ts.PostJSON(t, base+"/abc/accept", nil),
		ts.PostJSON(t, base+"/abc/reject", nil),
	} {
		if res.Status != 401 {
			t.Errorf("%d %s", res.Status, res.Body)
		}
	}
}

// py: tests/test_transfer_api.py::test_transfer_admin_endpoints_blocked_from_public_ip
func TestTransferAdminEndpointsBlockedFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/admin/transfer", testutil.WithCookies(ts.AdminCookies(t, "test123")...), testutil.FromIP("8.8.8.8"))
	if res.Status != 403 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_transfer_api.py::test_transfer_state_without_tunnel
func TestTransferStateWithoutTunnel(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.AdminClient(t, "test123").GetJSON(t, "/api/admin/transfer")
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := jsonText(res.JSON(t)); got != `{"available":false,"incoming":[],"name":null,"outgoing":[]}` {
		t.Errorf("state %s", got)
	}
}

// py: tests/test_transfer_api.py::test_send_without_tunnel
func TestSendWithoutTunnel(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.AdminClient(t, "test123").PostJSON(t, "/api/admin/transfer/send", map[string]any{"to": "room-b", "files": transferFiles})
	if res.Status != 409 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if text, _ := res.JSON(t)["error"].(string); !strings.Contains(text, "not connected") {
		t.Errorf("error %q", text)
	}
}

// py: tests/test_transfer_api.py::test_send_invalid_json
func TestSendInvalidJSON(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	if res := admin.PostJSON(t, "/api/admin/transfer/send", "not json"); res.Status != 400 {
		t.Errorf("not json: %d %s", res.Status, res.Body)
	}
	if res := admin.PostJSON(t, "/api/admin/transfer/send", []any{"list"}); res.Status != 400 {
		t.Errorf("a list: %d %s", res.Status, res.Body)
	}
}

// py: tests/test_transfer_api.py::test_accept_and_reject_unknown_request
func TestAcceptAndRejectUnknownRequest(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	base := "/api/admin/transfer"
	choices := map[string]any{"files": []any{map[string]any{"type": "quizzes", "name": "a.yaml", "action": "skip"}}}
	for _, c := range []struct {
		res  *testutil.Response
		want int
	}{
		{admin.PostJSON(t, base+"/unknown/accept", nil), 404},
		{admin.PostJSON(t, base+"/unknown/accept", choices), 404},
		{admin.PostJSON(t, base+"/unknown/accept", "not json"), 400},
		{admin.PostJSON(t, base+"/unknown/reject", nil), 404},
	} {
		if c.res.Status != c.want {
			t.Errorf("%d, want %d: %s", c.res.Status, c.want, c.res.Body)
		}
	}
}

// py: tests/test_transfer_api.py::test_public_endpoints_work_from_public_ip
func TestPublicEndpointsWorkFromPublicIP(t *testing.T) {
	t.Parallel()
	// Other servers call these through the tunnel: the token protects them,
	// not the address.
	ts := testutil.StartServer(t, testutil.Options{})
	public := testutil.FromIP("8.8.8.8")
	base := "/api/transfer"
	token := strings.Repeat("t", 43)

	// No tunnel on this server, so it cannot receive offers
	if res := ts.PostJSON(t, base+"/offer", map[string]any{"from": "room-a", "token": token, "files": transferFiles}, public); res.Status != 409 {
		t.Errorf("offer: %d %s", res.Status, res.Body)
	}
	// Unknown token: nothing is served
	if res := ts.GetJSON(t, base+"/download/"+token+"/quizzes/default.yaml", public); res.Status != 404 {
		t.Errorf("download: %d %s", res.Status, res.Body)
	}
	if res := ts.PostJSON(t, base+"/result", map[string]any{"token": token, "status": "accepted"}, public); res.Status != 404 {
		t.Errorf("result: %d %s", res.Status, res.Body)
	}
	if res := ts.PostJSON(t, base+"/offer", "not json", public); res.Status != 400 {
		t.Errorf("offer, not json: %d %s", res.Status, res.Body)
	}
}

// py: tests/test_transfer_api.py::test_send_panel_hidden_until_tunnel_connected
func TestSendPanelHiddenUntilTunnelConnected(t *testing.T) {
	t.Parallel()
	// The page shows the send panel only after GET /api/admin/transfer says
	// it is available. The panel is part of the React files page (Task 16);
	// the server serves the page and says "not available" without a tunnel.
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := testutil.WithCookies(ts.AdminCookies(t, "test123")...)
	if res := ts.GetJSON(t, "/files/", cookies); res.Status != 200 {
		t.Errorf("files page: %d", res.Status)
	}
	if state := ts.GetJSON(t, "/api/admin/transfer", cookies).JSON(t); state["available"] != false {
		t.Errorf("state %v", state)
	}
}

// ----- Two whole servers -----

// tunnelServer is a fake tunnel server for whole test servers: it passes
// "/start/<name>/..." to the server with that socket name, from a public
// address, and answers 502 for an unknown name.
type tunnelServer struct {
	URL     string // "<server>/start"
	mu      sync.Mutex
	servers map[string]*httputil.ReverseProxy
}

func newTunnelServer(t *testing.T) *tunnelServer {
	t.Helper()
	tunnel := &tunnelServer{servers: map[string]*httputil.ReverseProxy{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.EscapedPath(), "/start/")
		name, path, _ := strings.Cut(rest, "/")
		tunnel.mu.Lock()
		proxy := tunnel.servers[name]
		tunnel.mu.Unlock()
		if !ok || proxy == nil {
			http.Error(w, "502: Bad Gateway", http.StatusBadGateway)
			return
		}
		r.URL.RawPath = "/" + path
		r.URL.Path, _ = url.PathUnescape(r.URL.RawPath)
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	tunnel.URL = server.URL + "/start"
	return tunnel
}

// connect makes ts reachable as name, as if its tunnel were connected.
func (tunnel *tunnelServer) connect(t *testing.T, ts *testutil.TestServer, name string) {
	t.Helper()
	target, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		// nginx of the tunnel server: the address of the calling server
		r.Out.Header.Set("X-Forwarded-For", "8.8.8.8")
	}}
	tunnel.mu.Lock()
	tunnel.servers[name] = proxy
	tunnel.mu.Unlock()
	ts.Server.SetTransferEndpoint(tunnel.URL, name, true)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Two servers on one (fake) tunnel server send files to each other through
// the admin API, as two classroom computers do: the sender offers a quiz
// with its image and a log, the receiver's admin page hears about the
// request, sees which files the receiver's running server uses, accepts,
// and the sender's admin page hears the result.
func TestTransferBetweenTwoServers(t *testing.T) {
	t.Parallel()
	quiz := "title: Shared\nquestions:\n- question: Where?\n  image: /imgs/map.png\n  options: [a, b]\n  correct_answer: 0\n"
	a := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"test_quiz.yaml": quiz}})
	b := testutil.StartServer(t, testutil.Options{})
	tunnel := newTunnelServer(t)
	tunnel.connect(t, a, "room-a")
	tunnel.connect(t, b, "room-b")
	writeTestFile(t, filepath.Join(a.QuizzesDir, "imgs", "map.png"), "the map")
	logName := filepath.Base(a.LogFile())
	adminA, adminB := a.AdminClient(t, "test123"), b.AdminClient(t, "test123")
	socketA, socketB := openSocket(t, a, "/ws/admin"), openSocket(t, b, "/ws/admin")
	if initial := socketB.next(); initial["type"] != "initial_state" || jsonText(initial["transfer_requests"]) != "[]" {
		t.Fatalf("receiver's initial state %v", initial)
	}
	socketA.next()

	state := adminA.GetJSON(t, "/api/admin/transfer").JSON(t)
	if state["available"] != true || state["name"] != "room-a" {
		t.Fatalf("sender's state %v", state)
	}
	res := adminA.PostJSON(t, "/api/admin/transfer/send", map[string]any{"to": "room-b", "files": []any{
		map[string]any{"type": "quizzes", "name": "test_quiz.yaml"},
		map[string]any{"type": "logs", "name": logName},
	}})
	if res.Status != 200 {
		t.Fatalf("send: %d %s", res.Status, res.Body)
	}
	sent := res.JSON(t)
	offer := sent["offer"].(map[string]any)
	if sent["success"] != true || offer["to"] != "room-b" || offer["status"] != "waiting" || len(offer["files"].([]any)) != 3 {
		t.Fatalf("send reply %v", sent)
	}

	message := socketB.next()
	request, _ := message["request"].(map[string]any)
	if message["type"] != "transfer_request" || request["from"] != "room-a" {
		t.Fatalf("receiver's message %v", message)
	}
	cases := map[string]string{}
	for _, f := range request["files"].([]any) {
		f := f.(map[string]any)
		cases[f["name"].(string)] = f["case"].(string) + " " + f["target"].(string)
	}
	// test_quiz.yaml is the receiver's running quiz: it cannot be replaced.
	want := map[string]string{"test_quiz.yaml": "in_use test_quiz.yaml", logName: "new room-a_" + logName, "map.png": "new map.png"}
	if jsonText(cases) != jsonText(want) {
		t.Errorf("cases %v, want %v", cases, want)
	}
	incoming := adminB.GetJSON(t, "/api/admin/transfer").JSON(t)["incoming"].([]any)
	if len(incoming) != 1 || incoming[0].(map[string]any)["id"] != request["id"] {
		t.Fatalf("receiver's incoming %v", incoming)
	}

	res = adminB.PostJSON(t, "/api/admin/transfer/"+request["id"].(string)+"/accept", nil)
	if res.Status != 200 {
		t.Fatalf("accept: %d %s", res.Status, res.Body)
	}
	result := res.JSON(t)
	if result["success"] != true || result["from"] != "room-a" || len(result["saved"].([]any)) != 3 || jsonText(result["failed"]) != "[]" {
		t.Fatalf("accept reply %v", result)
	}
	if got := readTestFile(t, filepath.Join(b.QuizzesDir, "room-a_test_quiz.yaml")); got != quiz {
		t.Errorf("received quiz %q", got)
	}
	if got := readTestFile(t, filepath.Join(b.QuizzesDir, "imgs", "map.png")); got != "the map" {
		t.Errorf("received image %q", got)
	}
	if got := readTestFile(t, filepath.Join(b.LogsDir, "room-a_"+logName)); !strings.Contains(got, "Server Started") {
		t.Errorf("received log %q", got)
	}
	if message := socketB.next(); message["type"] != "transfer_request_removed" || message["id"] != request["id"] {
		t.Errorf("receiver's message %v", message)
	}
	if message := socketA.next(); message["type"] != "transfer_result" || message["offer"].(map[string]any)["status"] != "accepted" {
		t.Errorf("sender's message %v", message)
	}
	outgoing := adminA.GetJSON(t, "/api/admin/transfer").JSON(t)["outgoing"].([]any)
	if len(outgoing) != 1 || outgoing[0].(map[string]any)["status"] != "accepted" {
		t.Errorf("sender's outgoing %v", outgoing)
	}
}

// An admin page that connects gets the waiting transfer requests in its
// initial state.
func TestAdminSocketInitialStateHasTransferRequests(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	tunnel := newTunnelServer(t)
	tunnel.connect(t, ts, "room-b")
	offer := map[string]any{"from": "room-a", "token": strings.Repeat("t", 43), "files": []any{map[string]any{"type": "logs", "name": "a.log", "size": 3}}}
	if res := ts.PostJSON(t, "/api/transfer/offer", offer, testutil.FromIP("8.8.8.8")); res.Status != 202 {
		t.Fatalf("offer: %d %s", res.Status, res.Body)
	}
	initial := openSocket(t, ts, "/ws/admin").next()
	requests, _ := initial["transfer_requests"].([]any)
	if len(requests) != 1 {
		t.Fatalf("initial state %v", initial)
	}
	file := requests[0].(map[string]any)["files"].([]any)[0].(map[string]any)
	if file["target"] != "room-a_a.log" || file["case"] != "new" || file["size"] != float64(3) {
		t.Errorf("request file %v", file)
	}
}

// An admin page that connects while offers arrive sees every request: in
// its initial state or as a transfer_request message, never neither.
func TestAdminSocketMissesNoTransferRequest(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	tunnel := newTunnelServer(t)
	tunnel.connect(t, ts, "room-b")
	const offers, pages = 15, 15

	type page struct {
		conn *websocket.Conn
		err  error
	}
	results := make([]page, pages)
	var wg sync.WaitGroup
	for i := range pages {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			results[i].conn, _, results[i].err = websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/admin", nil)
		})
	}
	for i := range offers {
		wg.Go(func() {
			offer := map[string]any{"from": fmt.Sprintf("room-%d", i), "token": strings.Repeat("t", 43), "files": []any{map[string]any{"type": "logs", "name": "a.log"}}}
			res := ts.PostJSON(t, "/api/transfer/offer", offer)
			if res.Status != 202 {
				t.Errorf("offer %d: %d %s", i, res.Status, res.Body)
			}
		})
	}
	wg.Wait()

	for i, p := range results {
		if p.err != nil {
			t.Fatalf("page %d: %v", i, p.err)
		}
		defer p.conn.CloseNow()
		seen := map[string]bool{}
		for len(seen) < offers {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, data, err := p.conn.Read(ctx)
			cancel()
			if err != nil {
				t.Fatalf("page %d saw %d of %d requests: %v", i, len(seen), offers, err)
			}
			var message map[string]any
			if err := json.Unmarshal(data, &message); err != nil {
				t.Fatal(err)
			}
			switch message["type"] {
			case "initial_state":
				for _, r := range message["transfer_requests"].([]any) {
					seen[r.(map[string]any)["id"].(string)] = true
				}
			case "transfer_request":
				seen[message["request"].(map[string]any)["id"].(string)] = true
			}
		}
	}
}
