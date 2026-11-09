package pyval

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The implicit resolvers of PyYAML (yaml/resolver.py, YAML 1.1). A plain
// (unquoted) scalar that matches one of them is not text.
var (
	boolPattern  = regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	floatPattern = regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+][0-9]+)?` +
		`|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?` +
		`|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*` +
		`|[-+]?\.(?:inf|Inf|INF)` +
		`|\.(?:nan|NaN|NAN))$`)
	intPattern = regexp.MustCompile(`^(?:[-+]?0b[0-1_]+` +
		`|[-+]?0[0-7_]+` +
		`|[-+]?(?:0|[1-9][0-9_]*)` +
		`|[-+]?0x[0-9a-fA-F_]+` +
		`|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	nullPattern      = regexp.MustCompile(`^(?:~|null|Null|NULL|)$`)
	timestampPattern = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]` +
		`|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?` +
		`(?:[Tt]|[ \t]+)[0-9][0-9]?` +
		`:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?` +
		`(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)
)

// boolValues are the words PyYAML's !!bool accepts, in lower case.
var boolValues = map[string]bool{"yes": true, "no": false, "true": true, "false": false, "on": true, "off": false}

// LoadYAML reads one YAML document the way PyYAML's yaml.safe_load did in
// WebQuiz 1.19. An empty document gives nil. More than one document, a syntax
// error, an unknown tag or a value PyYAML could not build gives an error.
//
// Differences from PyYAML, all in files nobody writes by hand: !!set, !!omap
// and !!pairs are errors; an alias inside its own anchor is an error; the
// keys 1, 1.0 and true stay three keys (Python merges them into one).
func LoadYAML(data []byte) (any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	var another yaml.Node
	if err := decoder.Decode(&another); err == nil {
		return nil, fmt.Errorf("expected a single document in the stream, but found another document (line %d)", another.Line)
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	c := &constructor{built: map[*yaml.Node]any{}, building: map[*yaml.Node]bool{}}
	return c.value(&document)
}

// constructor turns yaml.v3 nodes into values, like PyYAML's SafeConstructor.
type constructor struct {
	built    map[*yaml.Node]any  // values of anchored nodes: aliases share them, as in Python
	building map[*yaml.Node]bool // nodes being built, to find an alias inside its own anchor
}

func (c *constructor) value(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return c.value(n.Content[0])
	case yaml.AliasNode:
		if n.Alias == nil {
			return nil, nodeError(n, "unknown alias")
		}
		return c.value(n.Alias)
	}
	if v, ok := c.built[n]; ok {
		return v, nil
	}
	if c.building[n] {
		return nil, nodeError(n, "an alias refers to the node that contains it")
	}
	c.building[n] = true
	defer delete(c.building, n)

	var v any
	var err error
	switch n.Kind {
	case yaml.ScalarNode:
		v, err = scalar(n)
	case yaml.SequenceNode:
		v, err = c.sequence(n)
	case yaml.MappingNode:
		v, err = c.mapping(n)
	default:
		err = nodeError(n, "unknown node")
	}
	if err != nil {
		return nil, err
	}
	if n.Anchor != "" {
		c.built[n] = v
	}
	return v, nil
}

