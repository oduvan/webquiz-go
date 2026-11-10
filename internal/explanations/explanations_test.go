package explanations

import (
	"reflect"
	"strings"
	"testing"
)

func render(markdown string) string {
	return Render(markdown, StudentImagesURL)
}

func mustContain(t *testing.T, html string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(html, part) {
			t.Errorf("%q does not contain %q", html, part)
		}
	}
}

func mustNotContain(t *testing.T, html string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if strings.Contains(html, part) {
			t.Errorf("%q contains %q", html, part)
		}
	}
}

// py: tests/test_explanations_unit.py::test_renders_bold_and_list
func TestRendersBoldAndList(t *testing.T) {
	mustContain(t, render("**b** text\n\n- one\n- two"), "<strong>b</strong>", "<li>one</li>")
}

// py: tests/test_explanations_unit.py::test_renders_table
func TestRendersTable(t *testing.T) {
	mustContain(t, render("| a | b |\n|---|---|\n| 1 | 2 |"), "<table>", "<td>1</td>")
}

// py: tests/test_explanations_unit.py::test_raw_html_is_shown_as_text
func TestRawHTMLIsShownAsText(t *testing.T) {
	html := render("<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>")
	mustNotContain(t, html, "<script>", "<img src=x")
	mustContain(t, html, "&lt;script&gt;", "&lt;img src=x onerror=alert(1)&gt;")
}

// py: tests/test_explanations_unit.py::test_javascript_link_is_plain_text
func TestJavascriptLinkIsPlainText(t *testing.T) {
	html := render("see [x](javascript:alert) now")
	mustNotContain(t, html, "<a")
	mustContain(t, html, "see x now")
}

// py: tests/test_explanations_unit.py::test_links_open_in_new_tab
func TestLinksOpenInNewTab(t *testing.T) {
	mustContain(t, render("[site](https://e.com)"), `href="https://e.com"`, `target="_blank"`, `rel="noopener"`)
}

// py: tests/test_explanations_unit.py::test_image_path_for_student_page
func TestImagePathForStudentPage(t *testing.T) {
	mustContain(t, render("![a](/imgs/a.png)"), `src="imgs/a.png"`)
}

// py: tests/test_explanations_unit.py::test_image_path_for_admin_preview
func TestImagePathForAdminPreview(t *testing.T) {
	mustContain(t, Render("![a](/imgs/a.png)", AdminImagesURL), `src="../imgs/a.png"`)
}

// py: tests/test_explanations_unit.py::test_image_name_with_space_in_angle_brackets
func TestImageNameWithSpaceInAngleBrackets(t *testing.T) {
	mustContain(t, render("![a](</imgs/my pic.png>)"), `src="imgs/my%20pic.png"`)
}

// py: tests/test_explanations_unit.py::test_external_image_url_unchanged
func TestExternalImageURLUnchanged(t *testing.T) {
	mustContain(t, render("![a](https://e.com/a.png)"), `src="https://e.com/a.png"`)
}

// py: tests/test_explanations_unit.py::test_has_explanation
func TestHasExplanation(t *testing.T) {
	if !Has(map[string]any{"explanation": "x"}) {
		t.Error(`Has({"explanation": "x"}) = false`)
	}
	for _, q := range []map[string]any{
		{},
		{"explanation": nil},
		{"explanation": ""},
		{"explanation": "  \n "},
		{"explanation": 5},
		{"explanation": []any{"x"}},
	} {
		if Has(q) {
			t.Errorf("Has(%#v) = true", q)
		}
	}
}

// py: tests/test_explanations_unit.py::test_explanation_image_names
func TestExplanationImageNames(t *testing.T) {
	text := "![a](/imgs/a.png) and ![b](/imgs/b.png \"title\")\n" +
		"![c](</imgs/my pic.png>) ![d](https://e.com/c.png) ![e](/imgs/a.png)"
	want := []string{"a.png", "b.png", "my pic.png"}
	if got := ImageNames(text); !reflect.DeepEqual(got, want) {
		t.Errorf("ImageNames = %q, want %q", got, want)
	}
}

