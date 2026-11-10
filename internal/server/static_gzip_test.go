package server_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

const immutableCache = "public, max-age=31536000, immutable"

// A script and a style that gzip makes much smaller, like the built files.
var (
	gzipTestScript = []byte(strings.Repeat("function show(a){return document.getElementById(a)}\n", 100))
	gzipTestStyle  = []byte(strings.Repeat("body{margin:0;padding:0;color:#222}\n", 60))
)

func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gunzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the body is not gzip: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("the gzip body is broken: %v", err)
	}
	return out
}

// startGzipBuildServer starts a server with a small web build like web/dist:
// a script and a style with gzip copies (name.gz), a script too small for
// gzip to help (no copy), and a file of a type Go does not know, with a copy.
// It returns the build too, so a test can compare answers with its files.
func startGzipBuildServer(t *testing.T) (*testutil.TestServer, fstest.MapFS) {
	t.Helper()
	build := fstest.MapFS{
		"index.html":            {Data: []byte(`<title>__WEBQUIZ_TITLE__</title><script id="webquiz-boot" type="application/json">__WEBQUIZ_BOOT__</script>`)},
		"assets/page-abc.js":    {Data: gzipTestScript},
		"assets/page-abc.js.gz": {Data: gzipped(t, gzipTestScript)},
		"assets/ui-abc.css":     {Data: gzipTestStyle},
		"assets/ui-abc.css.gz":  {Data: gzipped(t, gzipTestStyle)},
		"assets/tiny-abc.js":    {Data: []byte("x()")},
		"assets/notes.wqx":      {Data: gzipTestStyle},
		"assets/notes.wqx.gz":   {Data: gzipped(t, gzipTestStyle)},
	}
	return testutil.StartServer(t, testutil.Options{ServerOptions: server.Options{Pages: build}}), build
}

// rawClient sends requests as they are written. Go's client normally adds
// "Accept-Encoding: gzip" by itself and unpacks the answer; this one does
// neither, so a test sees exactly what came over the network.
func rawClient(t *testing.T, ts *testutil.TestServer) *testutil.Client {
	t.Helper()
	transport := ts.HTTP.Transport.(*http.Transport).Clone()
	transport.DisableCompression = true
	t.Cleanup(transport.CloseIdleConnections)
	return &testutil.Client{BaseURL: ts.URL, HTTP: &http.Client{Transport: transport}}
}

// acceptEncoding adds one Accept-Encoding line per value (none: no header).
func acceptEncoding(values ...string) testutil.RequestOption {
	return func(r *http.Request) {
		for _, value := range values {
			r.Header.Add("Accept-Encoding", value)
		}
	}
}

