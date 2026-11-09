package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	buildDir  string
	buildPath string
	buildErr  string
)

// BuildBinary builds cmd/webquiz once per test binary (CGO_ENABLED=0, like a
// release) and returns the path of the program. Tests that start the real
// program (CLI tests) use it. The web pages must be built first ("make
// web"), as for every Go build of the program.
//
// The program is built into a new temp folder. A package that uses
// BuildBinary can remove it at the end with RemoveBinary from TestMain.
func BuildBinary(t testing.TB) string {
	t.Helper()
	buildOnce.Do(build)
	if buildErr != "" {
		t.Fatal(buildErr)
	}
	return buildPath
}

// RemoveBinary deletes the program that BuildBinary built, if any. Call it
// from TestMain after m.Run.
func RemoveBinary() {
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
}

func build() {
	root, err := moduleRoot()
	if err != nil {
		buildErr = "BuildBinary: " + err.Error()
		return
	}
	buildDir, err = os.MkdirTemp("", "webquiz-test-bin-")
	if err != nil {
		buildErr = "BuildBinary: " + err.Error()
		return
	}
	name := "webquiz"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(buildDir, name)
	cmd := exec.Command(goTool(), "build", "-o", path, "./cmd/webquiz")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		buildErr = "BuildBinary: go build ./cmd/webquiz failed (did you run \"make web\"?): " + err.Error() + "\n" + string(output)
		return
	}
	buildPath = path
}

// moduleRoot is the folder of go.mod.
func moduleRoot() (string, error) {
	output, err := exec.Command(goTool(), "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	gomod := strings.TrimSpace(string(output))
	if gomod == "" || gomod == os.DevNull {
		return "", os.ErrNotExist
	}
	return filepath.Dir(gomod), nil
}

// goTool is the go command (from PATH, as "go test" found it).
func goTool() string {
	return "go"
}
