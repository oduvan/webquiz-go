package transfer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/webquiz-go/internal/explanations"
)

// loadError is a received quiz that cannot be read as YAML: its references
// stay as they are, and a warning goes to the log (Python's ruamel.yaml
// failed to load it the same way).
type loadError struct{ err error }

func (e *loadError) Error() string { return e.err.Error() }

// rewriteQuizMedia points the references of a quiz file to images and
// attachments that were saved under a new name (rewrite_quiz_media).
// renamed maps (file type, original name) to the saved name. It returns
// whether the file changed.
//
// Like Python's round-trip YAML parser (ruamel.yaml), it keeps comments,
// the quote style of every value, flow lists ([a, b]) and block texts
// (explanation: |). The file is written again by the YAML library, so the
// indentation of lists may change and blank lines between entries are not
// kept. A file that is not valid YAML is left alone and the error is a
// *loadError. Unlike ruamel.yaml, which refused a file with a repeated key
// (PyYAML reads the last one), every repeated "image", "file", "options"
// or "explanation" key is updated.
func rewriteQuizMedia(quizPath string, renamed map[mediaFile]string) (bool, error) {
	data, err := os.ReadFile(quizPath)
	if err != nil {
		return false, &loadError{err}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil // an empty file has no references
		}
		return false, &loadError{err}
	}
	var another yaml.Node
	if err := decoder.Decode(&another); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("expected a single document in the stream, but found another document (line %d)", another.Line)
		}
		return false, &loadError{err}
	}

	renamedImages := map[string]string{}
	for f, newName := range renamed {
		if f.Type == "images" {
			renamedImages[f.Name] = newName
		}
	}
	changed := false
	for _, question := range questionNodes(&document) {
		for i := 0; i+1 < len(question.Content); i += 2 {
			key := question.Content[i]
			if !isStringScalar(key) {
				continue
			}
			slot := &question.Content[i+1]
			switch key.Value {
			case "image":
				changed = renameReference(slot, "images", renamed) || changed
			case "file":
				changed = renameReference(slot, "attachments", renamed) || changed
			case "options":
				if options := resolve(*slot); options.Kind == yaml.SequenceNode {
					for j := range options.Content {
						changed = renameReference(&options.Content[j], "images", renamed) || changed
					}
				}
			case "explanation":
				value := resolve(*slot)
				if !isStringScalar(value) {
					continue
				}
				if text := explanations.RewriteImages(value.Value, renamedImages); text != value.Value {
					setScalar(slot, text) // the style stays, for example a "|" block
					changed = true
				}
			}
		}
	}
	if !changed {
		return false, nil
	}
	fixMergeKeys(&document)
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return false, err
	}
	if err := encoder.Close(); err != nil {
		return false, err
	}
	if err := os.WriteFile(quizPath, out.Bytes(), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// questionNodes are the mapping nodes of the "questions" list.
func questionNodes(document *yaml.Node) []*yaml.Node {
	if len(document.Content) == 0 {
		return nil
	}
	root := resolve(document.Content[0])
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var questions *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if isStringScalar(root.Content[i]) && root.Content[i].Value == "questions" {
			questions = resolve(root.Content[i+1]) // the last one counts, as for a mapping
		}
	}
	if questions == nil || questions.Kind != yaml.SequenceNode {
		return nil
	}
	var list []*yaml.Node
	for _, item := range questions.Content {
		if item = resolve(item); item.Kind == yaml.MappingNode {
			list = append(list, item)
		}
	}
	return list
}

// renameReference points one quiz value to the new name of its file, when
// the file got one. The value keeps its quote style.
func renameReference(slot **yaml.Node, fileType string, renamed map[mediaFile]string) bool {
	value := resolve(*slot)
	if !isStringScalar(value) {
		return false
	}
	name, ok := mediaFileName(fileType, value.Value)
	if !ok {
		return false
	}
	newName, ok := renamed[mediaFile{fileType, name}]
	if !ok || newName == "" {
		return false
	}
	setScalar(slot, value.Value[:len(value.Value)-len(name)]+newName)
	return true
}

// setScalar gives a string value a new text and keeps its style. An alias
// (*name) is replaced by its own value, so the anchored value stays.
func setScalar(slot **yaml.Node, text string) {
	node := *slot
	if node.Kind == yaml.AliasNode {
		target := resolve(node)
		copied := *target
		copied.Anchor = ""
		copied.HeadComment, copied.LineComment, copied.FootComment = node.HeadComment, node.LineComment, node.FootComment
		node = &copied
		*slot = node
	}
	node.Value = text
}

// resolve follows aliases to the node they stand for.
func resolve(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// isStringScalar tells whether a node is text (a string for the YAML
// library, as for Python's round-trip parser).
func isStringScalar(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str"
}

// fixMergeKeys keeps merge keys ("<<: *defaults") as they were written:
// yaml.v3 would write them as "!!merge <<".
func fixMergeKeys(n *yaml.Node) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind == yaml.ScalarNode && key.Value == "<<" && key.Tag == "!!merge" && key.Style&yaml.TaggedStyle == 0 {
				key.Tag = "!!str"
			}
		}
	}
	for _, child := range n.Content {
		fixMergeKeys(child)
	}
}