// A browser that accepts gzip gets the gzip copy of a built file, with
// Content-Encoding: gzip and the type of the file itself. Other clients get
// the file itself. Both answers have "Vary: Accept-Encoding", so a cache
// between the server and the students keeps the two apart.
func TestAssetsGzipFollowsAcceptEncoding(t *testing.T) {
	t.Parallel()
	ts, build := startGzipBuildServer(t)
	client := rawClient(t, ts)
	packed := build["assets/page-abc.js.gz"].Data
	jsType := mime.TypeByExtension(".js")

	cases := []struct {
		accept []string // the Accept-Encoding lines; none: no header
		gzip   bool
	}{
		{nil, false},
		{[]string{"gzip"}, true},
		{[]string{"gzip, deflate, br, zstd"}, true}, // Chrome
		{[]string{"gzip;q=0.5"}, true},
		{[]string{"br;q=1.0, gzip ; q=0.001"}, true},
		{[]string{"GZip"}, true},
		{[]string{"x-gzip"}, true},
		{[]string{"*"}, true},
		{[]string{"br", "gzip"}, true}, // two header lines
		{[]string{""}, false},
		{[]string{"identity"}, false},
		{[]string{"deflate, br"}, false},
		{[]string{"gzip;q=0"}, false},
		{[]string{"gzip; q=0.000"}, false},
		{[]string{"*;q=0"}, false},
		{[]string{"gzip;q=0, *"}, false},
	}
	for _, c := range cases {
		label := fmt.Sprintf("Accept-Encoding %q", c.accept)
		res := client.GetJSON(t, "/assets/page-abc.js", acceptEncoding(c.accept...))
		if res.Status != 200 {
			t.Errorf("%s: status %d", label, res.Status)
			continue
		}
		if got := res.Header.Get("Content-Type"); got != jsType {
			t.Errorf("%s: Content-Type %q, want %q", label, got, jsType)
		}
		if got := res.Header.Get("Cache-Control"); got != immutableCache {
			t.Errorf("%s: Cache-Control %q", label, got)
		}
		if !slices.Contains(res.Header.Values("Vary"), "Accept-Encoding") {
			t.Errorf("%s: Vary %q, want Accept-Encoding", label, res.Header.Values("Vary"))
		}
		want, encoding, file := gzipTestScript, "", "page-abc.js"
		if c.gzip {
			want, encoding, file = packed, "gzip", "page-abc.js.gz"
		}
		if got := res.Header.Get("Content-Encoding"); got != encoding {
			t.Errorf("%s: Content-Encoding %q, want %q", label, got, encoding)
		}
		if !bytes.Equal(res.Body, want) {
			t.Errorf("%s: got %d bytes, want the %d bytes of %s", label, len(res.Body), len(want), file)
		}
		if got := res.Header.Get("Content-Length"); got != strconv.Itoa(len(want)) {
			t.Errorf("%s: Content-Length %q, want %d", label, got, len(want))
		}
	}

	if res := client.GetJSON(t, "/assets/page-abc.js", acceptEncoding("gzip")); !bytes.Equal(gunzipped(t, res.Body), gzipTestScript) {
		t.Error("the gzip answer does not unpack to the script")
	}
	res := client.GetJSON(t, "/assets/ui-abc.css", acceptEncoding("gzip, deflate"))
	if res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get("Content-Type") != mime.TypeByExtension(".css") || !bytes.Equal(gunzipped(t, res.Body), gzipTestStyle) {
		t.Errorf("the style: %q %q, %d bytes", res.Header.Get("Content-Encoding"), res.Header.Get("Content-Type"), len(res.Body))
	}
}

// HEAD gives the headers of GET without the body: the gzip copy's length
// when gzip is accepted, the file's length when not.
func TestAssetsGzipHead(t *testing.T) {
	t.Parallel()
	ts, build := startGzipBuildServer(t)
	client := rawClient(t, ts)
	packed := build["assets/page-abc.js.gz"].Data

	res := client.Do(t, http.MethodHead, "/assets/page-abc.js", nil, acceptEncoding("gzip"))
	if res.Status != 200 || len(res.Body) != 0 {
		t.Fatalf("HEAD with gzip: %d, %d bytes of body", res.Status, len(res.Body))
	}
	if res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get("Content-Length") != strconv.Itoa(len(packed)) ||
		res.Header.Get("Content-Type") != mime.TypeByExtension(".js") || res.Header.Get("Cache-Control") != immutableCache ||
		!slices.Contains(res.Header.Values("Vary"), "Accept-Encoding") {
		t.Errorf("HEAD with gzip: headers %v, want the length %d of the gzip copy", res.Header, len(packed))
	}

	res = client.Do(t, http.MethodHead, "/assets/page-abc.js", nil)
	if res.Status != 200 || res.Header.Get("Content-Encoding") != "" || res.Header.Get("Content-Length") != strconv.Itoa(len(gzipTestScript)) {
		t.Errorf("HEAD without gzip: %d, headers %v, want the length %d of the file", res.Status, res.Header, len(gzipTestScript))
	}
}

