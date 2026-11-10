package server

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// dumpYAML writes a value as yaml.dump(value, default_flow_style=False,
// allow_unicode=True) did: block style, mapping keys sorted, non-ASCII text
// kept, and a float always with a decimal point (30.0 stays 30.0). The
// values are what pyval.LoadYAML or a JSON body decoded with UseNumber give.
//
// yaml.v3 indents the items of a list under a key and PyYAML did not. Both
// read back the same, so this does not try to copy that.
func dumpYAML(value any) ([]byte, error) {
	out, err := dumpYAMLStyle(value, false)
	if err != nil {
		return nil, err
	}
	// Last check: the text must load back to the same value. If not, write
	// every text in double quotes, which always reads back.
	if back, err := pyval.LoadYAML(out); err != nil || !yamlSame(value, back) {
		return dumpYAMLStyle(value, true)
	}
	return out, nil
}

// yamlSame tells whether a value written by dumpYAML came back as the same
// value (numbers are compared by their text form, so 2 and 2.0 differ).
func yamlSame(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, item := range x {
			other, has := y[key]
			if !has || !yamlSame(item, other) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !yamlSame(x[i], y[i]) {
				return false
			}
		}
		return true
	case string:
		y, ok := b.(string)
		return ok && x == y
	case json.Number:
		return pyval.IsInt(x) == pyval.IsInt(b) && pyval.Equal(x, b)
	}
	return pyval.KindOf(a) == pyval.KindOf(b) && pyval.Equal(a, b)
}

func dumpYAMLStyle(value any, quoteAll bool) ([]byte, error) {
	node, err := dumpNode(value, quoteAll)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// stringNode writes a text as YAML. It uses yaml.v3's choice only when the
// scalar reads back as the same text; otherwise (or with quoteAll) the text
// is double-quoted. Cases that need this: "=", "<<", a text that starts with
// a line end.
func stringNode(text string, quoteAll bool) *yaml.Node {
	quoted := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: text, Style: yaml.DoubleQuotedStyle}
	if quoteAll {
		return quoted
	}
	node := &yaml.Node{}
	if err := node.Encode(text); err != nil {
		return quoted
	}
	out, err := yaml.Marshal(node)
	if err != nil {
		return quoted
	}
	if back, err := pyval.LoadYAML(out); err != nil || back != any(text) {
		return quoted
	}
	return node
}

func dumpNode(value any, quoteAll bool) (*yaml.Node, error) {
	switch v := value.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, key := range keys {
			keyNode := stringNode(key, quoteAll)
			valueNode, err := dumpNode(v[key], quoteAll)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, keyNode, valueNode)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range v {
			itemNode, err := dumpNode(item, quoteAll)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, itemNode)
		}
		return node, nil
	case json.Number:
		if pyval.IsInt(v) {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: v.String()}, nil
		}
		f, _ := pyval.ToFloat(v)
		return floatNode(f), nil
	case float64:
		return floatNode(v), nil
	case *big.Int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: v.String()}, nil
	case pyval.Timestamp:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: string(v)}, nil
	case []byte:
		return nil, fmt.Errorf("cannot write binary data as YAML")
	}
	if text, ok := value.(string); ok {
		return stringNode(text, quoteAll), nil
	}
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		return nil, err
	}
	return node, nil
}

// floatNode writes a float like PyYAML's represent_float.
func floatNode(f float64) *yaml.Node {
	var text string
	switch {
	case math.IsNaN(f):
		text = ".nan"
	case math.IsInf(f, 1):
		text = ".inf"
	case math.IsInf(f, -1):
		text = "-.inf"
	default:
		text = strings.ToLower(pyval.FloatRepr(f))
		if !strings.Contains(text, ".") && strings.Contains(text, "e") {
			text = strings.Replace(text, "e", ".0e", 1)
		}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: text}
}

// jsonSafe copies a decoded value for the JSON reply, with every float
// written like Python's json.dumps does (30.0, not 30): a float becomes a
// json.Number holding its repr.
func jsonSafe(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = jsonSafe(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = jsonSafe(item)
		}
		return out
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return v
		}
		return json.Number(pyval.FloatRepr(v))
	}
	return value
}
