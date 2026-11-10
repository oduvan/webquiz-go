package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// maxBodySize is the largest request body the handlers read: aiohttp's
// client_max_size (1 MiB). Python's request.json() failed on a bigger body,
// and the handlers answered "Invalid JSON".
const maxBodySize = 1 << 20

// newHTTPClient is httpx.AsyncClient(timeout=30): proxies from the
// environment, no redirects followed, 30 seconds to connect and 30 seconds
// for each wait for data (not for the whole download).
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DialContext = (&net.Dialer{Timeout: httpTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = httpTimeout
	transport.ResponseHeaderTimeout = httpTimeout
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // httpx does not follow redirects
		},
	}
}

// postJSON sends value as JSON and returns the answer's status and body.
// The JSON keeps "<", ">" and "&" as they are, like httpx.
func (m *Manager) postJSON(ctx context.Context, url string, value any) (int, []byte, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return 0, nil, err
	}
	data := bytes.TrimSuffix(body.Bytes(), []byte("\n"))
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := m.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(idleLimit(response.Body, stop))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, answer, nil
}

// fetch downloads one offered file into out (the body of _download_file).
func (m *Manager) fetch(ctx context.Context, url, sender string, out io.Writer) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return newError("Download failed: %v", err)
	}
	response, err := m.client.Do(request)
	if err != nil {
		return newError("Download failed: %v", err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable ||
		response.StatusCode == http.StatusGatewayTimeout:
		return newError("Server '%s' is not online", sender)
	case response.StatusCode != http.StatusOK:
		return newError("The sender does not offer this file anymore")
	}
	body := idleLimit(response.Body, stop)
	buffer := make([]byte, 64*1024)
	var size int64
	for {
		n, readErr := body.Read(buffer)
		if n > 0 {
			size += int64(n)
			if size > m.maxFileSize {
				return newError("File is larger than %d MB", m.maxFileSize/(1024*1024))
			}
			if _, err := out.Write(buffer[:n]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return newError("Download failed: %v", readErr)
		}
	}
}

// idleLimit stops a response (with stop) when no data comes for
// httpTimeout, like httpx's read timeout.
func idleLimit(body io.Reader, stop context.CancelFunc) io.Reader {
	return &idleReader{body: body, stop: stop}
}

type idleReader struct {
	body io.Reader
	stop context.CancelFunc
}

func (r *idleReader) Read(p []byte) (int, error) {
	timer := time.AfterFunc(httpTimeout, r.stop)
	defer timer.Stop()
	return r.body.Read(p)
}

// pyQuote is Python's urllib.parse.quote(text): every byte except letters,
// digits, "_.-~" and "/" becomes %XX.
func pyQuote(text string) string {
	const hexDigits = "0123456789ABCDEF"
	var out []byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '_', c == '.', c == '-', c == '~', c == '/':
			out = append(out, c)
		default:
			out = append(out, '%', hexDigits[c>>4], hexDigits[c&15])
		}
	}
	return string(out)
}

// decodeJSON decodes one JSON value like Python's json.loads: numbers stay
// json.Number, so 12 and 12.0 differ, and nothing may follow the value.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("extra data after the JSON value")
	}
	return value, nil
}

// readJSON is aiohttp's request.json(); any failure (also a body bigger
// than 1 MiB) is an error.
func readJSON(r *http.Request) (any, error) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodySize {
		return nil, fmt.Errorf("the body is bigger than %d bytes", maxBodySize)
	}
	return decodeJSON(data)
}

// writeJSON answers with JSON, like aiohttp's web.json_response.
func writeJSON(w http.ResponseWriter, status int, value any) error {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(bytes.TrimSuffix(body.Bytes(), []byte("\n")))
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) error {
	return writeJSON(w, status, map[string]any{"error": message})
}

// HandleOffer is POST /api/transfer/offer: another server offers files
// (handle_offer). It answers 202 when the request waits for the admin.
func (m *Manager) HandleOffer(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return writeError(w, http.StatusBadRequest, "Invalid JSON")
	}
	m.mu.Lock()
	incoming, err := m.addIncomingLocked(data)
	var view RequestView
	if err == nil {
		m.log.Info(fmt.Sprintf("File transfer request from '%s': %d file(s)", incoming.from, len(incoming.files)))
		if view, err = m.requestViewLocked(incoming); err == nil {
			m.notifyLocked(map[string]any{"type": "transfer_request", "request": view})
		}
	}
	m.mu.Unlock()
	var transferError *Error
	if errors.As(err, &transferError) {
		return writeError(w, transferError.Status, transferError.Message)
	}
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, map[string]any{"success": true})
}

// HandleDownload is GET /api/transfer/download/{token}/{type}/{filename}:
// it serves one offered file to the receiver (handle_download).
func (m *Manager) HandleDownload(w http.ResponseWriter, r *http.Request) error {
	fileType, filename := r.PathValue("type"), r.PathValue("filename")
	m.mu.Lock()
	o := m.findWaitingOfferLocked(r.PathValue("token"))
	offered := false
	if o != nil {
		for _, f := range o.files {
			if f.Type == fileType && f.Name == filename {
				offered = true
			}
		}
	}
	m.mu.Unlock()
	if !offered || !isAllowedFile(fileType, filename) {
		return writeError(w, http.StatusNotFound, "Not found")
	}

	path := filepath.Join(m.directories()[fileType], filename)
	file, err := os.Open(path)
	if err != nil {
		return writeError(w, http.StatusNotFound, "Not found")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return writeError(w, http.StatusNotFound, "Not found")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	// aiohttp's ETag: modification time in nanoseconds and size, in hex
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
	http.ServeContent(w, r, filename, info.ModTime(), file)
	return nil
}

// HandleResult is POST /api/transfer/result: the receiver reports
// accepted, rejected or failed (handle_result).
func (m *Manager) HandleResult(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return writeError(w, http.StatusBadRequest, "Invalid JSON")
	}
	fields, _ := data.(map[string]any)
	status, _ := fields["status"].(string)
	if fields == nil || (status != "accepted" && status != "rejected" && status != "failed") {
		return writeError(w, http.StatusBadRequest, "Invalid status")
	}

	m.mu.Lock()
	o := m.findWaitingOfferLocked(fields["token"])
	if o != nil {
		o.status = status
		m.log.Info(fmt.Sprintf("File transfer to '%s': %s", o.to, o.status))
		m.notifyLocked(map[string]any{"type": "transfer_result", "offer": o.view()})
	}
	m.mu.Unlock()
	if o == nil {
		return writeError(w, http.StatusNotFound, "Not found")
	}
	return writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
