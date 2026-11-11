package desktop

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FolderFacts are what DataFolder looks at.
type FolderFacts struct {
	Env        string                // WEBQUIZ_BINARY_DIR; "" when it is not set
	Executable string                // the program file, with symbolic links resolved
	OS         string                // runtime.GOOS
	Home       string                // the person's home folder
	Writable   func(dir string) bool // whether the program can make files in a folder (CanWrite)
}

// Folder is the data folder: where webquiz.yaml, quizzes/, data/, logs/ and
// static/ live.
type Folder struct {
	Path     string
	Fallback bool   // ~/Documents/WebQuiz is used instead of the folder next to WebQuiz.app
	Reason   string // why the fallback is used, for the log; "" without a fallback
}

// DataFolder chooses the data folder of the desktop version.
//
// WEBQUIZ_BINARY_DIR wins when it is set. Otherwise it is the folder of the
// program, as in the console version, except for the macOS app: its program
// is WebQuiz.app/Contents/MacOS/webquiz, so the data folder is the folder
// that holds WebQuiz.app. When macOS runs the app from its read-only copy
// (App Translocation), when that folder cannot be written, and when the app
// is in /Applications or ~/Applications (folders for programs, not for
// data), the data folder is ~/Documents/WebQuiz instead.
func DataFolder(f FolderFacts) Folder {
	if f.Env != "" {
		return Folder{Path: f.Env}
	}
	app, ok := appBundle(f.Executable)
	if f.OS != "darwin" || !ok {
		return Folder{Path: filepath.Dir(f.Executable)}
	}
	// macOS paths always use "/", so the "path" package reads them the same
	// on every system the tests run on.
	parent := path.Dir(app)
	var reason string
	switch {
	case strings.Contains(f.Executable, "/AppTranslocation/"):
		reason = "macOS runs WebQuiz.app from a read-only copy (App Translocation). Move WebQuiz.app into a folder of its own with Finder and open it again to keep the data next to it"
	case inFolder(parent, "/Applications") || inFolder(parent, path.Join(f.Home, "Applications")):
		reason = "WebQuiz.app is in the Applications folder (" + parent + "), which is for programs, not for data"
	case !f.Writable(parent):
		reason = "the folder " + parent + " that holds WebQuiz.app cannot be written"
	default:
		return Folder{Path: parent}
	}
	return Folder{Path: path.Join(f.Home, "Documents", "WebQuiz"), Fallback: true, Reason: reason}
}

// appBundle returns the .app folder when exe is <name>.app/Contents/MacOS/<program>.
func appBundle(exe string) (string, bool) {
	macOS := path.Dir(exe)
	contents := path.Dir(macOS)
	app := path.Dir(contents)
	if path.Base(macOS) != "MacOS" || path.Base(contents) != "Contents" || !strings.HasSuffix(strings.ToLower(app), ".app") {
		return "", false
	}
	return app, true
}

// inFolder tells whether dir is root or inside it. macOS names are not case
// sensitive, so neither is this.
func inFolder(dir, root string) bool {
	dir, root = strings.ToLower(dir), strings.ToLower(root)
	return dir == root || strings.HasPrefix(dir, root+"/")
}

// CanWrite tells whether the program can make files in dir: it makes a small
// file and removes it.
func CanWrite(dir string) bool {
	file, err := os.CreateTemp(dir, ".webquiz-write-check-*")
	if err != nil {
		return false
	}
	name := file.Name()
	file.Close()
	os.Remove(name)
	return true
}
