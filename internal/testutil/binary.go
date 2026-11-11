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

// program is one version of the program that the tests build once.
type program struct {
	tags string // the build tags: "" for the console version
	once sync.Once
	dir  string // the temp folder it is built into
	path string
	err  string
}

var (
	consoleProgram = &program{}
	desktopProgram = &program{tags: "desktop"}
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
	return consoleProgram.build(t)
}

// BuildDesktopBinary is BuildBinary for the desktop version (-tags desktop).
// With CGO_ENABLED=0 it builds for Linux and Windows only: the tray library
// needs cgo on macOS.
func BuildDesktopBinary(t testing.TB) string {
	t.Helper()
	return desktopProgram.build(t)
}

// RemoveBinary deletes the programs that BuildBinary and BuildDesktopBinary
// built, if any. Call it from TestMain after m.Run.
func RemoveBinary() {
	for _, p := range []*program{consoleProgram, desktopProgram} {
		if p.dir != "" {
			os.RemoveAll(p.dir)
		}
	}
}

func (p *program) build(t testing.TB) string {
	t.Helper()
	p.once.Do(p.compile)
	if p.err != "" {
		t.Fatal(p.err)
	}
	return p.path
}

func (p *program) compile() {
	root, err := moduleRoot()
	if err != nil {
		p.err = "BuildBinary: " + err.Error()
		return
	}
	p.dir, err = os.MkdirTemp("", "webquiz-test-bin-")
	if err != nil {
		p.err = "BuildBinary: " + err.Error()
		return
	}
	name := "webquiz"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(p.dir, name)
	cmd := exec.Command(goTool(), "build", "-tags", p.tags, "-o", path, "./cmd/webquiz")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		p.err = "BuildBinary: go build ./cmd/webquiz (tags \"" + p.tags + "\") failed (did you run \"make web\"?): " + err.Error() + "\n" + string(output)
		return
	}
	p.path = path
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