// py: tests/test_explanations_unit.py::test_rewrite_explanation_images
func TestRewriteExplanationImages(t *testing.T) {
	text := "Look: ![a](/imgs/a.png) and ![c](</imgs/my pic.png>) and ![k](/imgs/keep.png)."
	got := RewriteImages(text, map[string]string{"a.png": "s_a.png", "my pic.png": "s_my pic.png"})
	want := "Look: ![a](/imgs/s_a.png) and ![c](</imgs/s_my pic.png>) and ![k](/imgs/keep.png)."
	if got != want {
		t.Errorf("RewriteImages =\n%q\nwant\n%q", got, want)
	}
}

// py: tests/test_explanations_unit.py::test_validate_explanations
func TestValidateExplanations(t *testing.T) {
	got := Validate(map[string]any{"show_explanation": "yes", "questions": []any{}})
	if want := []string{"'show_explanation' must be a boolean (true or false)"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Validate(show_explanation: yes) = %q, want %q", got, want)
	}
	got = Validate(map[string]any{"questions": []any{map[string]any{"explanation": 5}, map[string]any{"explanation": []any{"x"}}}})
	if want := []string{"Question 1 'explanation' must be text", "Question 2 'explanation' must be text"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Validate(bad explanations) = %q, want %q", got, want)
	}
	got = Validate(map[string]any{"show_explanation": true, "questions": []any{map[string]any{"explanation": "t"}, map[string]any{"explanation": nil}}})
	if len(got) != 0 {
		t.Errorf("Validate(good) = %q, want no errors", got)
	}
}

// py: tests/test_explanations_unit.py::test_image_name_with_parentheses_is_found
func TestImageNameWithParenthesesIsFound(t *testing.T) {
	text := "![a](/imgs/pic(1).png) and ![b](/imgs/plain.png)"
	if got, want := ImageNames(text), []string{"pic(1).png", "plain.png"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ImageNames = %q, want %q", got, want)
	}
}

// py: tests/test_explanations_unit.py::test_rewrite_to_a_name_that_needs_angle_brackets
func TestRewriteToANameThatNeedsAngleBrackets(t *testing.T) {
	text := "![a](/imgs/map.png) ![b](/imgs/pic(1).png)"
	result := RewriteImages(text, map[string]string{"map.png": "my map.png", "pic(1).png": "s_pic(1).png"})
	if want := "![a](</imgs/my map.png>) ![b](</imgs/s_pic(1).png>)"; result != want {
		t.Errorf("RewriteImages = %q, want %q", result, want)
	}
	if got, want := ImageNames(result), []string{"my map.png", "s_pic(1).png"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ImageNames = %q, want %q", got, want)
	}
	mustContain(t, render(result), `src="imgs/my%20map.png"`)
}

func TestImageNamesEdgeCases(t *testing.T) {
	cases := map[string][]string{
		"":                                   {},
		"no images here":                     {},
		"![a]( /imgs/spaced.png )":           {"spaced.png"},
		"![a](</imgs/ padded.png >)":         {"padded.png"},
		"![a](/imgs/x.png)![b](/imgs/x.png)": {"x.png"},
		"[link](/imgs/doc.pdf)":              {"doc.pdf"}, // the Python regex also finds links
		"![a](/images/a.png)":                {},
	}
	for text, want := range cases {
		if got := ImageNames(text); !reflect.DeepEqual(got, want) {
			t.Errorf("ImageNames(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestRewriteImagesKeepsOtherText(t *testing.T) {
	if got := RewriteImages("![a](/imgs/a.png)", nil); got != "![a](/imgs/a.png)" {
		t.Errorf("no renames: %q", got)
	}
	got := RewriteImages(`![a](/imgs/a.png "t") ![b]( /imgs/a.png)`, map[string]string{"a.png": "b c.png"})
	if want := `![a](</imgs/b c.png> "t") ![b]( </imgs/b c.png>)`; got != want {
		t.Errorf("RewriteImages = %q, want %q", got, want)
	}
}
