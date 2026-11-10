package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/pyval"
)

// Quiz downloads from ZIP archives and file attachments (server.py
// admin_download_quiz, can_send_token_to, find_downloadable_quiz,
// serve_quiz_file).

// downloadTimeout is how long one step of a download may wait (httpx
// timeout=30.0): connecting, the answer headers, and each wait for data.
const downloadTimeout = 30 * time.Second

// canSendTokenTo tells whether an access token may be sent to this address.
// Tokens go over https only, so nobody can read them on the way. Plain http
// is allowed for this machine, where test servers run.
func canSendTokenTo(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		switch strings.ToLower(parsed.Hostname()) {
		case "localhost", "127.0.0.1", "::1":
			return true
		}
	}
	return false
}

// findDownloadableQuiz is find_downloadable_quiz: the entry of the config's
// "quizzes" list with this name.
func (s *Server) findDownloadableQuiz(name any) (config.DownloadableQuiz, bool) {
	text, ok := name.(string)
	if !ok {
		return config.DownloadableQuiz{}, false
	}
	var found config.DownloadableQuiz
	var ok2 bool
	s.withLock(func() {
		for _, quiz := range s.cfg.Quizzes {
			if quiz.Name == text {
				found, ok2 = quiz, true
				return
			}
		}
	})
	return found, ok2
}

// checkDownloadFolder checks the folder of a download and returns the folder in
// normal form, or the text of the 400 error when the folder is not allowed.
func (s *Server) checkDownloadFolder(folder string) (string, string) {
	// Absolute paths, Unix-style or Windows-style.
	if filepath.IsAbs(folder) || strings.HasPrefix(folder, "/") || (len(folder) >= 2 && folder[1] == ':') {
		return "", "Invalid folder path: absolute paths not allowed"
	}
	if strings.Contains(folder, "..") {
		return "", "Invalid folder path: parent directory access not allowed"
	}
	normalized := filepath.Clean(folder)
	if strings.Contains(normalized, "..") {
		return "", "Invalid folder path: parent directory access not allowed"
	}
	// The folder must stay inside the quizzes folder.
	testPath := filepath.Clean(filepath.Join(s.quizzesDir, normalized))
	base := filepath.Clean(s.quizzesDir) + string(filepath.Separator)
	if !strings.HasPrefix(testPath+string(filepath.Separator), base) {
		return "", "Invalid folder path: path traversal detected"
	}
	return normalized, ""
}

// adminDownloadQuiz is POST /api/admin/download-quiz (admin_download_quiz):
// it downloads a ZIP archive and copies the chosen folder of it into the
// quizzes folder.
func (s *Server) adminDownloadQuiz(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return err
	}
	object, err := pyObject(data)
	if err != nil {
		return err
	}
	name, downloadPath := object["name"], object["download_path"]
	rawFolder := object["folder"]
	folder := ""
	if pyval.Truthy(rawFolder) {
		text, ok := rawFolder.(string)
		if !ok {
			return fmt.Errorf("expected str, bytes or os.PathLike object, not %s", pyval.KindOf(rawFolder))
		}
		folder = text
	}
	if !pyval.Truthy(name) || !pyval.Truthy(downloadPath) {
		return writeError(w, http.StatusBadRequest, "Name and download_path are required")
	}

	// A private archive needs an access token. The token belongs to the config
	// entry, so the address and the folder come from the config too: this way a
	// token can never go to an address that came with the request.
	token := ""
	if configured, ok := s.findDownloadableQuiz(name); ok && configured.AuthHeaderToken != "" {
		token = configured.AuthHeaderToken
		downloadPath = configured.DownloadPath
		folder = configured.Folder
		if !canSendTokenTo(configured.DownloadPath) {
			return writeError(w, http.StatusBadRequest, "A quiz token can only be sent over https")
		}
	}
	nameText, ok := name.(string)
	if !ok {
		return fmt.Errorf("'%s' object has no attribute 'replace'", pyval.KindOf(name))
	}
	address, ok := downloadPath.(string)
	if !ok {
		return fmt.Errorf("Request URL is missing an 'http://' or 'https://' protocol.")
	}

	folder, problem := s.checkDownloadFolder(folder)
	if problem != "" {
		return writeError(w, http.StatusBadRequest, problem)
	}

	s.log.Info("Downloading quiz from " + address)
	archive, status, err := s.fetchArchive(address, token)
	switch {
	case status != 0:
		s.log.Error(fmt.Sprintf("HTTP error downloading quiz: %d", status))
		return writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to download quiz: HTTP %d", status))
	case err != nil:
		s.log.Error(fmt.Sprintf("Network error downloading quiz: %v", err))
		return writeError(w, http.StatusInternalServerError, fmt.Sprintf("Network error: %v", err))
	}

	tempDir, err := os.MkdirTemp("", "webquiz-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	s.log.Info("Extracting ZIP to " + tempDir)
	count, err := extractZip(archive, tempDir)
	if errors.Is(err, errBadZip) {
		s.log.Error(fmt.Sprintf("Invalid ZIP file: %v", err))
		return writeError(w, http.StatusInternalServerError, "Downloaded file is not a valid ZIP archive")
	}
	if err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("ZIP contains %d files", count))

	source := filepath.Join(tempDir, folder)
	// Archives made by GitHub keep everything in one folder named after the
	// repository and the commit, so its name changes with every push. The
	// folder may be given inside that folder too.
	if _, err := os.Stat(source); err != nil {
		if dirs := subdirectories(tempDir); len(dirs) == 1 {
			candidate := filepath.Join(tempDir, dirs[0], folder)
			if _, err := os.Stat(candidate); err == nil {
				s.log.Info(fmt.Sprintf("Resolved folder '%s' inside archive root '%s'", folder, dirs[0]))
				source = candidate
			}
		}
	}
	if _, err := os.Stat(source); err != nil {
		available := []any{}
		entries, _ := os.ReadDir(tempDir)
		for _, entry := range entries {
			available = append(available, entry.Name())
		}
		message := fmt.Sprintf("Folder '%s' not found in ZIP archive. Available paths: %s", folder, pyval.Repr(available))
		s.log.Error("Validation error: " + message)
		return writeError(w, http.StatusBadRequest, message)
	}

	s.log.Info(fmt.Sprintf("Moving contents from %s to %s", source, s.quizzesDir))
	if err := copyTree(source, s.quizzesDir); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Quiz '%s' downloaded successfully", nameText),
	})
}

