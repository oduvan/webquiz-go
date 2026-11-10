package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// MergeYAML puts a partial update into the text of a config file, like the
// {"data": ...} form of PUT /api/admin/config, and returns the new text.
//
// Each key of partial replaces the value of that top-level key. When both the
// update and the file have a mapping there (a section such as
// "registration"), the update's keys replace or join the section's keys one
// by one and the other keys of the section stay. Keys are handled in
// alphabetical order; new keys go after the last key of their section, and
// new top-level keys at the end of the file (before a "..." line that ends
// the document), after any new keys of the last section.
//
// Only the lines of changed values are rewritten. Comments, blank lines,
// quotes and spacing elsewhere stay as they were; a changed value keeps the
// comment on its line and the quote style of the old text. The values of
// partial are what a JSON body holds (decoded with UseNumber); they are
// written so that the file reads back with PyYAML's rules as the same values
// (text like "yes" gets quotes, 1.0 stays a float, text with line ends goes
// in double quotes on one line, as ruamel.yaml wrote it).
//
// As in the Python version, a file whose text is not a mapping (a list, plain
// text, broken YAML) is replaced by the update. Unlike Python (ruamel.yaml
// refused repeated keys and dropped the whole file), a repeated key is
// handled like PyYAML reads it: the last one counts and is the one changed.
//
// The config save checks the result with CheckMerge before it writes it.
// One known shape fails that check: a new value for a key whose old value
// has an anchor ("&name") that an alias elsewhere uses. The anchor goes away
// with the old value, so the save is refused and the file stays as it was.
func MergeYAML(original []byte, partial map[string]any) ([]byte, error) {
	text := string(original)
	// A byte order mark (Notepad writes one) stays, but the lines are edited
	// without it: yaml.v3 does not count it in its columns.
	bom := strings.HasPrefix(text, byteOrderMark)
	text = strings.TrimPrefix(text, byteOrderMark)
	crlf := strings.Contains(text, "\r\n")
	if crlf {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	keys := sortedKeys(partial)

	root, status := parseRoot(text)
	var out string
	var err error
	switch status {
	case rootMissing: // empty, broken or not a mapping: start from an empty mapping
		if pyval.Strip(text) != "" && !onlyComments(text) {
			text = ""
		}
		out, err = appendEntries(text, keys, partial)
	case rootFlow:
		out, err = mergeFlowDocument(root, keys, partial)
	default:
		e := newEditor(text)
		if !strings.HasSuffix(text, "\n") {
			e.stripFinalLineEnd(root)
		}
		out, err = e.merge(root, keys, partial)
	}
	if err != nil {
		return nil, err
	}
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if bom {
		out = byteOrderMark + out
	}
	return []byte(out), nil
}

const byteOrderMark = "\xef\xbb\xbf"

// CheckMerge tells whether merged, the text that MergeYAML made from
// original and partial, is safe to write. It must read back (with PyYAML's
// rules) as exactly the values of original with the update in them, merged
// the way 1.19 merged them, and nothing else may change. The config save
// runs it before it writes the file, so a mistake of the line editor can
// never break the config or change a setting that nobody sent. The error
// tells what is wrong.
//
// When the old text cannot be read with PyYAML's rules (an unknown tag, for
// example), there is nothing to compare with. New text that does not read
// back either is left to the caller's own YAML check, which reports it as
// 1.19 did; new text that reads back must hold the update's values.
func CheckMerge(original []byte, partial map[string]any, merged []byte) error {
	old, known := mergeBase(original)
	value, err := pyval.LoadYAML(merged)
	if err != nil {
		if !known {
			return nil
		}
		return fmt.Errorf("the new text does not read back: %w", err)
	}
	got, ok := value.(map[string]any)
	if !ok {
		return errors.New("the new text is not a mapping")
	}
	if !known {
		return checkUpdateIn(got, partial)
	}
	return compareConfigs(got, mergeUpdate(old, partial))
}

// mergeBase gives the values that a partial update goes into: the values of
// the file, or an empty mapping for a file that is empty, broken or not a
// mapping (MergeYAML starts again from nothing, as 1.19 did). known is false
// for a mapping that cannot be read with PyYAML's rules.
func mergeBase(original []byte) (values map[string]any, known bool) {
	value, err := pyval.LoadYAML(original)
	if err == nil {
		if m, ok := value.(map[string]any); ok {
			return m, true
		}
		return map[string]any{}, true
	}
	text := strings.ReplaceAll(strings.TrimPrefix(string(original), byteOrderMark), "\r\n", "\n")
	if _, status := parseRoot(text); status == rootMissing {
		return map[string]any{}, true
	}
	return nil, false
}

// mergeUpdate puts the update into values the way 1.19 did: a dict goes into
// a dict section key by key, any other value replaces the old one. values
// changes in place, so a section shared through a YAML alias changes for
// every key that uses it, as in Python.
func mergeUpdate(values, partial map[string]any) map[string]any {
	for _, key := range sortedKeys(partial) {
		updates, isDict := partial[key].(map[string]any)
		section, isSection := values[key].(map[string]any)
		if isDict && isSection {
			for k, v := range updates {
				section[k] = v
			}
			continue
		}
		values[key] = partial[key]
	}
	return values
}

// checkUpdateIn checks that each value of the update is in got: a section
// update key by key, any other value as a whole.
func checkUpdateIn(got, partial map[string]any) error {
	for _, key := range sortedKeys(partial) {
		updates, isDict := partial[key].(map[string]any)
		section, isSection := got[key].(map[string]any)
		if !isDict || !isSection {
			if err := compareValue(key, key, got, partial); err != nil {
				return err
			}
			continue
		}
		for _, k := range sortedKeys(updates) {
			if err := compareValue(key+"."+k, k, section, updates); err != nil {
				return err
			}
		}
	}
	return nil
}

// compareConfigs finds the first value that differs between got and want,
// looking one level into sections, so the error names the setting.
func compareConfigs(got, want map[string]any) error {
	for _, key := range unionKeys(got, want) {
		gotSection, isGotSection := got[key].(map[string]any)
		wantSection, isWantSection := want[key].(map[string]any)
		if isGotSection && isWantSection {
			for _, k := range unionKeys(gotSection, wantSection) {
				if err := compareValue(key+"."+k, k, gotSection, wantSection); err != nil {
					return err
				}
			}
			continue
		}
		if err := compareValue(key, key, got, want); err != nil {
			return err
		}
	}
	return nil
}

// compareValue compares the values of key in got and want; name is the
// setting's name for the error ("registration.approve").
func compareValue(name, key string, got, want map[string]any) error {
	gotValue, inGot := got[key]
	wantValue, inWant := want[key]
	switch {
	case !inGot && inWant:
		return fmt.Errorf("%s would be missing", name)
	case inGot && !inWant:
		return fmt.Errorf("%s would be added", name)
	case !sameValue(gotValue, wantValue):
		return fmt.Errorf("%s would read back as %s, not %s", name, pyval.Repr(gotValue), pyval.Repr(wantValue))
	}
	return nil
}

// unionKeys gives the keys of a and b, sorted.
func unionKeys(a, b map[string]any) []string {
	keys := sortedKeys(a)
	for key := range b {
		if _, ok := a[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// sameValue tells whether two values are the same, with the same types all
// the way down: Python's == except that True is not 1 and 1 is not 1.0, and
// NaN is the same as NaN.
func sameValue(a, b any) bool {
	kind := pyval.KindOf(a)
	if kind != pyval.KindOf(b) {
		return false
	}
	switch kind {
	case pyval.KindFloat:
		x, _ := pyval.ToFloat(a)
		y, _ := pyval.ToFloat(b)
		if math.IsNaN(x) && math.IsNaN(y) {
			return true
		}
	case pyval.KindList:
		listA, _ := pyval.List(a)
		listB, _ := pyval.List(b)
		if len(listA) != len(listB) {
			return false
		}
		for i := range listA {
			if !sameValue(listA[i], listB[i]) {
				return false
			}
		}
		return true
	case pyval.KindDict:
		dictA, _ := pyval.Dict(a)
		dictB, _ := pyval.Dict(b)
		if len(dictA) != len(dictB) {
			return false
		}
		for key, value := range dictA {
			other, ok := dictB[key]
			if !ok || !sameValue(value, other) {
				return false
			}
		}
		return true
	}
	return pyval.Equal(a, b)
}

type rootStatus int

const (
	rootMissing rootStatus = iota
	rootFlow
	rootBlock
)

// parseRoot reads the top-level mapping of a single-document file.
func parseRoot(text string) (*yaml.Node, rootStatus) {
	decoder := yaml.NewDecoder(strings.NewReader(text))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, rootMissing
	}
	var another yaml.Node
	if err := decoder.Decode(&another); !errors.Is(err, io.EOF) {
		return nil, rootMissing // a second document, or broken text after the first
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, rootMissing
	}
	root := document.Content[0]
	if root.Style&yaml.FlowStyle != 0 {
		return root, rootFlow
	}
	return root, rootBlock
}

// onlyComments tells whether the text has nothing but comments and blank lines.
func onlyComments(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if isContentLine(line) {
			return false
		}
	}
	return true
}

// isContentLine tells whether a line holds YAML content, not only white space
// or a comment.
func isContentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed != "" && !strings.HasPrefix(trimmed, "#")
}

// isDocumentMarker tells whether a line starts ("---") or ends ("...") a
// YAML document: the marker at the line start, alone or before a space.
func isDocumentMarker(line string) bool {
	for _, marker := range []string{"---", "..."} {
		if rest, ok := strings.CutPrefix(line, marker); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// appendEntries adds "key: value" entries at the end of text.
func appendEntries(text string, keys []string, partial map[string]any) (string, error) {
	var b strings.Builder
	b.WriteString(text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
	for _, key := range keys {
		lines, err := encodeEntry(key, partial[key], 0)
		if err != nil {
			return "", err
		}
		for _, line := range lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// editor changes some lines of a file and copies the others as they are.
type editor struct {
	lines []string // the original lines, without line ends
	edits []edit
}

// edit replaces lines[start:end] with newLines; start == end inserts them
// before line start. indent is the indentation of inserted keys.
type edit struct {
	start, end int
	indent     int
	newLines   []string
}

func newEditor(text string) *editor {
	return &editor{lines: strings.Split(strings.TrimSuffix(text, "\n"), "\n")}
}

// entry is one key of a block mapping with the lines it takes.
type entry struct {
	key, value *yaml.Node
	start      int // the line of the key (0-based)
	end        int // the line after the entry's last line (see entries)
}

// entries lists the keys of a block mapping; limit is the line where the
// mapping ends at the latest. An entry ends after its last content line, or
// after the text of a block scalar that ends it (blockScalarEnd).
func (e *editor) entries(m *yaml.Node, limit int) []entry {
	list := make([]entry, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		list = append(list, entry{key: m.Content[i], value: m.Content[i+1], start: m.Content[i].Line - 1})
	}
	for i := range list {
		next := limit
		if i+1 < len(list) {
			next = list[i+1].start
		}
		// Comments and blank lines after the entry belong to what follows,
		// and so does a line that ends the document ("...").
		list[i].end = list[i].start + 1
		for line := list[i].start + 1; line < next && line < len(e.lines); line++ {
			if isDocumentMarker(e.lines[line]) {
				break
			}
			if isContentLine(e.lines[line]) {
				list[i].end = line + 1
			}
		}
		if end := e.blockScalarEnd(list[i].value, next); end > list[i].end {
			list[i].end = end
		}
	}
	return list
}

// lastNode is the node that a value ends with: the value itself, or the
// last item of a list or mapping, all the way down.
func lastNode(value *yaml.Node) *yaml.Node {
	for (value.Kind == yaml.MappingNode || value.Kind == yaml.SequenceNode) && len(value.Content) > 0 {
		value = value.Content[len(value.Content)-1]
	}
	return value
}

// isBlockScalar tells whether a node is a block scalar ("|" or ">").
func isBlockScalar(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0
}

// blockScalarEnd is the line after the end of a value that ends with a block
// scalar, or 0 for other values. The text of a block scalar is every line
// indented like its first line, also a line that starts with "#" (checker
// code often ends with a comment of its own). When the scalar keeps its
// final line ends ("|+"), the blank lines after its text belong to it too.
func (e *editor) blockScalarEnd(value *yaml.Node, limit int) int {
	last := lastNode(value)
	if !isBlockScalar(last) {
		return 0
	}
	limit = min(limit, len(e.lines))
	end := last.Line // the line after the "|" or ">"
	if strings.Trim(last.Value, "\n") != "" {
		indent := -1
		for line := last.Line; line < limit; line++ {
			text := e.lines[line]
			if strings.TrimSpace(text) == "" {
				continue
			}
			lineIndent := len(text) - len(strings.TrimLeft(text, " "))
			if indent < 0 {
				indent = lineIndent
			}
			if lineIndent < indent {
				break
			}
			end = line + 1
		}
	}
	// Each kept blank line adds a line end after the one that ends the text.
	blank := len(last.Value) - len(strings.TrimRight(last.Value, "\n")) - 1
	for ; blank > 0 && end < limit && strings.TrimSpace(e.lines[end]) == ""; blank-- {
		end++
	}
	return end
}

// stripFinalLineEnd handles a file that does not end with a line end and
// whose last value is a block scalar: then the scalar's text has no final
// line end either. The new text always ends with one, so the scalar gets the
// "strip" indicator ("|-"), which keeps its text as it was.
func (e *editor) stripFinalLineEnd(root *yaml.Node) {
	last := lastNode(root)
	if !isBlockScalar(last) || last.Value == "" || strings.HasSuffix(last.Value, "\n") {
		return
	}
	line := e.lines[last.Line-1]
	start, ok := byteOffset(line, last.Column-1)
	if !ok || start >= len(line) || (line[start] != '|' && line[start] != '>') {
		return
	}
	// After "|" or ">" come the chomping ("+" or "-") and the indentation
	// (a digit), in any order.
	end := start + 1
	indentation := ""
	for ; end < len(line) && strings.IndexByte("+-123456789", line[end]) >= 0; end++ {
		switch line[end] {
		case '-':
			return // strip already
		case '+':
		default:
			indentation = string(line[end])
		}
	}
	e.lines[last.Line-1] = line[:start+1] + indentation + "-" + line[end:]
}

// lastEntry finds the last entry with the key (the one PyYAML reads).
func lastEntry(entries []entry, key string) *entry {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].key.Kind == yaml.ScalarNode && entries[i].key.Value == key {
			return &entries[i]
		}
	}
	return nil
}

func (e *editor) merge(root *yaml.Node, keys []string, partial map[string]any) (string, error) {
	topLevel := e.entries(root, len(e.lines))
	// Top-level keys usually start at the line start, but a file may indent
	// all of them.
	indent := 0
	if len(topLevel) > 0 {
		indent = topLevel[0].key.Column - 1
	}
	for _, key := range keys {
		value := partial[key]
		ent := lastEntry(topLevel, key)
		updates, isDict := value.(map[string]any)
		var err error
		switch {
		case ent == nil:
			err = e.insertEntry(e.appendLine(topLevel), key, value, indent)
		case isDict && ent.value.Kind == yaml.MappingNode && ent.value.Style&yaml.FlowStyle == 0:
			err = e.mergeSection(*ent, updates)
		case isDict && ent.value.Kind == yaml.MappingNode:
			merged, mergeErr := mergeMappingNode(ent.value, updates)
			if mergeErr != nil {
				return "", mergeErr
			}
			err = e.replaceEntry(*ent, indent, merged)
		default:
			err = e.replaceValue(*ent, indent, value)
		}
		if err != nil {
			return "", err
		}
	}
	return e.result()
}

// mergeSection changes the keys of a block mapping section one by one.
func (e *editor) mergeSection(section entry, updates map[string]any) error {
	children := e.entries(section.value, section.end)
	indent := 2
	if len(children) > 0 {
		indent = children[0].key.Column - 1
	}
	insertAt := section.end
	for _, key := range sortedKeys(updates) {
		child := lastEntry(children, key)
		var err error
		if child == nil {
			err = e.insertEntry(insertAt, key, updates[key], indent)
		} else {
			err = e.replaceValue(*child, indent, updates[key])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// appendLine is where new top-level keys go: the end of the file, or before
// a document marker ("...") that follows the mapping.
func (e *editor) appendLine(topLevel []entry) int {
	from := 0
	if len(topLevel) > 0 {
		from = topLevel[len(topLevel)-1].end
	}
	for line := from; line < len(e.lines); line++ {
		if isDocumentMarker(e.lines[line]) {
			return line
		}
	}
	return len(e.lines)
}

func (e *editor) insertEntry(at int, key string, value any, indent int) error {
	lines, err := encodeEntry(key, value, indent)
	if err != nil {
		return err
	}
	e.edits = append(e.edits, edit{start: at, end: at, indent: indent, newLines: lines})
	return nil
}

// replaceValue gives an existing key a new value. A one-line value is
// replaced inside its line, so the rest of the line (a comment) stays.
func (e *editor) replaceValue(ent entry, indent int, value any) error {
	if line, ok := e.inlineReplacement(ent, value); ok {
		e.edits = append(e.edits, edit{start: ent.start, end: ent.start + 1, newLines: []string{line}})
		return nil
	}
	node, err := valueNode(value)
	if err != nil {
		return err
	}
	return e.replaceEntry(ent, indent, node)
}

// replaceEntry rewrites all lines of an entry with a new value, keeping the
// comment of the key line.
func (e *editor) replaceEntry(ent entry, indent int, value *yaml.Node) error {
	lines, err := encodeNodeEntry(ent.key.Value, value, indent)
	if err != nil {
		return err
	}
	if column, comment, ok := e.keyLineComment(ent); ok {
		lines[0] = withComment(lines[0], column, comment)
	}
	e.edits = append(e.edits, edit{start: ent.start, end: ent.end, newLines: lines})
	return nil
}

// inlineReplacement gives the key's line with the value replaced, when both
// the old and the new value are one-line scalars.
func (e *editor) inlineReplacement(ent entry, value any) (string, bool) {
	old := ent.value
	if old.Kind != yaml.ScalarNode || old.Line != ent.key.Line || ent.end != ent.start+1 {
		return "", false
	}
	if old.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0 || old.Anchor != "" {
		return "", false
	}
	line := e.lines[ent.start]
	start, end, ok := scalarSpan(line, old)
	if !ok {
		return "", false
	}
	rest := line[end:]
	if isContentLine(rest) {
		return "", false // something else follows the value
	}
	newText, ok := inlineText(value, old.Style)
	if !ok {
		return "", false
	}
	return line[:start] + newText + keepCommentColumn(rest, utf8.RuneCountInString(line[start:end]), utf8.RuneCountInString(newText)), true
}

// keyLineComment finds the comment on an entry's key line: after a one-line
// value, or after "key:" when the value starts on the next line. For a flow
// list or mapping ("[a, b]", "{a: 1}") it is the comment after the value, on
// the entry's last line; for a block scalar, the comment after its "|" or
// ">". column is where the comment starts, in characters.
func (e *editor) keyLineComment(ent entry) (column int, comment string, ok bool) {
	// yaml.v3 keeps these two comments in the value's LineComment.
	switch {
	case ent.value.Kind != yaml.ScalarNode && ent.value.Style&yaml.FlowStyle != 0:
		return e.commentAtLineEnd(ent.end-1, ent.value.LineComment)
	case isBlockScalar(ent.value):
		return e.commentAtLineEnd(ent.value.Line-1, ent.value.LineComment)
	}
	line := e.lines[ent.start]
	// "key:" with nothing after it has an empty null value on the key line.
	empty := ent.value.Kind == yaml.ScalarNode && ent.value.Style == 0 && ent.value.Value == ""
	var after int
	switch {
	case ent.value.Line == ent.key.Line && ent.value.Kind == yaml.ScalarNode && !empty:
		_, end, found := scalarSpan(line, ent.value)
		if !found {
			return 0, "", false
		}
		after = end
	case ent.value.Line > ent.key.Line || empty:
		_, keyEnd, found := scalarSpan(line, ent.key)
		if !found {
			return 0, "", false
		}
		colon := strings.IndexByte(line[keyEnd:], ':')
		if colon < 0 || strings.TrimSpace(line[keyEnd:keyEnd+colon]) != "" {
			return 0, "", false
		}
		after = keyEnd + colon + 1
	default:
		return 0, "", false
	}
	rest := line[after:]
	trimmed := strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	return utf8.RuneCountInString(line[:len(line)-len(trimmed)]), trimmed, true
}

// commentAtLineEnd finds a comment that ends a line, and where it starts (in
// characters).
func (e *editor) commentAtLineEnd(line int, comment string) (column int, found string, ok bool) {
	text := strings.TrimRight(e.lines[line], " \t")
	if comment == "" || !strings.HasSuffix(text, comment) {
		return 0, "", false
	}
	return utf8.RuneCountInString(text[:len(text)-len(comment)]), comment, true
}

// withComment adds a comment to a line, at the given column when there is room.
func withComment(line string, column int, comment string) string {
	pad := column - utf8.RuneCountInString(line)
	if pad < 1 {
		pad = 1
	}
	return line + strings.Repeat(" ", pad) + comment
}

// keepCommentColumn keeps a comment after a changed value at its column.
func keepCommentColumn(rest string, oldLength, newLength int) string {
	trimmed := strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return rest
	}
	gap := oldLength + (len(rest) - len(trimmed)) - newLength
	if gap < 1 {
		gap = 1
	}
	return strings.Repeat(" ", gap) + trimmed
}

// scalarSpan finds the bytes of a one-line scalar on its line.
func scalarSpan(line string, n *yaml.Node) (start, end int, ok bool) {
	start, ok = byteOffset(line, n.Column-1)
	if !ok || start >= len(line) && n.Value != "" {
		return 0, 0, false
	}
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		if !strings.HasPrefix(line[start:], `"`) {
			return 0, 0, false
		}
		for i := start + 1; i < len(line); i++ {
			switch line[i] {
			case '\\':
				i++
			case '"':
				return start, i + 1, true
			}
		}
	case n.Style&yaml.SingleQuotedStyle != 0:
		if !strings.HasPrefix(line[start:], "'") {
			return 0, 0, false
		}
		for i := start + 1; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				return start, i + 1, true
			}
		}
	case n.Style == 0 && n.Value != "":
		if strings.HasPrefix(line[start:], n.Value) {
			return start, start + len(n.Value), true
		}
	}
	return 0, 0, false
}

// byteOffset turns a column in characters into a byte offset.
func byteOffset(line string, column int) (int, bool) {
	if column < 0 {
		return 0, false
	}
	offset := 0
	for i := 0; i < column; i++ {
		if offset >= len(line) {
			return 0, false
		}
		_, size := utf8.DecodeRuneInString(line[offset:])
		offset += size
	}
	return offset, true
}

// inlineText writes a value as one line of YAML, or ok is false when it
// needs more lines. Text keeps the quote style of the value it replaces, as
// ruamel's preserve_quotes did.
func inlineText(value any, oldStyle yaml.Style) (string, bool) {
	node, err := valueNode(value)
	if err != nil {
		return "", false
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && !strings.Contains(node.Value, "\n") {
		switch {
		case oldStyle&yaml.DoubleQuotedStyle != 0:
			node.Style = yaml.DoubleQuotedStyle
		case oldStyle&yaml.SingleQuotedStyle != 0:
			node.Style = yaml.SingleQuotedStyle
		}
	}
	if node.Kind != yaml.ScalarNode && len(node.Content) > 0 {
		return "", false
	}
	out, err := encodeNode(node)
	if err != nil {
		return "", false
	}
	text := strings.TrimSuffix(out, "\n")
	if strings.Contains(text, "\n") || text == "" {
		return "", false
	}
	return text, true
}

// encodeEntry writes "key: value" as lines with the given indent.
func encodeEntry(key string, value any, indent int) ([]string, error) {
	node, err := valueNode(value)
	if err != nil {
		return nil, err
	}
	return encodeNodeEntry(key, node, indent)
}

func encodeNodeEntry(key string, value *yaml.Node, indent int) ([]string, error) {
	keyNode := &yaml.Node{}
	if err := keyNode.Encode(key); err != nil {
		return nil, err
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{keyNode, value}}
	out, err := encodeNode(mapping)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	prefix := strings.Repeat(" ", indent)
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return lines, nil
}

func encodeNode(node *yaml.Node) (string, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// valueNode turns a JSON value into a YAML node that PyYAML reads back as
// the same value.
func valueNode(value any) (*yaml.Node, error) {
	converted, err := yamlValue(value)
	if err != nil {
		return nil, err
	}
	node := &yaml.Node{}
	if err := node.Encode(converted); err != nil {
		return nil, err
	}
	quoteMultiline(node)
	return node, nil
}

// quoteMultiline writes text with line ends in double quotes on one line
// ("a\nb"), as ruamel.yaml did. yaml.v3 would write a block scalar ("|"),
// and the comment lines after it could become part of its text.
func quoteMultiline(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" && strings.Contains(n.Value, "\n") {
		n.Style = yaml.DoubleQuotedStyle
	}
	for _, child := range n.Content {
		quoteMultiline(child)
	}
}

// yamlValue prepares a JSON value for the YAML encoder: numbers keep their
// Python type (1 stays an int, 1.0 a float).
func yamlValue(value any) (any, error) {
	switch x := value.(type) {
	case nil, bool, string:
		return x, nil
	case json.Number:
		if strings.ContainsAny(string(x), ".eE") {
			f, ok := pyval.Number(x)
			if !ok {
				return nil, fmt.Errorf("bad number %s", x)
			}
			return pythonFloat(f), nil
		}
		i, ok := new(big.Int).SetString(string(x), 10)
		if !ok {
			return nil, fmt.Errorf("bad number %s", x)
		}
		return pythonInt{i}, nil
	case float32:
		return pythonFloat(x), nil
	case float64:
		return pythonFloat(x), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return x, nil
	case []any:
		items := make([]any, len(x))
		for i, item := range x {
			converted, err := yamlValue(item)
			if err != nil {
				return nil, err
			}
			items[i] = converted
		}
		return items, nil
	case map[string]any:
		m := make(map[string]any, len(x))
		for key, item := range x {
			converted, err := yamlValue(item)
			if err != nil {
				return nil, err
			}
			m[key] = converted
		}
		return m, nil
	}
	return nil, fmt.Errorf("cannot write a %T value to the config", value)
}

// pythonFloat is written like PyYAML writes a float: "1.0", "0.5", "1.0e+20".
type pythonFloat float64

func (f pythonFloat) MarshalYAML() (any, error) {
	value := float64(f)
	var text string
	switch {
	case math.IsNaN(value):
		text = ".nan"
	case math.IsInf(value, 1):
		text = ".inf"
	case math.IsInf(value, -1):
		text = "-.inf"
	default:
		text = pyval.FloatRepr(value)
		if !strings.Contains(text, ".") && strings.Contains(text, "e") {
			text = strings.Replace(text, "e", ".0e", 1)
		}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: text}, nil
}

// pythonInt is a whole number of any size.
type pythonInt struct{ value *big.Int }

func (i pythonInt) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: i.value.String()}, nil
}

// mergeMappingNode gives a copy of a mapping node with the updates in it:
// keys that exist get the new value, other keys are added at the end.
func mergeMappingNode(m *yaml.Node, updates map[string]any) (*yaml.Node, error) {
	merged := *m
	merged.Style &^= yaml.FlowStyle // written as a block, like the rest of the file
	merged.Content = append([]*yaml.Node(nil), m.Content...)
	for _, key := range sortedKeys(updates) {
		node, err := valueNode(updates[key])
		if err != nil {
			return nil, err
		}
		found := false
		for i := len(merged.Content) - 2; i >= 0; i -= 2 {
			if merged.Content[i].Kind == yaml.ScalarNode && merged.Content[i].Value == key {
				merged.Content[i+1] = node
				found = true
				break
			}
		}
		if !found {
			keyNode := &yaml.Node{}
			if err := keyNode.Encode(key); err != nil {
				return nil, err
			}
			merged.Content = append(merged.Content, keyNode, node)
		}
	}
	return &merged, nil
}

// mergeFlowDocument handles a file written as one flow mapping ("{a: 1}"):
// it is merged as nodes and written again.
func mergeFlowDocument(root *yaml.Node, keys []string, partial map[string]any) (string, error) {
	merged := *root
	merged.Style &^= yaml.FlowStyle
	merged.Content = append([]*yaml.Node(nil), root.Content...)
	for _, key := range keys {
		value := partial[key]
		index := -1
		for i := len(merged.Content) - 2; i >= 0; i -= 2 {
			if merged.Content[i].Kind == yaml.ScalarNode && merged.Content[i].Value == key {
				index = i
				break
			}
		}
		updates, isDict := value.(map[string]any)
		var node *yaml.Node
		var err error
		if index >= 0 && isDict && merged.Content[index+1].Kind == yaml.MappingNode {
			node, err = mergeMappingNode(merged.Content[index+1], updates)
		} else {
			node, err = valueNode(value)
		}
		if err != nil {
			return "", err
		}
		if index >= 0 {
			merged.Content[index+1] = node
			continue
		}
		keyNode := &yaml.Node{}
		if err := keyNode.Encode(key); err != nil {
			return "", err
		}
		merged.Content = append(merged.Content, keyNode, node)
	}
	return encodeNode(&merged)
}

// result applies the edits and returns the new text.
func (e *editor) result() (string, error) {
	edits := append([]edit(nil), e.edits...)
	sort.SliceStable(edits, func(i, j int) bool {
		a, b := edits[i], edits[j]
		if a.start != b.start {
			return a.start < b.start
		}
		// At the same line, inserted lines come before replaced ones. When
		// the last section ends at the end of the file, its new keys and the
		// new top-level keys go to the same line: the more indented keys come
		// first, because a less indented key would end the section.
		aInsert, bInsert := a.start == a.end, b.start == b.end
		if aInsert != bInsert {
			return aInsert
		}
		return aInsert && a.indent > b.indent
	})
	var out []string
	position := 0
	for _, ed := range edits {
		if ed.start < position {
			return "", errors.New("config merge: two changes overlap")
		}
		out = append(out, e.lines[position:ed.start]...)
		out = append(out, ed.newLines...)
		position = ed.end
	}
	out = append(out, e.lines[position:]...)
	return strings.Join(out, "\n") + "\n", nil
}
