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
// alphabetical order; new keys go after the last key of their section (new
// top-level keys at the end of the file).
//
// Only the lines of changed values are rewritten. Comments, blank lines,
// quotes and spacing elsewhere stay as they were; a changed value keeps the
// comment on its line and the quote style of the old text. The values of
// partial are what a JSON body holds (decoded with UseNumber); they are
// written so that the file reads back with PyYAML's rules as the same values
// (text like "yes" gets quotes, 1.0 stays a float).
//
// As in the Python version, a file whose text is not a mapping (a list, plain
// text, broken YAML) is replaced by the update. Unlike Python (ruamel.yaml
// refused repeated keys and dropped the whole file), a repeated key is
// handled like PyYAML reads it: the last one counts and is the one changed.
func MergeYAML(original []byte, partial map[string]any) ([]byte, error) {
	text := string(original)
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
		out, err = newEditor(text).merge(root, keys, partial)
	}
	if err != nil {
		return nil, err
	}
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return []byte(out), nil
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
// before line start.
type edit struct {
	start, end int
	newLines   []string
}

func newEditor(text string) *editor {
	return &editor{lines: strings.Split(strings.TrimSuffix(text, "\n"), "\n")}
}

// entry is one key of a block mapping with the lines it takes.
type entry struct {
	key, value *yaml.Node
	start      int // the line of the key (0-based)
	end        int // the line after the entry's last content line
}

// entries lists the keys of a block mapping; limit is the line where the
// mapping ends at the latest.
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
		// Comments and blank lines after the entry belong to what follows.
		list[i].end = list[i].start + 1
		for line := list[i].start + 1; line < next && line < len(e.lines); line++ {
			if isContentLine(e.lines[line]) {
				list[i].end = line + 1
			}
		}
	}
	return list
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
	for _, key := range keys {
		value := partial[key]
		ent := lastEntry(topLevel, key)
		updates, isDict := value.(map[string]any)
		var err error
		switch {
		case ent == nil:
			err = e.insertEntry(e.appendLine(topLevel), key, value, 0)
		case isDict && ent.value.Kind == yaml.MappingNode && ent.value.Style&yaml.FlowStyle == 0:
			err = e.mergeSection(*ent, updates)
		case isDict && ent.value.Kind == yaml.MappingNode:
			merged, mergeErr := mergeMappingNode(ent.value, updates)
			if mergeErr != nil {
				return "", mergeErr
			}
			err = e.replaceEntry(*ent, 0, merged)
		default:
			err = e.replaceValue(*ent, 0, value)
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
// a document marker ("..." or "---") that follows the mapping.
func (e *editor) appendLine(topLevel []entry) int {
	from := 0
	if len(topLevel) > 0 {
		from = topLevel[len(topLevel)-1].end
	}
	for line := from; line < len(e.lines); line++ {
		trimmed := strings.TrimSpace(e.lines[line])
		if trimmed == "..." || strings.HasPrefix(trimmed, "---") {
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
	e.edits = append(e.edits, edit{start: at, end: at, newLines: lines})
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
// value, or after "key:" when the value starts on the next line. column is
// where the comment starts, in characters.
func (e *editor) keyLineComment(ent entry) (column int, comment string, ok bool) {
	line := e.lines[ent.start]
	var after int
	if ent.value.Line == ent.key.Line && ent.value.Kind == yaml.ScalarNode {
		_, end, found := scalarSpan(line, ent.value)
		if !found {
			return 0, "", false
		}
		after = end
	} else if ent.value.Line > ent.key.Line {
		_, keyEnd, found := scalarSpan(line, ent.key)
		if !found {
			return 0, "", false
		}
		colon := strings.IndexByte(line[keyEnd:], ':')
		if colon < 0 || strings.TrimSpace(line[keyEnd:keyEnd+colon]) != "" {
			return 0, "", false
		}
		after = keyEnd + colon + 1
	} else {
		return 0, "", false
	}
	rest := line[after:]
	trimmed := strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	return utf8.RuneCountInString(line[:len(line)-len(trimmed)]), trimmed, true
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
	return node, nil
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
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		// at the same line, inserted lines come before replaced ones
		return edits[i].start == edits[i].end && edits[j].start != edits[j].end
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
