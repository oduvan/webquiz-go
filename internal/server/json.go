package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

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
