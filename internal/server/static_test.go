package server_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
	"github.com/oduvan/webquiz-go/internal/version"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// /imgs/ serves quizzes/imgs with a folder listing (aiohttp add_static with
// show_index=True); the listing looks like aiohttp's.
func TestImagesFolder(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	imgs := filepath.Join(ts.QuizzesDir, "imgs")
	writeFile(t, filepath.Join(imgs, "a.png"), "png data")
	writeFile(t, filepath.Join(imgs, "b c.png"), "x")
	writeFile(t, filepath.Join(imgs, "sub", "d.png"), "y")

	wantRoot := "<html>\n<head>\n<title>Index of /.</title>\n</head>\n<body>\n<h1>Index of /.</h1>\n<ul>\n" +
		`<li><a href="/imgs/a.png">a.png</a></li>` + "\n" +
		`<li><a href="/imgs/b%20c.png">b c.png</a></li>` + "\n" +
		`<li><a href="/imgs/sub">sub/</a></li>` + "\n</ul>\n</body>\n</html>"
	for _, path := range []string{"/imgs/", "/imgs"} {
		res := ts.GetJSON(t, path)
		if res.Status != 200 || res.Text() != wantRoot || res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Errorf("GET %s: %d %q\n%s", path, res.Status, res.Header.Get("Content-Type"), res.Body)
		}
	}
	wantSub := "<html>\n<head>\n<title>Index of /sub</title>\n</head>\n<body>\n<h1>Index of /sub</h1>\n<ul>\n" +
		`<li><a href="/imgs/sub/d.png">d.png</a></li>` + "\n</ul>\n</body>\n</html>"
	for _, path := range []string{"/imgs/sub", "/imgs/sub/"} {
		if res := ts.GetJSON(t, path); res.Status != 200 || res.Text() != wantSub {
			t.Errorf("GET %s: %d\n%s", path, res.Status, res.Body)
		}
	}
	if res := ts.GetJSON(t, "/imgs/a.png"); res.Status != 200 || res.Text() != "png data" || res.Header.Get("Content-Type") != "image/png" {
		t.Errorf("GET /imgs/a.png: %d %q %q", res.Status, res.Header.Get("Content-Type"), res.Body)
	}
	if res := ts.GetJSON(t, "/imgs/b%20c.png"); res.Status != 200 || res.Text() != "x" {
		t.Errorf("a name with a space: %d", res.Status)
	}
	for _, path := range []string{"/imgs/none.png", "/imgs/..%2fwebquiz.yaml", "/imgs/sub/none"} {
		if res := ts.GetJSON(t, path); res.Status != 404 {
			t.Errorf("GET %s: %d, want 404", path, res.Status)
		}
	}
	if res := ts.Do(t, "POST", "/imgs/a.png", nil); res.Status != 405 {
		t.Errorf("POST /imgs/a.png: %d, want 405", res.Status)
	}
}

// Other paths come from static_dir, like add_static("/", static_dir): files
// are served, folders are 403 (no listing), missing files are 404.
func TestStaticFolderFallback(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	writeFile(t, filepath.Join(ts.StaticDir, "page.html"), "<p>hi</p>")
	writeFile(t, filepath.Join(ts.StaticDir, "sub", "x.txt"), "text")

	if res := ts.GetJSON(t, "/page.html"); res.Status != 200 || res.Text() != "<p>hi</p>" {
		t.Errorf("GET /page.html: %d %s", res.Status, res.Body)
	}
	if res := ts.GetJSON(t, "/sub/x.txt"); res.Status != 200 || res.Text() != "text" {
		t.Errorf("GET /sub/x.txt: %d %s", res.Status, res.Body)
	}
	if res := ts.GetJSON(t, "/index.html"); res.Status != 200 || !strings.Contains(res.Text(), "webquiz-boot") {
		t.Errorf("GET /index.html: %d", res.Status)
	}
	for _, path := range []string{"/sub", "/sub/"} {
		if res := ts.GetJSON(t, path); res.Status != 403 || res.Text() != "403: Forbidden" {
			t.Errorf("GET %s: %d %s, want 403", path, res.Status, res.Body)
		}
	}
	for _, path := range []string{"/missing.js", "/../webquiz.yaml", "/%2e%2e/webquiz.yaml", "/sub/../../webquiz.yaml"} {
		res := ts.GetJSON(t, path)
		if res.Status != 404 || strings.Contains(res.Text(), "master_key") {
			t.Errorf("GET %s: %d %s, want 404", path, res.Status, res.Body)
		}
	}
	if err := os.Symlink(filepath.Join(ts.Dir, "webquiz.yaml"), filepath.Join(ts.StaticDir, "link.yaml")); err == nil {
		if res := ts.GetJSON(t, "/link.yaml"); res.Status != 404 {
			t.Errorf("a link out of static_dir: %d %s, want 404", res.Status, res.Body)
		}
	}
}

var assetPattern = regexp.MustCompile(`(?:src|href)="(\.?/assets/[^"]+)"`)

