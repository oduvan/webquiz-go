package transfer

import (
	"net/http"
	"strings"
	"testing"
)

// The public routes read their body like aiohttp's request.json(): a body
// that is not UTF-8, is too big or is not one JSON value is "Invalid JSON".
func TestPublicRoutesRefuseBadBodies(t *testing.T) {
	_, _, tunnel := twoMachines(t)
	token := strings.Repeat("t", 43)
	valid := `{"from": "room-a", "token": "` + token + `", "files": [{"type": "logs", "name": "a.log"}]}`
	bodies := map[string]string{
		"not UTF-8":  `{"from": "room-a", "token": "` + token + `", "files": [{"type": "logs", "name": "a\xff.log"}]}`,
		"too big":    valid + strings.Repeat(" ", 1<<20),
		"two values": valid + valid,
		"empty":      "",
	}
	for name, body := range bodies {
		for _, path := range []string{"/room-b/api/transfer/offer", "/room-a/api/transfer/result"} {
			response, err := http.Post(tunnel.baseURL+path, "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			data := readAll(t, response)
			if response.StatusCode != 400 || data != `{"error":"Invalid JSON"}` {
				t.Errorf("%s to %s: %d %s", name, path, response.StatusCode, data)
			}
		}
	}
	response, err := http.Post(tunnel.baseURL+"/room-b/api/transfer/offer", "application/json", strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if data := readAll(t, response); response.StatusCode != 202 || data != `{"success":true}` {
		t.Errorf("valid offer: %d %s", response.StatusCode, data)
	}
}

func readAll(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := response.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String()
		}
	}
}

// pyQuote is Python's urllib.parse.quote with the default safe "/".
func TestPyQuote(t *testing.T) {
	for text, want := range map[string]string{
		"math.yaml":     "math.yaml",
		"my map.png":    "my%20map.png",
		"нотатки 1.pdf": "%D0%BD%D0%BE%D1%82%D0%B0%D1%82%D0%BA%D0%B8%201.pdf",
		"a+b&c=d'e(f)":  "a%2Bb%26c%3Dd%27e%28f%29",
		"~_.-/":         "~_.-/",
		"#?%":           "%23%3F%25",
	} {
		if got := pyQuote(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}

// splitExt is os.path.splitext for a file name.
func TestSplitExt(t *testing.T) {
	for name, want := range map[string][2]string{
		"room-a_math.yaml": {"room-a_math", ".yaml"},
		"room-a_README":    {"room-a_README", ""},
		"a.tar.gz":         {"a.tar", ".gz"},
		".bashrc":          {".bashrc", ""},
		"..x":              {"..x", ""},
		"a.":               {"a", "."},
	} {
		stem, extension := splitExt(name)
		if stem != want[0] || extension != want[1] {
			t.Errorf("%q: %q %q, want %q %q", name, stem, extension, want[0], want[1])
		}
	}
}
