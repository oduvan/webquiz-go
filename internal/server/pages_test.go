package server

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

// fakePage is a built page like web/dist/*.html, with both markers.
const fakePage = `<!doctype html>
<html lang="uk">
  <head>
    <title>__WEBQUIZ_TITLE__</title>
    <script type="module" crossorigin src="./assets/page-abc.js"></script>
    <link rel="stylesheet" crossorigin href="./assets/ui-abc.css">
  </head>
  <body>
    <div id="root"></div>
    <script id="webquiz-boot" type="application/json">__WEBQUIZ_BOOT__</script>
  </body>
</html>
`

func fakePages() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte(fakePage)},
		"admin.html":           {Data: []byte(fakePage)},
		"assets/page-abc.js":   {Data: []byte("console.log(1)")},
		"assets/ui-abc.css":    {Data: []byte("body{}")},
		"assets/sub/other.txt": {Data: []byte("x")},
	}
}

// bootText returns the text inside <script id="webquiz-boot">.
func bootText(t *testing.T, page string) string {
	t.Helper()
	const open = `<script id="webquiz-boot" type="application/json">`
	start := strings.Index(page, open)
	if start < 0 {
		t.Fatalf("no boot script in:\n%s", page)
	}
	rest := page[start+len(open):]
	end := strings.Index(rest, "</script>")
	if end < 0 {
		t.Fatalf("the boot script is not closed:\n%s", page)
	}
	return rest[:end]
}

// A title or boot value with "</script>" must not end the script: the
// title is HTML-escaped like html.escape(quote=False), and in the boot JSON
// <, > and & are \u escapes (json_for_script wrote "<" as \u003c).
func TestRenderPageEscapesTitleAndBoot(t *testing.T) {
	title := `Quiz "one" & </script><b>two</b>`
	boot := map[string]any{
		"title": title,
		"items": []any{"</script>", "a & b", "<!--", "__WEBQUIZ_BOOT__", "__WEBQUIZ_TITLE__"},
	}
	page, err := renderPage(fakePages(), "index.html", title, boot, false)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)

	wantTitle := `<title>Quiz "one" &amp; &lt;/script&gt;&lt;b&gt;two&lt;/b&gt;</title>`
	if !strings.Contains(html, wantTitle) {
		t.Errorf("title not escaped like html.escape(quote=False):\n%s", html)
	}
	if strings.Count(html, "</script>") != 2 { // the module script and the boot script
		t.Errorf("the page has %d </script>, want 2:\n%s", strings.Count(html, "</script>"), html)
	}
	text := bootText(t, html)
	for _, raw := range []string{"<", ">", "&"} {
		if strings.Contains(text, raw) {
			t.Errorf("boot JSON contains a raw %q: %s", raw, text)
		}
	}
	for _, escaped := range []string{`\u003c/script\u003e`, `a \u0026 b`} {
		if !strings.Contains(text, escaped) {
			t.Errorf("boot JSON lacks %s: %s", escaped, text)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("boot is not JSON: %v\n%s", err, text)
	}
	if decoded["title"] != title {
		t.Errorf("boot title %q, want %q", decoded["title"], title)
	}
	items := decoded["items"].([]any)
	if items[3] != "__WEBQUIZ_BOOT__" || items[4] != "__WEBQUIZ_TITLE__" {
		t.Errorf("marker text inside the boot data was changed: %v", items)
	}
}

// The quiz page keeps ./assets/ (it also works under the tunnel prefix);
// admin pages load /assets/ because they live at /admin/, /files/, ...
func TestRenderPageAssetURLs(t *testing.T) {
	relative, err := renderPage(fakePages(), "index.html", "T", struct{}{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(relative), `src="./assets/page-abc.js"`) || !strings.Contains(string(relative), `href="./assets/ui-abc.css"`) {
		t.Errorf("quiz page must keep relative asset URLs:\n%s", relative)
	}
	absolute, err := renderPage(fakePages(), "admin.html", `src="./assets/x"`, map[string]any{"v": `src="./assets/y"`}, true)
	if err != nil {
		t.Fatal(err)
	}
	html := string(absolute)
	if !strings.Contains(html, `src="/assets/page-abc.js"`) || !strings.Contains(html, `href="/assets/ui-abc.css"`) {
		t.Errorf("admin page must load /assets/:\n%s", html)
	}
	if !strings.Contains(html, `<title>src="./assets/x"</title>`) {
		t.Errorf("the title text must not be rewritten:\n%s", html)
	}
}

// A page missing from the build gives the template error page (Python's
// templates/template_error.html) and an error for the log.
func TestRenderPageMissingEntryGivesTemplateError(t *testing.T) {
	page, err := renderPage(fakePages(), "files.html", "T", struct{}{}, true)
	if err == nil {
		t.Fatal("want an error for a missing page")
	}
	if !strings.Contains(string(page), "Template Error") || !strings.Contains(string(page), "Unable to load the full interface template") {
		t.Errorf("want the template error page, got:\n%s", page)
	}

	broken := fstest.MapFS{"index.html": {Data: []byte("<html><title>x</title></html>")}}
	page, err = renderPage(broken, "index.html", "T", struct{}{}, false)
	if err == nil || !strings.Contains(string(page), "Template Error") {
		t.Errorf("a page without the markers must give the template error page, got %v:\n%s", err, page)
	}
}

func TestEscapeHTMLTextLikePython(t *testing.T) {
	cases := map[string]string{
		`a & b`:          `a &amp; b`,
		`<tag attr="x">`: `&lt;tag attr="x"&gt;`,
		`it's`:           `it's`,
	}
	for in, want := range cases {
		if got := escapeHTMLText(in); got != want {
			t.Errorf("escapeHTMLText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := escapeHTMLAttr(`<a href="x">it's</a>`); got != `&lt;a href=&quot;x&quot;&gt;it&#x27;s&lt;/a&gt;` {
		t.Errorf("escapeHTMLAttr = %q", got)
	}
}

func TestRegistrationFieldName(t *testing.T) {
	cases := map[string]string{
		"Grade":          "grade",
		"School Name":    "school_name",
		"Клас Навчання":  "клас_навчання",
		"Group <class>":  "group_<class>",
		"  two  spaces ": "__two__spaces_",
	}
	for label, want := range cases {
		if got := registrationFieldName(label); got != want {
			t.Errorf("registrationFieldName(%q) = %q, want %q", label, got, want)
		}
	}
}
