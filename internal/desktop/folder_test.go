package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The data folder rule: the console version and the desktop version on
// Windows and Linux keep their files next to the program; WebQuiz.app keeps
// them in the folder that holds WebQuiz.app, or in ~/Documents/WebQuiz when
// that folder cannot be used.
func TestDataFolder(t *testing.T) {
	writable := func(string) bool { return true }
	readOnly := func(string) bool { return false }
	const home = "/Users/olga"
	const fallback = "/Users/olga/Documents/WebQuiz"
	cases := []struct {
		name     string
		facts    FolderFacts
		want     string
		fallback bool
		reason   string // a part of the reason
	}{
		{
			name:  "inside a bundle in a folder of its own",
			facts: FolderFacts{Executable: "/Users/olga/Documents/WebQuiz/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:  "/Users/olga/Documents/WebQuiz",
		},
		{
			name:  "inside a bundle on the desktop",
			facts: FolderFacts{Executable: "/Users/olga/Desktop/Quiz/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:  "/Users/olga/Desktop/Quiz",
		},
		{
			name:     "translocated (run from the read-only copy macOS makes)",
			facts:    FolderFacts{Executable: "/private/var/folders/x1/abc/T/AppTranslocation/0F1E2D3C/d/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:     fallback,
			fallback: true,
			reason:   "App Translocation",
		},
		{
			name:     "in /Applications",
			facts:    FolderFacts{Executable: "/Applications/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:     fallback,
			fallback: true,
			reason:   "Applications",
		},
		{
			name:     "in a folder inside /Applications",
			facts:    FolderFacts{Executable: "/Applications/School/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:     fallback,
			fallback: true,
			reason:   "Applications",
		},
		{
			name:     "in /Applications, written in other letter case",
			facts:    FolderFacts{Executable: "/applications/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:     fallback,
			fallback: true,
			reason:   "Applications",
		},
		{
			name:     "in ~/Applications",
			facts:    FolderFacts{Executable: "/Users/olga/Applications/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:     fallback,
			fallback: true,
			reason:   "Applications",
		},
		{
			name:  "a folder whose name only starts like /Applications",
			facts: FolderFacts{Executable: "/Applications Old/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: writable},
			want:  "/Applications Old",
		},
		{
			name:     "a folder the program cannot write to",
			facts:    FolderFacts{Executable: "/Volumes/WebQuiz/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: readOnly},
			want:     fallback,
			fallback: true,
			reason:   "/Volumes/WebQuiz",
		},
		{
			name:  "WEBQUIZ_BINARY_DIR wins, even over a translocated app",
			facts: FolderFacts{Env: "/srv/webquiz", Executable: "/private/var/folders/x1/abc/T/AppTranslocation/0F1E2D3C/d/WebQuiz.app/Contents/MacOS/webquiz", OS: "darwin", Home: home, Writable: readOnly},
			want:  "/srv/webquiz",
		},
		{
			name:  "WEBQUIZ_BINARY_DIR on Linux",
			facts: FolderFacts{Env: "/srv/webquiz", Executable: "/opt/webquiz/webquiz-linux-desktop", OS: "linux", Home: "/home/olga", Writable: writable},
			want:  "/srv/webquiz",
		},
		{
			name:  "not a bundle on macOS (the console version)",
			facts: FolderFacts{Executable: "/Users/olga/WebQuiz/webquiz-macos-apple-silicon", OS: "darwin", Home: home, Writable: readOnly},
			want:  "/Users/olga/WebQuiz",
		},
		{
			name:  "a bundle path on Linux is not a bundle",
			facts: FolderFacts{Executable: "/home/olga/WebQuiz.app/Contents/MacOS/webquiz", OS: "linux", Home: "/home/olga", Writable: readOnly},
			want:  "/home/olga/WebQuiz.app/Contents/MacOS",
		},
		{
			name:  "the Linux desktop version",
			facts: FolderFacts{Executable: "/home/olga/WebQuiz/webquiz-linux-desktop", OS: "linux", Home: "/home/olga", Writable: readOnly},
			want:  "/home/olga/WebQuiz",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DataFolder(c.facts)
			if got.Path != c.want || got.Fallback != c.fallback {
				t.Errorf("DataFolder = %+v, want path %q, fallback %v", got, c.want, c.fallback)
			}
			if c.fallback && !strings.Contains(got.Reason, c.reason) {
				t.Errorf("reason %q does not mention %q", got.Reason, c.reason)
			}
			if !c.fallback && got.Reason != "" {
				t.Errorf("a reason without a fallback: %q", got.Reason)
			}
		})
	}
}

// The Windows desktop version uses the folder of webquiz.exe, as before.
func TestDataFolderOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows paths are read only on Windows")
	}
	got := DataFolder(FolderFacts{Executable: `C:\Program Files\WebQuiz\webquiz.exe`, OS: "windows", Home: `C:\Users\olga`, Writable: func(string) bool { return true }})
	if got.Path != `C:\Program Files\WebQuiz` || got.Fallback {
		t.Errorf("DataFolder = %+v", got)
	}
}

func TestCanWrite(t *testing.T) {
	dir := t.TempDir()
	if !CanWrite(dir) {
		t.Errorf("CanWrite(%s) = false for a new temp folder", dir)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("CanWrite left files behind: %v", entries)
	}
	if CanWrite(filepath.Join(dir, "missing")) {
		t.Error("CanWrite is true for a folder that does not exist")
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return // the administrator can write to a read-only folder
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	if CanWrite(locked) {
		t.Error("CanWrite is true for a read-only folder")
	}
}
