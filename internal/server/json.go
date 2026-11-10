package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// maxBodySize is the largest request body the server reads: aiohttp's
// default client_max_size (1 MiB). A bigger body gets 413, as in Python.
const maxBodySize = 1 << 20

// statusError is an error that answers with its own status and plain text
// instead of the 500 JSON error (aiohttp's HTTPException passed through
// error_middleware the same way).
type statusError struct {
	status int
	text   string
}

func (e *statusError) Error() string { return e.text }

// readBody reads the request body, at most maxBodySize bytes.
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodySize {
		return nil, &statusError{
			status: http.StatusRequestEntityTooLarge,
			text:   fmt.Sprintf("Maximum request body size %d exceeded", maxBodySize),
		}
	}
	return data, nil
}

// readJSON reads the request body as JSON, like aiohttp's request.json().
// Numbers stay json.Number (UseNumber), so 1 and 1.0 stay different as in
// Python. An empty or broken body is an error, so is anything after the value
// (json.loads' "Extra data"). Objects are map[string]any, arrays []any.
func readJSON(r *http.Request) (any, error) {
	data, err := readBody(r)
	if err != nil {
		return nil, err
	}
	return decodeJSON(data)
}

// decodeJSON decodes one JSON value with UseNumber.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("Expecting value: the request body is empty")
		}
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("Extra data after the JSON value")
	}
	return value, nil
}

// pyObject returns a decoded JSON value as an object. For any other value it
// returns the error Python raised when a handler called data.get(...) on it:
// "'list' object has no attribute 'get'".
func pyObject(data any) (map[string]any, error) {
	if m, ok := data.(map[string]any); ok {
		return m, nil
	}
	return nil, fmt.Errorf("'%s' object has no attribute 'get'", pyval.KindOf(data))
}

// keyError is the error of Python's data[key] for a missing key. Its text is
// what str(KeyError(key)) gave, for example 'username' with the quotes.
func keyError(key string) error {
	return errors.New(pyval.Repr(key))
}

// encodeJSON writes a value as JSON without HTML escaping (like Python's
// json.dumps, which kept "<" and "&" as they are) and without a line end.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// writeJSON answers with status and v as JSON (web.json_response). An error
// means v could not be encoded and nothing was written yet.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	data, err := encodeJSON(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
	return nil
}

// writeError answers with {"error": text}.
func writeError(w http.ResponseWriter, status int, text string) error {
	return writeJSON(w, status, map[string]any{"error": text})
}

// writeHTML answers with an HTML page.
func writeHTML(w http.ResponseWriter, status int, page []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(page)
}

// writeText answers with plain text, like aiohttp's own error pages
// ("404: Not Found").
func writeText(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

// notImplemented answers 501 {"error": "not implemented"}. It is the body of
// every handler that a later task of the Go migration builds; the route
// table already points to those handlers.
func notImplemented(w http.ResponseWriter, _ *http.Request) error {
	return writeError(w, http.StatusNotImplemented, "not implemented")
}

// The helpers below give a handler the results and errors of the Python
// expressions it was written from, for request values decoded by readJSON.
// A Python handler that failed on a bad request answered 500 with the
// exception text (error_middleware); returning these errors does the same.

// pyGetItem is Python's data[key]: a KeyError for a missing key, a TypeError
// when data is not an object.
func pyGetItem(data any, key string) (any, error) {
	switch d := data.(type) {
	case map[string]any:
		value, ok := d[key]
		if !ok {
			return nil, keyError(key)
		}
		return value, nil
	case []any:
		return nil, errors.New("list indices must be integers or slices, not str")
	case string:
		return nil, errors.New("string indices must be integers, not 'str'")
	}
	return nil, fmt.Errorf("'%s' object is not subscriptable", pyval.KindOf(data))
}

// pyGet is Python's data.get(key, fallback).
func pyGet(data any, key string, fallback any) (any, error) {
	object, err := pyObject(data)
	if err != nil {
		return nil, err
	}
	if value, ok := object[key]; ok {
		return value, nil
	}
	return fallback, nil
}

// pyStrip is Python's value.strip() for a value that should be text.
func pyStrip(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("'%s' object has no attribute 'strip'", pyval.KindOf(value))
	}
	return pyval.Strip(text), nil
}

// pyStrKey is a value used as a key of a dict with text keys (users,
// registration tokens), as in "user_id in self.users": text is the key, any
// other value is a key that is never there, and a list or an object is
// Python's "unhashable type" error.
func pyStrKey(value any) (key string, ok bool, err error) {
	switch kind := pyval.KindOf(value); kind {
	case pyval.KindStr:
		return value.(string), true, nil
	case pyval.KindList, pyval.KindDict:
		return "", false, fmt.Errorf("unhashable type: '%s'", kind)
	}
	return "", false, nil
}

// jsonKeyOrder returns the keys of the object body[field] of a JSON request
// body in the order they were written. Python's dicts keep that order and
// its handlers went through the keys in it; a decoded Go map has no order.
// Repeated keys count once, at their first place; a repeated field counts
// with its last value, as in Python's json module. It returns nil when the
// value is not an object.
func jsonKeyOrder(body []byte, field string) []string {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var keys []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return keys
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return keys
		}
		if token == field {
			keys = objectKeys(value)
		}
	}
	return keys
}

// objectKeys returns the keys of a JSON object in their order, each once.
func objectKeys(object []byte) []string {
	decoder := json.NewDecoder(bytes.NewReader(object))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	keys := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return keys
		}
		if key, ok := token.(string); ok && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}
