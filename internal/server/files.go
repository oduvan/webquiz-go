package server

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
)

// The file manager API (server.py files_list, files_view, files_download,
// files_save_quiz). The page itself (/files/) is in pages.go.

// maxViewSize is the largest file the view shows (10 MB); bigger files are
// for download only.
const maxViewSize = 10 * 1024 * 1024

// listedFile is a file in the lists of GET /api/files/list.
type listedFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // the modification time, ISO format, local time
	Type     string `json:"type"`     // "log", "csv" or "yaml"
}

// filesList is GET /api/files/list: the files of logs_dir, csv_dir and
// quizzes_dir with their size and modification time, newest first.
func (s *Server) filesList(w http.ResponseWriter, r *http.Request) error {
	var lists [3][]listedFile
	for i, dir := range []struct{ path, fileType string }{{s.logsDir, "log"}, {s.csvDir, "csv"}, {s.quizzesDir, "yaml"}} {
		files, err := listFilesInDirectory(dir.path, dir.fileType)
		if err != nil {
			return err
		}
		lists[i] = files
	}
	return writeJSON(w, http.StatusOK, struct {
		Logs    []listedFile `json:"logs"`
		CSV     []listedFile `json:"csv"`
		Quizzes []listedFile `json:"quizzes"`
	}{lists[0], lists[1], lists[2]})
}

// listFilesInDirectory is _list_files_in_directory: the files (not the
// folders) of a folder, sorted by the modification time text, newest first.
// A missing folder has no files.
func listFilesInDirectory(dir, fileType string) ([]listedFile, error) {
	files := []listedFile{}
	if _, err := os.Stat(dir); err != nil {
		return files, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(dir, entry.Name())) // follows links, like os.path.isfile
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, listedFile{
			Name:     entry.Name(),
			Size:     info.Size(),
			Modified: isoformat(fromTimestamp(info.ModTime())),
			Type:     fileType,
		})
	}
	// Python sorted the ISO texts with reverse=True, which keeps the order of
	// equal times.
	slices.SortStableFunc(files, func(a, b listedFile) int { return strings.Compare(b.Modified, a.Modified) })
	return files, nil
}

// fromTimestamp is datetime.fromtimestamp(os.stat(path).st_mtime): Python
// made a float of the seconds and rounded it to microseconds (half to even).
func fromTimestamp(t time.Time) time.Time {
	seconds := float64(t.Unix()) + float64(t.Nanosecond())*1e-9
	whole, fraction := math.Modf(seconds)
	micro := math.RoundToEven(fraction * 1e6)
	if micro >= 1e6 {
		micro -= 1e6
		whole++
	} else if micro < 0 {
		micro += 1e6
		whole--
	}
	return time.Unix(int64(whole), int64(micro)*1000)
}