// A built file without a gzip copy is sent as it is, even to a browser that
// accepts gzip. So is a file whose type Go cannot name from its extension:
// the answer would have to carry the type of the gzip data instead.
func TestAssetsWithoutUsableGzipCopy(t *testing.T) {
	t.Parallel()
	ts, _ := startGzipBuildServer(t)
	client := rawClient(t, ts)

	res := client.GetJSON(t, "/assets/tiny-abc.js", acceptEncoding("gzip"))
	if res.Status != 200 || res.Text() != "x()" || res.Header.Get("Content-Encoding") != "" {
		t.Errorf("no copy: %d %q, Content-Encoding %q", res.Status, res.Body, res.Header.Get("Content-Encoding"))
	}
	if res.Header.Get("Cache-Control") != immutableCache || res.Header.Get("Content-Type") != mime.TypeByExtension(".js") {
		t.Errorf("no copy: Cache-Control %q, Content-Type %q", res.Header.Get("Cache-Control"), res.Header.Get("Content-Type"))
	}

	res = client.GetJSON(t, "/assets/notes.wqx", acceptEncoding("gzip"))
	if res.Status != 200 || !bytes.Equal(res.Body, gzipTestStyle) || res.Header.Get("Content-Encoding") != "" {
		t.Errorf("unknown type: %d, %d bytes, Content-Encoding %q", res.Status, len(res.Body), res.Header.Get("Content-Encoding"))
	}
}

// A name that is not in the build still comes from static_dir, as it is:
// no gzip and no long caching (such a file can change under the same name).
func TestAssetsStaticDirFallbackStaysPlain(t *testing.T) {
	t.Parallel()
	ts, _ := startGzipBuildServer(t)
	client := rawClient(t, ts)
	writeFile(t, filepath.Join(ts.StaticDir, "assets", "legacy.js"), string(gzipTestScript))
	writeFile(t, filepath.Join(ts.StaticDir, "assets", "legacy.js.gz"), string(gzipped(t, gzipTestScript)))

	res := client.GetJSON(t, "/assets/legacy.js", acceptEncoding("gzip"))
	if res.Status != 200 || !bytes.Equal(res.Body, gzipTestScript) || res.Header.Get("Content-Encoding") != "" {
		t.Errorf("static_dir file: %d, %d bytes, Content-Encoding %q", res.Status, len(res.Body), res.Header.Get("Content-Encoding"))
	}
	if got := res.Header.Get("Cache-Control"); got != "" {
		t.Errorf("static_dir file: Cache-Control %q, want none", got)
	}
	if res := client.GetJSON(t, "/assets/missing.js", acceptEncoding("gzip")); res.Status != 404 {
		t.Errorf("a missing file: %d", res.Status)
	}
}

var quizPageAssets = regexp.MustCompile(`(?:src|href)="\./(assets/[^"]+)"`)

// With the real build, the scripts and styles of the quiz page come gzipped,
// and each unpacks to the file a client without gzip gets.
func TestAssetsGzipRealBuild(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	client := rawClient(t, ts)

	found := quizPageAssets.FindAllStringSubmatch(readIndex(t, ts), -1)
	if len(found) == 0 {
		t.Fatal("the quiz page names no assets")
	}
	plainBytes, sentBytes := 0, 0
	for _, m := range found {
		path := "/" + m[1]
		plain := client.GetJSON(t, path)
		packed := client.GetJSON(t, path, acceptEncoding("gzip, deflate, br, zstd"))
		if plain.Status != 200 || packed.Status != 200 || plain.Header.Get("Content-Encoding") != "" {
			t.Errorf("GET %s: %d and %d", path, plain.Status, packed.Status)
			continue
		}
		plainBytes += len(plain.Body)
		sentBytes += len(packed.Body)
		if packed.Header.Get("Content-Type") != plain.Header.Get("Content-Type") {
			t.Errorf("GET %s: Content-Type %q with gzip, %q without", path, packed.Header.Get("Content-Type"), plain.Header.Get("Content-Type"))
		}
		if packed.Header.Get("Content-Encoding") != "gzip" {
			if len(plain.Body) >= 1024 {
				t.Errorf("GET %s (%d bytes) did not come gzipped", path, len(plain.Body))
			}
			continue
		}
		if !bytes.Equal(gunzipped(t, packed.Body), plain.Body) {
			t.Errorf("GET %s: the gzip answer does not unpack to the file", path)
		}
	}
	t.Logf("the quiz page's scripts and styles: %d bytes, %d with gzip", plainBytes, sentBytes)
}
