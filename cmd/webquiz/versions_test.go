package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The console version (no build tag) is the program as it was: it does not
// link the tray library, on any system. The desktop version (-tags desktop)
// does, so this test cannot pass by accident.
func TestOnlyTheDesktopVersionLinksTheTrayLibrary(t *testing.T) {
	for _, goos := range []string{"linux", "windows", "darwin"} {
		if deps := listDeps(t, goos, ""); strings.Contains(deps, "fyne.io/systray") {
			t.Errorf("the console version for %s links fyne.io/systray", goos)
		}
	}
	for _, goos := range []string{"linux", "windows"} {
		if deps := listDeps(t, goos, "desktop"); !strings.Contains(deps, "\nfyne.io/systray\n") {
			t.Errorf("the desktop version for %s does not link fyne.io/systray:\n%s", goos, deps)
		}
	}
}

// listDeps is "go list -deps ./cmd/webquiz" for a system and build tags.
func listDeps(t *testing.T, goos, tags string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-tags", tags, ".")
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0")
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list for %s (tags %q): %v\n%s", goos, tags, err, exit.Stderr)
		}
		t.Fatal(err)
	}
	return "\n" + string(output)
}
