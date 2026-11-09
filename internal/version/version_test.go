package version

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	webquiz "github.com/oduvan/webquiz-go"
)

// py: tests/test_config_loading.py::test_get_package_version
func TestGetPackageVersion(t *testing.T) {
	// Python returned "unknown" or a version that starts with a digit.
	if Version == "" {
		t.Fatal("Version is empty")
	}
	if Version != "unknown" && (Version[0] < '0' || Version[0] > '9') {
		t.Errorf("Version should start with a digit, got %q", Version)
	}
}

func TestVersionComesFromTheVersionFile(t *testing.T) {
	if want := strings.TrimSpace(webquiz.VersionFile); Version != want {
		t.Errorf("Version = %q, the VERSION file says %q", Version, want)
	}
	if strings.ContainsAny(Version, " \n\r\t") {
		t.Errorf("Version should be trimmed, got %q", Version)
	}
}

func TestRepositoryAddresses(t *testing.T) {
	if RepoURL != "https://github.com/oduvan/webquiz-go" {
		t.Errorf("RepoURL = %q", RepoURL)
	}
	if PagesURL != "https://oduvan.github.io/webquiz-go/" {
		t.Errorf("PagesURL = %q", PagesURL)
	}
}

// fakeBinary writes a shell script that acts like "webquiz --version".
func fakeBinary(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	path := filepath.Join(t.TempDir(), "webquiz")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileVersionReadsTheBinaryOnDisk(t *testing.T) {
	path := fakeBinary(t, `if [ "$1" = "--version" ]; then echo "webquiz 9.9.1"; fi`+"\n")
	got, err := FileVersion(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "9.9.1" {
		t.Errorf("FileVersion = %q, want 9.9.1", got)
	}
}

func TestFileVersionUsesTheFirstLine(t *testing.T) {
	path := fakeBinary(t, "printf 'webquiz 2.1\\nmore text\\n'\n")
	got, err := FileVersion(path)
	if err != nil || got != "2.1" {
		t.Errorf("FileVersion = %q, %v; want 2.1", got, err)
	}
}

func TestFileVersionRejectsOtherOutput(t *testing.T) {
	for _, script := range []string{
		"echo 'something else 1.0'\n",
		"echo ''\n",
		"echo 'webquiz'\n",
		"exit 3\n",
	} {
		path := fakeBinary(t, script)
		if got, err := FileVersion(path); err == nil {
			t.Errorf("script %q: FileVersion = %q, want an error", script, got)
		}
	}
}

func TestFileVersionMissingFile(t *testing.T) {
	if _, err := FileVersion(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file should give an error")
	}
}

func TestFileVersionStopsASlowBinary(t *testing.T) {
	old := FileVersionTimeout
	FileVersionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { FileVersionTimeout = old })

	path := fakeBinary(t, "exec sleep 5\n")
	start := time.Now()
	if _, err := FileVersion(path); err == nil {
		t.Error("a binary that does not answer should give an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("FileVersion waited %v, the timeout is %v", elapsed, FileVersionTimeout)
	}
}