// The built pages' files are served at /assets/ for the quiz page (./assets/
// at "/") and for the admin pages (/assets/ after the rewrite).
func TestAssetsAreServed(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")

	quizAssets := assetPattern.FindAllStringSubmatch(readIndex(t, ts), -1)
	if len(quizAssets) == 0 {
		t.Fatal("the quiz page names no assets")
	}
	for _, m := range quizAssets {
		if !strings.HasPrefix(m[1], "./assets/") {
			t.Errorf("quiz page asset %s must be relative", m[1])
		}
		res := ts.GetJSON(t, strings.TrimPrefix(m[1], "."))
		if res.Status != 200 || len(res.Body) == 0 {
			t.Errorf("GET %s: %d", m[1], res.Status)
		}
		if res.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s: Cache-Control %q", m[1], res.Header.Get("Cache-Control"))
		}
		if strings.HasSuffix(m[1], ".js") && !strings.HasPrefix(res.Header.Get("Content-Type"), "text/javascript") {
			t.Errorf("GET %s: Content-Type %q", m[1], res.Header.Get("Content-Type"))
		}
	}

	for _, page := range []string{"/admin/", "/files/", "/live-stats/", "/files/csv/answers.csv"} {
		res := admin.GetJSON(t, page)
		if res.Status != 200 || res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Errorf("GET %s: %d %q", page, res.Status, res.Header.Get("Content-Type"))
			continue
		}
		found := assetPattern.FindAllStringSubmatch(res.Text(), -1)
		if len(found) == 0 {
			t.Errorf("%s names no assets", page)
		}
		for _, m := range found {
			if !strings.HasPrefix(m[1], "/assets/") {
				t.Errorf("%s: asset %s must start with /assets/", page, m[1])
			} else if res := ts.GetJSON(t, m[1]); res.Status != 200 {
				t.Errorf("%s: GET %s: %d", page, m[1], res.Status)
			}
		}
	}

	if res := ts.GetJSON(t, "/assets/missing.js"); res.Status != 404 {
		t.Errorf("a missing asset: %d", res.Status)
	}
	if res := ts.GetJSON(t, "/assets/"); res.Status == 200 {
		t.Errorf("/assets/ must not list the files: %s", res.Body)
	}
}

// Each page gets its own boot data and title.
func TestAdminPagesBootData(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{
		"quizzes": []any{map[string]any{
			"name": "Private", "download_path": "https://example.com/a.zip", "folder": "a/", "auth_header_token": "secret-token",
		}},
		"admin": map[string]any{"trusted_ips": []any{"127.0.0.1"}},
	}})

	res := ts.GetJSON(t, "/admin/")
	html := res.Text()
	if !strings.Contains(html, "<title>Панель Адміністратора WebQuiz</title>") || strings.Contains(html, "secret-token") {
		t.Errorf("admin page:\n%s", html)
	}
	boot := pageBoot(t, html)
	if boot["is_trusted_ip"] != true || boot["tunnel_public_key"] != "" {
		t.Errorf("admin boot: %s", jsonText(boot))
	}
	if got := jsonText(boot["downloadable_quizzes"]); got != `[{"download_path":"https://example.com/a.zip","folder":"a/","name":"Private"}]` {
		t.Errorf("downloadable_quizzes %s", got)
	}
	if got := jsonText(boot["network_info"]); !strings.HasPrefix(got, `{"urls":[`) {
		t.Errorf("network_info %s", got)
	}
	if boot["repo_url"] != version.RepoURL || boot["pages_url"] != version.PagesURL {
		t.Errorf("admin boot URLs: %v %v", boot["repo_url"], boot["pages_url"])
	}
	if v, _ := boot["version"].(string); v == "" {
		t.Error("admin boot has no version")
	}
	if pageBoot(t, ts.GetJSON(t, "/admin/", testutil.FromIP("192.168.1.9")).Text())["is_trusted_ip"] != false {
		t.Error("192.168.1.9 is not trusted")
	}

	files := ts.GetJSON(t, "/files/").Text()
	if !strings.Contains(files, "<title>WebQuiz File Manager</title>") {
		t.Errorf("files page title:\n%s", files)
	}
	boot = pageBoot(t, files)
	config, _ := os.ReadFile(ts.ConfigPath)
	if boot["config_content"] != string(config) || boot["config_path"] != ts.ConfigPath || boot["is_trusted_ip"] != true {
		t.Errorf("files boot: %s", jsonText(boot))
	}
	if got := jsonText(boot["config_data"]); got != `{"extra_answers_with_users_csv":false,"language":"uk","registration":{}}` {
		t.Errorf("config_data %s", got)
	}

	if live := ts.GetJSON(t, "/live-stats/").Text(); !strings.Contains(live, "<title>WebQuiz Live Stats</title>") || jsonText(pageBoot(t, live)) != "{}" {
		t.Errorf("live stats page:\n%s", live)
	}
	if table := ts.GetJSON(t, "/files/csv/x.csv").Text(); !strings.Contains(table, "<title>CSV Table - WebQuiz</title>") || jsonText(pageBoot(t, table)) != "{}" {
		t.Errorf("CSV table page:\n%s", table)
	}
}

// The files page reads the config file each time: its form data follows the
// file (CONFIG_DATA), and a file that is not YAML gives null form data.
func TestFilesPageConfigData(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	write := func(text string) map[string]any {
		t.Helper()
		writeFile(t, ts.ConfigPath, text)
		return pageBoot(t, ts.GetJSON(t, "/files/").Text()) // a local page: no login needed
	}

	boot := write("language: en\nextra_answers_with_users_csv: yes\nregistration:\n  approve: true\n  fields: [Grade]\n")
	if got := jsonText(boot["config_data"]); got != `{"extra_answers_with_users_csv":true,"language":"en","registration":{"approve":true,"fields":["Grade"]}}` {
		t.Errorf("config_data %s", got)
	}
	boot = write("registration: just text\n")
	if got := jsonText(boot["config_data"]); got != `{"extra_answers_with_users_csv":false,"language":"uk","registration":{}}` {
		t.Errorf("registration that is not a mapping: %s", got)
	}
	boot = write("")
	if got := jsonText(boot["config_data"]); got != `{"extra_answers_with_users_csv":false,"language":"uk","registration":{}}` {
		t.Errorf("empty file: %s", got)
	}
	boot = write("key: [unclosed\n")
	if boot["config_content"] != "key: [unclosed\n" || boot["config_data"] != nil {
		t.Errorf("broken YAML: %s", jsonText(boot))
	}
}
