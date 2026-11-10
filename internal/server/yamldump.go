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
	node, err := dumpNode(value)
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

func dumpNode(value any) (*yaml.Node, error) {
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
			keyNode := &yaml.Node{}
			if err := keyNode.Encode(key); err != nil {
				return nil, err
			}
			valueNode, err := dumpNode(v[key])
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, keyNode, valueNode)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range v {
			itemNode, err := dumpNode(item)
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
