package explanations

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// mistuneCase is one entry of testdata/mistune_cases.json, written by
// testdata/gen_mistune_cases.py with the renderer of WebQuiz 1.19.
type mistuneCase struct {
	Markdown  string `json:"markdown"`
	ImagesURL string `json:"images_url"`
	HTML      string `json:"html"`
}

// lineBreaksNextToTags finds white space with a line break next to a tag.
// mistune and goldmark place these line breaks differently ("<li><p>" against
// "<li>\n<p>", "  <td>" against "<td>"); the page shows them the same way.
var lineBreaksNextToTags = regexp.MustCompile(`>[ \t]*\n\s*|\s*\n[ \t]*<`)

func normalize(html string) string {
	html = lineBreaksNextToTags.ReplaceAllStringFunc(html, func(s string) string {
		if strings.HasPrefix(s, ">") {
			return ">"
		}
		return "<"
	})
	return strings.TrimSpace(html)
}

// knownDifferences are inputs where the Go renderer differs from mistune on
// purpose, with the reason.
var knownDifferences = map[string]string{
	// mistune drops the table when a row has fewer cells than the header;
	// goldmark follows GitHub and fills the row with empty cells.
	// (No case in the fixture needs this entry yet; it documents the rule.)
}

func TestRenderMatchesMistune(t *testing.T) {
	data, err := os.ReadFile("testdata/mistune_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []mistuneCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 100 {
		t.Fatalf("expected the full case list, got %d cases", len(cases))
	}
	for _, c := range cases {
		if reason, ok := knownDifferences[c.Markdown]; ok {
			t.Logf("skip %q: %s", c.Markdown, reason)
			continue
		}
		got := Render(c.Markdown, c.ImagesURL)
		if normalize(got) != normalize(c.HTML) {
			t.Errorf("Render(%q, %q)\n got %q\nwant %q", c.Markdown, c.ImagesURL, got, c.HTML)
		}
	}
}

func TestRenderIsSafeForConcurrentUse(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			url := StudentImagesURL
			if i%2 == 1 {
				url = AdminImagesURL
			}
			html := Render("**x** ![a](/imgs/a.png)", url)
			if !strings.Contains(html, `src="`+url+`a.png"`) {
				t.Errorf("Render with %q gave %q", url, html)
			}
		}(i)
	}
	wg.Wait()
}

func TestRenderOtherImagesURL(t *testing.T) {
	mustContain(t, Render("![a](/imgs/a.png)", "/static/imgs/"), `src="/static/imgs/a.png"`)
}
