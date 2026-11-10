package server_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
	"github.com/oduvan/webquiz-go/internal/version"
)

// The version check runs the program file on disk with --version. In a test
// the program file is the test binary, so these tests give the server a small
// script that answers like the program does. Python read __init__.py instead;
// the idea is the same: a file changed after the start shows up.

// writeProgram makes (or replaces) a script that prints "webquiz <version>".
// It replaces the file by renaming a new one over it, which works while the
// old file is running.
func writeProgram(t *testing.T, path, programVersion string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake program is a shell script")
	}
	temp := path + ".new"
	if err := os.WriteFile(temp, []byte("#!/bin/sh\necho \"webquiz "+programVersion+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp, path); err != nil {
		t.Fatal(err)
	}
}

func startWithProgram(t *testing.T, programVersion string) (*testutil.TestServer, string) {
	t.Helper()
	program := filepath.Join(t.TempDir(), "webquiz")
	writeProgram(t, program, programVersion)
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: server.Options{Executable: program}})
	return ts, program
}

func checkVersions(t *testing.T, ts *testutil.TestServer) map[string]any {
	t.Helper()
	res := ts.GetJSON(t, "/api/admin/version-check") // a local request needs no login
	if res.Status != 200 {
		t.Fatalf("status %d: %s", res.Status, res.Text())
	}
	return res.JSON(t)
}

// py: tests/test_version_check.py::test_version_check_endpoint_returns_versions
func TestVersionCheckReturnsVersions(t *testing.T) {
	t.Parallel()
	ts, _ := startWithProgram(t, version.Version)
	data := checkVersions(t, ts)
	for _, key := range []string{"running_version", "file_version", "restart_required"} {
		if _, ok := data[key]; !ok {
			t.Errorf("no %s in %v", key, data)
		}
	}
	if data["restart_required"] != false {
		t.Errorf("restart_required %v", data["restart_required"])
	}
}

// py: tests/test_version_check.py::test_version_check_returns_matching_versions
func TestVersionCheckMatchingVersions(t *testing.T) {
	t.Parallel()
	ts, _ := startWithProgram(t, version.Version)
	data := checkVersions(t, ts)
	if data["running_version"] != version.Version || data["file_version"] != version.Version || data["restart_required"] != false {
		t.Errorf("%v", data)
	}
}

// py: tests/test_version_check.py::test_version_check_endpoint_requires_local_network
func TestVersionCheckNeedsLocalNetwork(t *testing.T) {
	t.Parallel()
	ts, _ := startWithProgram(t, version.Version)
	if res := ts.GetJSON(t, "/api/admin/version-check"); res.Status != 200 {
		t.Errorf("local request: %d", res.Status)
	}
	// Python's test only tried the local request; the guard is the same as the
	// other local pages, so a public address is refused.
	if res := ts.GetJSON(t, "/api/admin/version-check", testutil.FromIP("8.8.8.8")); res.Status != 403 {
		t.Errorf("public address: %d", res.Status)
	}
}

// py: tests/test_version_check.py::test_get_file_version_function
func TestFileVersionFunction(t *testing.T) {
	t.Parallel()
	// get_file_version() became version.FileVersion: it runs the file.
	program := filepath.Join(t.TempDir(), "webquiz")
	writeProgram(t, program, version.Version)
	found, err := version.FileVersion(program)
	if err != nil || found == "" || found == "unknown" || found != version.Version {
		t.Errorf("FileVersion = %q, %v; running version %q", found, err, version.Version)
	}
}

// py: tests/test_version_check.py::test_get_file_version_parses_correctly
func TestFileVersionParsesCorrectly(t *testing.T) {
	t.Parallel()
	// Python compared with webquiz.__version__; here the file prints its
	// version on the first line, "webquiz <version>", and extra lines are ignored.
	program := filepath.Join(t.TempDir(), "webquiz")
	if runtime.GOOS == "windows" {
		t.Skip("the fake program is a shell script")
	}
	if err := os.WriteFile(program, []byte("#!/bin/sh\necho \"webquiz 7.8.9\"\necho \"more text\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if found, err := version.FileVersion(program); err != nil || found != "7.8.9" {
		t.Errorf("FileVersion = %q, %v", found, err)
	}
}

// py: tests/test_version_check.py::test_version_mismatch_detected_when_file_changed
func TestVersionMismatchDetectedWhenFileChanged(t *testing.T) {
	t.Parallel()
	ts, program := startWithProgram(t, version.Version)
	data := checkVersions(t, ts)
	if data["running_version"] != version.Version || data["file_version"] != version.Version || data["restart_required"] != false {
		t.Fatalf("at the start: %v", data)
	}

	// The program file is replaced while the server runs (an update).
	writeProgram(t, program, "99.99.99")
	data = checkVersions(t, ts)
	if data["running_version"] != version.Version || data["file_version"] != "99.99.99" || data["restart_required"] != true {
		t.Errorf("after the change: %v", data)
	}
}

// When the file cannot be read the answer says "unknown" and asks for no restart.
func TestVersionCheckUnknownFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "gone")
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: server.Options{Executable: missing}})
	data := checkVersions(t, ts)
	if data["file_version"] != "unknown" || data["restart_required"] != false || data["running_version"] != version.Version {
		t.Errorf("%v", data)
	}
}