// isSafeFilename is _is_safe_filename: no path, no ".." anywhere, no NUL,
// at most 255 characters.
func isSafeFilename(filename string) bool {
	switch {
	case filename == "":
		return false
	case strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\`):
		return false
	case strings.ContainsRune(filename, 0):
		return false
	case filename == "." || filename == "..":
		return false
	case utf8.RuneCountInString(filename) > 255:
		return false
	}
	return true
}

// fileForType is _get_file_path_and_validate: the path of a file of the
// file manager, or the error answer (written to w) when the type or name is
// wrong or the file is missing.
func (s *Server) fileForType(w http.ResponseWriter, fileType, filename string) (string, bool) {
	var dir string
	switch fileType {
	case "csv":
		dir = s.csvDir
	case "logs":
		dir = s.logsDir
	case "quizzes":
		dir = s.quizzesDir
	default:
		_ = writeError(w, http.StatusBadRequest, "Invalid file type")
		return "", false
	}
	if !isSafeFilename(filename) {
		_ = writeError(w, http.StatusBadRequest, "Invalid filename")
		return "", false
	}
	path := filepath.Join(dir, filename)
	info, err := os.Stat(path)
	if err != nil {
		_ = writeError(w, http.StatusNotFound, "File not found")
		return "", false
	}
	if !info.Mode().IsRegular() {
		_ = writeError(w, http.StatusBadRequest, "Path is not a file")
		return "", false
	}
	return path, true
}

// filesView is GET /api/files/{type}/view/{filename}: a text file (at most
// 10 MB, UTF-8) as plain text.
func (s *Server) filesView(w http.ResponseWriter, r *http.Request) error {
	filename := r.PathValue("filename")
	path, ok := s.fileForType(w, r.PathValue("type"), filename)
	if !ok {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if size := info.Size(); size > maxViewSize {
		return writeJSON(w, http.StatusBadRequest, struct {
			Error string `json:"error"`
			Size  int64  `json:"size"`
		}{fmt.Sprintf("File too large for viewing (>10MB). Size: %d bytes. Use download instead.", size), size})
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return writeError(w, http.StatusBadRequest, "File contains non-UTF-8 content. Use download instead.")
	}
	// Python read the file in text mode, which turns "\r\n" and "\r" into
	// "\n" (CSV files are written with "\r\n").
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, text)
	return nil
}

// filesDownload is GET /api/files/{type}/download/{filename}: the file as
// an attachment.
func (s *Server) filesDownload(w http.ResponseWriter, r *http.Request) error {
	fileType, filename := r.PathValue("type"), r.PathValue("filename")
	path, ok := s.fileForType(w, fileType, filename)
	if !ok {
		return nil
	}
	contentType := "text/plain"
	switch fileType {
	case "csv":
		contentType = "text/csv"
	case "quizzes":
		contentType = "text/yaml"
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Type", contentType)
	setETag(w, info)
	http.ServeContent(w, r, filename, info.ModTime(), file)
	return nil
}

// filesSaveQuiz is PUT /api/files/quizzes/save/{filename}: it saves the
// YAML text of an existing quiz file after checking it, keeps the old file
// as "<name>.backup", and restarts the quiz when it is the running one.
//
// Python compared the file name with the path of the running quiz, so it
// never restarted the quiz; the restart follows CLAUDE.md ("active quiz
// reload") and the quiz editor (admin_update_quiz), which restart the
// running quiz with switch_quiz after a save.
func (s *Server) filesSaveQuiz(w http.ResponseWriter, r *http.Request) error {
	filename := r.PathValue("filename")
	data, err := readJSON(r)
	if err != nil {
		return err // Python did not catch this error: 500
	}
	fields, err := pyObject(data)
	if err != nil {
		return err
	}
	raw, ok := fields["content"]
	if !ok {
		raw = ""
	}
	text, isText := raw.(string)
	if !isText {
		return fmt.Errorf("'%s' object has no attribute 'strip'", pyval.KindOf(raw))
	}
	content := pyval.Strip(text)
	if content == "" {
		return writeError(w, http.StatusBadRequest, "Content is required")
	}
	if !isSafeFilename(filename) {
		return writeError(w, http.StatusBadRequest, "Invalid filename")
	}
	quizPath := filepath.Join(s.quizzesDir, filename)
	if _, err := os.Stat(quizPath); err != nil {
		return writeError(w, http.StatusNotFound, "Quiz file not found")
	}

	parsed, err := pyval.LoadYAML([]byte(content))
	if err != nil {
		return writeError(w, http.StatusBadRequest, "Неправильний YAML: "+err.Error())
	}
	if problems := quiz.Validate(parsed); len(problems) > 0 {
		return writeJSON(w, http.StatusBadRequest, struct {
			Error  string   `json:"error"`
			Errors []string `json:"errors"`
		}{"Неправильна структура даних квізу", problems})
	}

	backupPath := quizPath + ".backup"
	if err := copyWithMetadata(quizPath, backupPath); err != nil {
		return err
	}
	if err := writeFileSynced(quizPath, []byte(content)); err != nil {
		return err
	}

	// The full paths are compared: a running quiz in a subfolder
	// (sub/a.yaml) is another file than the top-level a.yaml saved here.
	var running string
	s.withLock(func() { running = s.currentQuizFile })
	if running != "" && running == quizPath {
		s.log.Info(fmt.Sprintf("Reloading active quiz after edit: %s", filename))
		if err := s.SwitchQuiz(filename, nil); err != nil {
			return err
		}
	}
	s.log.Info(fmt.Sprintf("Saved quiz file: %s", filename))
	return writeJSON(w, http.StatusOK, struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Backup  string `json:"backup"`
	}{true, fmt.Sprintf(`Quiz "%s" saved successfully (backup created)`, filename), backupPath})
}

// copyWithMetadata is shutil.copy2: the content, the permission bits and
// the modification time.
func copyWithMetadata(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	if err := os.Chmod(to, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}

// writeFileSynced replaces the content of an existing file and waits until
// it is on the disk (flush and fsync, for SD cards).
func writeFileSynced(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
