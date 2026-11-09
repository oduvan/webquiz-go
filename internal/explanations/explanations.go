package explanations

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// ImagesPath is how quiz files write the path of a quiz image.
const ImagesPath = "/imgs/"

// StudentImagesURL is where the quiz page (served at "/") finds quiz images.
const StudentImagesURL = "imgs/"

// AdminImagesURL is where the admin page (served at "/admin/") finds quiz images.
const AdminImagesURL = "../imgs/"

// pySpace is Python's \s for text patterns. Go's \s leaves out \v and the
// Unicode spaces that Python's re module counts.
const pySpace = `\t\n\v\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

var (
	// "](</imgs/name with spaces>)"
	angleImage = regexp.MustCompile(`(\]\([` + pySpace + `]*<` + regexp.QuoteMeta(ImagesPath) + `)([^>\n]+)(>)`)
	// "](/imgs/name)" with an optional title; a plain name may hold balanced
	// brackets, as in "pic(1).png"
	plainImage = regexp.MustCompile(`(\]\([` + pySpace + `]*` + regexp.QuoteMeta(ImagesPath) + `)((?:[^()` + pySpace + `>]|\([^()` + pySpace + `>]*\))+)()`)
	// Names with these characters must be written in angle brackets: "](</imgs/my map.png>)"
	needsAngleBrackets = regexp.MustCompile(`[` + pySpace + `()<>]`)
)

// Has tells whether a question has explanation text (not empty, not only spaces).
func Has(question map[string]any) bool {
	value, ok := question["explanation"].(string)
	return ok && pyval.Strip(value) != ""
}

// ImageNames returns the file names of the quiz images ("/imgs/...") that the
// explanation shows, in order and without duplicates.
func ImageNames(markdown string) []string {
	type found struct {
		start int
		name  string
	}
	var matches []found
	for _, pattern := range []*regexp.Regexp{angleImage, plainImage} {
		for _, m := range pattern.FindAllStringSubmatchIndex(markdown, -1) {
			matches = append(matches, found{m[0], pyval.Strip(markdown[m[4]:m[5]])})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].start != matches[j].start {
			return matches[i].start < matches[j].start
		}
		return matches[i].name < matches[j].name
	})
	names := []string{}
	for _, m := range matches {
		if m.name != "" && !contains(names, m.name) {
			names = append(names, m.name)
		}
	}
	return names
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// RewriteImages points image references to new file names ({old: new}),
// keeping their form. A plain reference whose new name needs angle brackets
// gets them.
func RewriteImages(markdown string, renamed map[string]string) string {
	for _, pattern := range []*regexp.Regexp{angleImage, plainImage} {
		markdown = replaceGroups(pattern, markdown, func(groups []string) string {
			name := pyval.Strip(groups[2])
			newName, ok := renamed[name]
			if !ok {
				return groups[0]
			}
			isPlain := groups[3] == ""
			if isPlain && needsAngleBrackets.MatchString(newName) {
				start := strings.TrimSuffix(groups[1], ImagesPath)
				return start + "<" + ImagesPath + newName + ">"
			}
			return groups[1] + newName + groups[3]
		})
	}
	return markdown
}

// replaceGroups is re.sub with a function: replace gets the whole match and
// its groups.
func replaceGroups(pattern *regexp.Regexp, text string, replace func(groups []string) string) string {
	var out strings.Builder
	last := 0
	for _, m := range pattern.FindAllStringSubmatchIndex(text, -1) {
		groups := make([]string, len(m)/2)
		for i := range groups {
			if m[2*i] >= 0 {
				groups[i] = text[m[2*i]:m[2*i+1]]
			}
		}
		out.WriteString(text[last:m[0]])
		out.WriteString(replace(groups))
		last = m[1]
	}
	out.WriteString(text[last:])
	return out.String()
}

// Validate checks show_explanation and every question's explanation before a
// quiz is saved. It returns error messages.
func Validate(data map[string]any) []string {
	var errors []string
	if value, ok := data["show_explanation"]; ok && !pyval.IsBool(value) {
		errors = append(errors, "'show_explanation' must be a boolean (true or false)")
	}
	questions, ok := pyval.List(data["questions"])
	if !ok {
		return errors
	}
	for index, question := range questions {
		q, isDict := pyval.Dict(question)
		if !isDict {
			continue
		}
		value, has := q["explanation"]
		if !has {
			continue
		}
		if value != nil && !pyval.IsStr(value) {
			errors = append(errors, fmt.Sprintf("Question %d 'explanation' must be text", index+1))
		}
	}
	return errors
}