func (c *constructor) sequence(n *yaml.Node) (any, error) {
	if hasExplicitTag(n) && n.Tag != "!!seq" {
		return nil, nodeError(n, "could not determine a constructor for the tag %s", n.Tag)
	}
	items := make([]any, 0, len(n.Content))
	for _, child := range n.Content {
		item, err := c.value(child)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (c *constructor) mapping(n *yaml.Node) (any, error) {
	if hasExplicitTag(n) && n.Tag != "!!map" {
		return nil, nodeError(n, "could not determine a constructor for the tag %s", n.Tag)
	}
	pairs, err := flatten(n)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, err := c.key(pair[0])
		if err != nil {
			return nil, err
		}
		value, err := c.value(pair[1])
		if err != nil {
			return nil, err
		}
		m[key] = value // a repeated key: the later value wins, as in PyYAML
	}
	return m, nil
}

// flatten gives the key/value pairs of a mapping with merge keys ("<<")
// replaced by the pairs they bring in, like PyYAML's flatten_mapping: merged
// pairs come first, so the mapping's own keys win.
func flatten(n *yaml.Node) ([][2]*yaml.Node, error) {
	var merged, own [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		if !isMergeKey(key) {
			own = append(own, [2]*yaml.Node{key, value})
			continue
		}
		value = resolveAlias(value)
		switch value.Kind {
		case yaml.MappingNode:
			pairs, err := flatten(value)
			if err != nil {
				return nil, err
			}
			merged = append(merged, pairs...)
		case yaml.SequenceNode:
			var groups [][][2]*yaml.Node
			for _, item := range value.Content {
				item = resolveAlias(item)
				if item.Kind != yaml.MappingNode {
					return nil, nodeError(item, "expected a mapping for merging")
				}
				pairs, err := flatten(item)
				if err != nil {
					return nil, err
				}
				groups = append(groups, pairs)
			}
			// PyYAML adds the later mappings first, so the earlier ones win.
			for i := len(groups) - 1; i >= 0; i-- {
				merged = append(merged, groups[i]...)
			}
		default:
			return nil, nodeError(value, "expected a mapping or list of mappings for merging")
		}
	}
	return append(merged, own...), nil
}

func isMergeKey(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!merge" && n.Value == "<<" && !isQuoted(n)
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// key builds a mapping key and turns it into text the way Python's
// json.dumps writes dict keys.
func (c *constructor) key(n *yaml.Node) (string, error) {
	if n.Kind == yaml.ScalarNode && n.Value == "=" && !isQuoted(n) && !hasExplicitTag(n) {
		return "=", nil // PyYAML turns the "value" tag into text when it is a key
	}
	value, err := c.value(n)
	if err != nil {
		return "", err
	}
	switch x := value.(type) {
	case string:
		return x, nil
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(x), nil
	case []any, map[string]any:
		return "", nodeError(n, "found unhashable key")
	case []byte:
		return string(x), nil
	}
	return Str(value), nil
}

// scalar builds the value of a scalar node.
func scalar(n *yaml.Node) (any, error) {
	if hasExplicitTag(n) {
		return tagged(n)
	}
	if isQuoted(n) {
		return n.Value, nil
	}
	return plain(n)
}

func hasExplicitTag(n *yaml.Node) bool {
	return n.Style&yaml.TaggedStyle != 0
}

func isQuoted(n *yaml.Node) bool {
	return n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0
}

// plain resolves an unquoted scalar with PyYAML's implicit resolvers.
func plain(n *yaml.Node) (any, error) {
	s := n.Value
	switch {
	case boolPattern.MatchString(s):
		return boolValues[strings.ToLower(s)], nil
	case floatPattern.MatchString(s):
		return constructFloat(n, s)
	case intPattern.MatchString(s):
		return constructInt(n, s)
	case s == "<<":
		return nil, nodeError(n, "could not determine a constructor for the tag 'tag:yaml.org,2002:merge'")
	case nullPattern.MatchString(s):
		return nil, nil
	case timestampPattern.MatchString(s):
		return Timestamp(s), nil
	case s == "=":
		return nil, nodeError(n, "could not determine a constructor for the tag 'tag:yaml.org,2002:value'")
	}
	return s, nil
}

// tagged builds a scalar with an explicit tag such as !!int.
func tagged(n *yaml.Node) (any, error) {
	s := n.Value
	switch n.Tag {
	case "!!str":
		return s, nil
	case "!!int":
		return constructInt(n, s)
	case "!!float":
		return constructFloat(n, s)
	case "!!bool":
		v, ok := boolValues[strings.ToLower(s)]
		if !ok {
			return nil, nodeError(n, "%q is not a boolean", s)
		}
		return v, nil
	case "!!null":
		return nil, nil
	case "!!timestamp":
		if !timestampPattern.MatchString(s) {
			return nil, nodeError(n, "%q is not a timestamp", s)
		}
		return Timestamp(s), nil
	case "!!binary":
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
		if err != nil {
			return nil, nodeError(n, "failed to decode base64 data: %v", err)
		}
		return decoded, nil
	}
	return nil, nodeError(n, "could not determine a constructor for the tag %s", n.Tag)
}

// constructInt is PyYAML's construct_yaml_int.
func constructInt(n *yaml.Node, s string) (any, error) {
	value := strings.ReplaceAll(s, "_", "")
	negative := strings.HasPrefix(value, "-")
	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	result := new(big.Int)
	var ok bool
	switch {
	case value == "0":
		ok = true
	case strings.HasPrefix(value, "0b"):
		_, ok = result.SetString(value[2:], 2)
	case strings.HasPrefix(value, "0x"):
		_, ok = result.SetString(value[2:], 16)
	case strings.HasPrefix(value, "0"):
		_, ok = result.SetString(value, 8)
	case strings.Contains(value, ":"):
		ok = true
		for _, part := range strings.Split(value, ":") {
			digit, partOK := new(big.Int).SetString(strings.TrimSpace(part), 10)
			if !partOK {
				ok = false
				break
			}
			result.Mul(result, big.NewInt(60))
			result.Add(result, digit)
		}
	default:
		_, ok = result.SetString(strings.TrimSpace(value), 10)
	}
	if !ok {
		return nil, nodeError(n, "invalid literal for int(): %q", s)
	}
	if negative {
		result.Neg(result)
	}
	return smallInt(result), nil
}

// smallInt gives an int when the value fits, else the *big.Int.
func smallInt(i *big.Int) any {
	if i.IsInt64() {
		v := i.Int64()
		if int64(int(v)) == v {
			return int(v)
		}
	}
	return i
}

// constructFloat is PyYAML's construct_yaml_float.
func constructFloat(n *yaml.Node, s string) (any, error) {
	value := strings.ToLower(strings.ReplaceAll(s, "_", ""))
	sign := 1.0
	if strings.HasPrefix(value, "-") {
		sign = -1
	}
	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	switch {
	case value == ".inf":
		return sign * math.Inf(1), nil
	case value == ".nan":
		return math.NaN(), nil
	case strings.Contains(value, ":"):
		// Same order of operations as PyYAML, so the float rounds the same way.
		parts := strings.Split(value, ":")
		total, base := 0.0, 1.0
		for i := len(parts) - 1; i >= 0; i-- {
			digit, ok := parsePythonFloat(parts[i])
			if !ok {
				return nil, nodeError(n, "could not convert string to float: %q", s)
			}
			total += digit * base
			base *= 60
		}
		return sign * total, nil
	}
	f, ok := parsePythonFloat(value)
	if !ok {
		return nil, nodeError(n, "could not convert string to float: %q", s)
	}
	return sign * f, nil
}

func nodeError(n *yaml.Node, format string, args ...any) error {
	return fmt.Errorf("yaml: line %d: %s", n.Line, fmt.Sprintf(format, args...))
}
