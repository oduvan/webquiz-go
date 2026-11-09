// Package migration checks that every test of the Python version of WebQuiz
// has a ported test in this repository.
//
// python-tests.txt lists the 911 pytest ids of WebQuiz 1.19. A ported test
// says which Python test it ports with a tag "py: <id>", in a comment or in a
// table field. This test fails for every id that has no tag anywhere.
package migration

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tagPattern finds "py: <id>" tags. An id has no spaces except inside the
// square brackets of a parametrized test, as in
// "tests/test_transfer.py::test_send_validation[room-a-files1-name of this server]".
var tagPattern = regexp.MustCompile(`py:\s*["'` + "`" + `]?(tests/[^\s\[\]"'` + "`" + `]+(?:\[[^\]]*\])?)`)

// skipDirs are folders that never hold our own test files.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	".superpowers": true,
}

// isTestFile tells which files may hold ported tests.
func isTestFile(path string) bool {
	slashed := filepath.ToSlash(path)
	switch {
	case strings.HasSuffix(slashed, "_test.go"):
		return true
	case strings.HasPrefix(slashed, "e2e/") && strings.HasSuffix(slashed, ".ts"):
		return true
	case strings.HasPrefix(slashed, "harness/") && strings.HasSuffix(slashed, ".test.ts"):
		return true
	case strings.HasPrefix(slashed, "web/") && (strings.HasSuffix(slashed, ".test.ts") || strings.HasSuffix(slashed, ".test.tsx")):
		return true
	}
	return false
}

// findTags walks root and returns every tagged Python id with the files that tag it.
func findTags(root string) (map[string][]string, error) {
	tags := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDirs[entry.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !isTestFile(relative) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range tagPattern.FindAllStringSubmatch(string(content), -1) {
			tags[match[1]] = append(tags[match[1]], filepath.ToSlash(relative))
		}
		return nil
	})
	return tags, err
}

// readIDs reads the list of Python test ids, one per line.
func readIDs(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var ids []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, scanner.Err()
}

// missingIDs returns the ids that have no tag, sorted.
func missingIDs(ids []string, tags map[string][]string) []string {
	var missing []string
	for _, id := range ids {
		if len(tags[id]) == 0 {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}

func TestEveryPythonTestIsPorted(t *testing.T) {
	// While the migration is in progress the check only runs on request.
	// Task 20 of docs/superpowers/plans/2026-11-09-go-migration.md removes this skip.
	if os.Getenv("WEBQUIZ_REQUIRE_ALL_PORTED") != "1" {
		t.Skip("set WEBQUIZ_REQUIRE_ALL_PORTED=1 to check that every Python test is ported")
	}
	ids, err := readIDs("python-tests.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 911 {
		t.Fatalf("python-tests.txt should list 911 ids, it lists %d", len(ids))
	}
	tags, err := findTags("..")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	for id, files := range tags {
		if !known[id] {
			t.Errorf("%v tag an id that is not in python-tests.txt: %s", files, id)
		}
	}
	missing := missingIDs(ids, tags)
	if len(missing) > 0 {
		t.Errorf("%d of %d Python tests have no ported test (no \"py: <id>\" tag):\n%s",
			len(missing), len(ids), strings.Join(missing, "\n"))
	}
}

func TestScannerFindsTags(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The sample tags are built from parts, so the real scan of this
	// repository does not find them in this file.
	tag := "py" + ": "
	write("internal/a/a_test.go", "// "+tag+"tests/test_a.py::test_one\nfunc TestOne(t *testing.T) {}\n"+
		"var cases = []struct{ py string }{{py: \"tests/test_b.py::test_two[room-a-files1-name of this server]\"}}\n")
	write("e2e/tests/quiz.spec.ts", "// "+tag+"tests/test_c.py::TestClass::test_three\n")
	write("harness/checks.test.ts", "// "+tag+"tests/test_d.py::test_four\n")
	write("web/src/x.test.tsx", "// "+tag+"tests/test_e.py::test_five\n")
	write("internal/a/a.go", "// "+tag+"tests/test_not_a_test_file.py::test_x\n")
	write("node_modules/pkg/x_test.go", "// "+tag+"tests/test_skipped.py::test_y\n")

	tags, err := findTags(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tests/test_a.py::test_one",
		"tests/test_b.py::test_two[room-a-files1-name of this server]",
		"tests/test_c.py::TestClass::test_three",
		"tests/test_d.py::test_four",
		"tests/test_e.py::test_five",
	}
	for _, id := range want {
		if len(tags[id]) == 0 {
			t.Errorf("tag not found: %s (found %v)", id, tags)
		}
	}
	if len(tags) != len(want) {
		t.Errorf("found %d tags, want %d: %v", len(tags), len(want), tags)
	}
	missing := missingIDs(append(want, "tests/test_z.py::test_missing"), tags)
	if len(missing) != 1 || missing[0] != "tests/test_z.py::test_missing" {
		t.Errorf("missing = %v", missing)
	}
}

func TestPythonListIsComplete(t *testing.T) {
	ids, err := readIDs("python-tests.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 911 {
		t.Fatalf("python-tests.txt should list 911 ids, it lists %d", len(ids))
	}
	for _, id := range ids {
		if !tagPattern.MatchString("py: "+id) || tagPattern.FindStringSubmatch("py: " + id)[1] != id {
			t.Errorf("the tag pattern cannot read this id: %s", id)
		}
	}
}
