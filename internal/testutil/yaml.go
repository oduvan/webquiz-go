package testutil

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// YAML writes a Go value as YAML the way PyYAML's yaml.dump did in the
// Python tests: mapping keys sorted, block style, and floats always with a
// decimal point (30.0 stays a float, 1e17 becomes 1.0e+17). yaml.v3 alone
// would write the float 30.0 as 30, which reads back as a whole number and
// changes what a test checks.
//
// Values may be nil, bool, numbers, strings, slices and maps with string
// keys (any element types).
func YAML(value any) ([]byte, error) {
	node, err := yamlNode(reflect.ValueOf(value))
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

// yamlNode turns a value into a YAML node.
func yamlNode(v reflect.Value) (*yaml.Node, error) {
	if !v.IsValid() {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
		}
		return yamlNode(v.Elem())
	}
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: pyYAMLFloat(v.Float())}, nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("testutil.YAML: map keys must be strings, not %s", v.Type().Key())
		}
		keys := make([]string, 0, v.Len())
		for _, key := range v.MapKeys() {
			keys = append(keys, key.String())
		}
		sort.Strings(keys) // PyYAML sorts keys (sort_keys=True)
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, key := range keys {
			valueNode, err := yamlNode(v.MapIndex(reflect.ValueOf(key).Convert(v.Type().Key())))
			if err != nil {
				return nil, err
			}
			keyNode := &yaml.Node{}
			if err := keyNode.Encode(key); err != nil {
				return nil, err
			}
			node.Content = append(node.Content, keyNode, valueNode)
		}
		return node, nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return nil, fmt.Errorf("testutil.YAML: []byte values are not supported")
		}
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i := 0; i < v.Len(); i++ {
			item, err := yamlNode(v.Index(i))
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, item)
		}
		return node, nil
	}
	// Strings, booleans and whole numbers: yaml.v3 writes them like PyYAML
	// (it quotes text that YAML 1.1 would read as something else, like "yes").
	node := &yaml.Node{}
	if err := node.Encode(v.Interface()); err != nil {
		return nil, err
	}
	return node, nil
}

// pyYAMLFloat writes a float like PyYAML's represent_float.
func pyYAMLFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return ".nan"
	case math.IsInf(f, 1):
		return ".inf"
	case math.IsInf(f, -1):
		return "-.inf"
	}
	text := strings.ToLower(pyval.FloatRepr(f))
	if !strings.Contains(text, ".") && strings.Contains(text, "e") {
		text = strings.Replace(text, "e", ".0e", 1)
	}
	return text
}
