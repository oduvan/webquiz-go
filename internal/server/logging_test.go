package server

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Only a file that lstat finds counts as taken. Any other result counts as
// free (like Python's os.path.exists), so the next step, opening the file,
// reports the real error instead of a search that never ends.
func TestFirstFreeLogPathCountsErrorsAsFree(t *testing.T) {
	lstat := func(path string) (os.FileInfo, error) {
		switch filepath.Base(path) {
		case "0001.log", "0002.log":
			return nil, nil // found
		case "0003.log":
			return nil, &fs.PathError{Op: "lstat", Path: path, Err: syscall.EACCES}
		}
		return nil, fs.ErrNotExist
	}
	if got := firstFreeLogPath("logs", lstat); got != filepath.Join("logs", "0003.log") {
		t.Errorf("got %s, want logs/0003.log", got)
	}
	none := func(path string) (os.FileInfo, error) { return nil, fs.ErrNotExist }
	if got := firstFreeLogPath("logs", none); got != filepath.Join("logs", "0001.log") {
		t.Errorf("got %s, want logs/0001.log", got)
	}
}

// A log file that cannot be made is an error at start, not an endless
// search: here the logs folder path is so long that the file path is too
// long for Linux (lstat and open both fail with ENAMETOOLONG).
func TestOpenLogReportsAnErrorInsteadOfSearching(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses Linux's path length limit (4096)")
	}
	const folderLength = 4090 // the folder can be made, "<folder>/0001.log" is too long
	dir := t.TempDir()
	for remaining := folderLength - len(dir); remaining > 0; {
		size := min(200, remaining-1)
		if remaining-(size+1) == 1 {
			size-- // never leave room for a name of length 0
		}
		dir = filepath.Join(dir, strings.Repeat("d", size))
		remaining -= size + 1
	}
	if len(dir) != folderLength {
		t.Fatalf("folder path has %d characters, want %d", len(dir), folderLength)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skipf("cannot make the long folder here: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, out, _, err := openLog(dir, nil)
		if out != nil {
			out.close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, syscall.ENAMETOOLONG) {
			t.Errorf("error %v, want ENAMETOOLONG", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("openLog did not return: it kept looking for a free number")
	}
}