// fetchArchive downloads the archive. It returns the HTTP status when the
// answer is an error status (4xx or 5xx), and an error when the request
// itself failed. It follows redirects; the token is not sent to another
// origin, except when the same host moves from http to https.
func (s *Server) fetchArchive(address, token string) (archive []byte, status int, err error) {
	ctx, cancel := context.WithCancel(s.ctx) // aiohttp did not stop a handler when its client left
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: downloadTimeout}).DialContext,
			TLSHandshakeTimeout:   downloadTimeout,
			ResponseHeaderTimeout: downloadTimeout,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 20 {
				return errors.New("too many redirects")
			}
			first := via[0].URL
			upgrade := first.Scheme == "http" && req.URL.Scheme == "https" && first.Hostname() == req.URL.Hostname()
			if !upgrade && (req.URL.Scheme != first.Scheme || req.URL.Host != first.Host) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, response.StatusCode, nil
	}
	// Each wait for data may take downloadTimeout, like httpx's read timeout.
	idle := time.AfterFunc(downloadTimeout, cancel)
	defer idle.Stop()
	body, err := io.ReadAll(&idleReader{r: response.Body, timer: idle})
	return body, 0, err
}

// idleReader restarts a timer after every read, so a download stops when the
// other side goes quiet but not when it is only large.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
}

func (i *idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	i.timer.Reset(downloadTimeout)
	return n, err
}

// errBadZip marks an archive that is not a valid ZIP file.
var errBadZip = errors.New("bad zip file")

// extractZip writes the files of a ZIP archive into dir, like
// ZipFile.extractall: empty, "." and ".." parts of a name are dropped, so
// nothing is written outside dir. It returns the number of entries.
func extractZip(archive []byte, dir string) (int, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return 0, fmt.Errorf("%w: %v", errBadZip, err)
	}
	for _, entry := range reader.File {
		var parts []string
		for _, part := range strings.Split(entry.Name, "/") {
			if part != "" && part != "." && part != ".." {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			continue
		}
		target := filepath.Join(append([]string{dir}, parts...)...)
		if strings.HasSuffix(entry.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return 0, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		if err := extractEntry(entry, target); err != nil {
			return 0, err
		}
	}
	return len(reader.File), nil
}

func extractEntry(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: %v", errBadZip, err)
	}
	defer source.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, source); err != nil {
		out.Close()
		if errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrAlgorithm) {
			return fmt.Errorf("%w: %v", errBadZip, err)
		}
		return err
	}
	return out.Close()
}

// subdirectories lists the folders directly inside dir.
func subdirectories(dir string) []string {
	var names []string
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if info, err := os.Stat(filepath.Join(dir, entry.Name())); err == nil && info.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

// copyTree copies the contents of the folder src into dst, replacing files
// that exist (shutil.copytree with dirs_exist_ok=True).
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("[Errno 20] Not a directory: '%s'", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from, to := filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyTree(from, to); err != nil {
				return err
			}
			continue
		}
		content, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// isSafeFilename is _is_safe_filename: no path parts, no NUL, not too long.
func isSafeFilename(name string) bool {
	switch {
	case name == "", name == ".", name == "..":
		return false
	case strings.Contains(name, ".."), strings.ContainsAny(name, "/\\\x00"):
		return false
	}
	return len(name) <= 255
}

// serveQuizFile is GET /attach/{filename} (public, serve_quiz_file): a file
// of quizzes/attach, sent as a download.
func (s *Server) serveQuizFile(w http.ResponseWriter, r *http.Request) error {
	filename := r.PathValue("filename")
	if !isSafeFilename(filename) {
		return writeError(w, http.StatusBadRequest, "Invalid filename")
	}
	path := filepath.Join(s.quizzesDir, "attach", filename)
	info, err := os.Stat(path)
	if err != nil {
		return writeError(w, http.StatusNotFound, "File not found")
	}
	if !info.Mode().IsRegular() {
		return writeError(w, http.StatusBadRequest, "Not a file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeContent(w, r, filename, info.ModTime(), file)
	return nil
}
