package version

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	webquiz "github.com/oduvan/webquiz-go"
)

// RepoURL is the address of the WebQuiz Go repository.
const RepoURL = "https://github.com/oduvan/webquiz-go"

// PagesURL is the address of the WebQuiz Go GitHub Pages site.
const PagesURL = "https://oduvan.github.io/webquiz-go/"

// Version is the version of the running program, like "2.0".
//
// It is declared without a value so that "-ldflags -X" can set it; when the
// build does not set it, init fills it from the embedded VERSION file.
var Version string

func init() {
	if Version == "" {
		Version = strings.TrimSpace(webquiz.VersionFile)
	}
}

// FileVersionTimeout is how long FileVersion waits for the binary to answer.
var FileVersionTimeout = 5 * time.Second

// FileVersion runs the WebQuiz binary at path with --version and returns the
// version from its first output line, which must read "webquiz <version>".
// It returns an error when the program cannot run, does not answer within
// FileVersionTimeout, or prints something else.
func FileVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), FileVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = time.Second // do not wait for pipes held by child processes
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s --version did not answer within %v", path, FileVersionTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("run %s --version: %w", path, err)
	}
	line, _, _ := bufio.NewReader(bytes.NewReader(output)).ReadLine()
	fields := strings.Fields(string(line))
	if len(fields) != 2 || fields[0] != "webquiz" {
		return "", fmt.Errorf("%s --version printed %q, expected \"webquiz <version>\"", path, string(line))
	}
	return fields[1], nil
}
