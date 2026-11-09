package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// Client sends requests to a test server. TestServer's own client keeps no
// cookies, like plain requests.get/requests.post in the Python tests; a
// client from NewClient keeps them, like a requests.Session.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// RequestOption changes one request.
type RequestOption func(*http.Request)

// WithCookies adds cookies to the request (for example from AdminCookies).
func WithCookies(cookies ...*http.Cookie) RequestOption {
	return func(r *http.Request) {
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
	}
}

// WithHeader sets a request header.
func WithHeader(name, value string) RequestOption {
	return func(r *http.Request) { r.Header.Set(name, value) }
}

// FromIP makes the request look like it comes from another address. The
// test client connects from 127.0.0.1, and the server trusts
// X-Forwarded-For from loopback (a proxy or the tunnel on the same machine),
// as the Python tests used it.
func FromIP(ip string) RequestOption {
	return WithHeader("X-Forwarded-For", ip)
}

// Response is a finished response with the whole body read.
type Response struct {
	Status  int
	Header  http.Header
	Body    []byte
	Cookies []*http.Cookie
}

// Text is the body as text.
func (r *Response) Text() string { return string(r.Body) }

// JSON decodes the body as a JSON object. Numbers stay json.Number, so a
// test can tell 1 from 1.0. The test fails when the body is not an object.
func (r *Response) JSON(t testing.TB) map[string]any {
	t.Helper()
	var data map[string]any
	r.Decode(t, &data)
	return data
}

// Decode decodes the body as JSON into v (numbers as json.Number when v
// holds them as any). The test fails on broken JSON.
func (r *Response) Decode(t testing.TB, v any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(r.Body))
	decoder.UseNumber()
	if err := decoder.Decode(v); err != nil {
		t.Fatalf("response %d is not the expected JSON (%v): %s", r.Status, err, r.Body)
	}
}

// Do sends a request and reads the whole response. body may be nil (no
// body), a string or []byte (sent as it is), or any other value (sent as
// JSON with Content-Type application/json). path starts with "/".
func (c *Client) Do(t testing.TB, method, path string, body any, opts ...RequestOption) *Response {
	t.Helper()
	var reader io.Reader
	isJSON := false
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	case []byte:
		reader = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("encode the request body: %v", err)
		}
		reader = bytes.NewReader(data)
		isJSON = true
	}
	request, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		t.Fatalf("make the request %s %s: %v", method, path, err)
	}
	if isJSON {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(request)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read the response of %s %s: %v", method, path, err)
	}
	return &Response{Status: response.StatusCode, Header: response.Header, Body: data, Cookies: response.Cookies()}
}

// GetJSON sends a GET request. Read the reply with Response.JSON.
func (c *Client) GetJSON(t testing.TB, path string, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, http.MethodGet, path, nil, opts...)
}

// PostJSON sends a POST request with body (see Do for the body types).
func (c *Client) PostJSON(t testing.TB, path string, body any, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, http.MethodPost, path, body, opts...)
}

// Put sends a PUT request with body (see Do for the body types).
func (c *Client) Put(t testing.TB, path string, body any, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, http.MethodPut, path, body, opts...)
}

// Delete sends a DELETE request.
func (c *Client) Delete(t testing.TB, path string, opts ...RequestOption) *Response {
	t.Helper()
	return c.Do(t, http.MethodDelete, path, nil, opts...)
}

// NoRedirects returns a copy of the client that does not follow redirects
// (requests' allow_redirects=False), so a test sees the 301 itself.
func (c *Client) NoRedirects() *Client {
	httpClient := *c.HTTP
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{BaseURL: c.BaseURL, HTTP: &httpClient}
}

// NewClient returns a client that keeps cookies, like a requests.Session.
func (c *Client) NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{BaseURL: c.BaseURL, HTTP: &http.Client{Jar: jar, Transport: c.HTTP.Transport}}
}
