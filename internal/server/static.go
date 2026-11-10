package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// serveIndexPage is serve_index_page: the quiz page file static_dir/index.html.
func (s *Server) serveIndexPage(w http.ResponseWriter, r *http.Request) error {
	file, err := os.Open(filepath.Join(s.staticDir, "index.html"))
	if err != nil {
		writeText(w, http.StatusNotFound, "404: Not Found")
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setETag(w, info)
	http.ServeContent(w, r, "index.html", info.ModTime(), file)
	return nil
}

// setETag sets aiohttp's ETag for a file: modification time in nanoseconds
// and size, in hex. A page written twice in one second still gets a new tag
// (Last-Modified alone only has whole seconds).
func setETag(w http.ResponseWriter, info os.FileInfo) {
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
}

// serveStatic is the static route at "/" (aiohttp's add_static("/",
// static_dir)): files of static_dir; a folder is 403, as aiohttp did
// without show_index.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) error {
	s.serveFolder(w, r, s.staticDir, strings.TrimPrefix(r.URL.Path, "/"), "")
	return nil
}

// serveImages is the /imgs/ route (add_static("/imgs/", quizzes/imgs,
// show_index=True)): image files, and a listing for folders.
func (s *Server) serveImages(w http.ResponseWriter, r *http.Request) error {
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/imgs"), "/")
	s.serveFolder(w, r, filepath.Join(s.quizzesDir, "imgs"), name, "/imgs")
	return nil
}

// serveFolder serves the file name (a slash-separated path) from dir. Names
// that leave dir, also through a symbolic link, are not found. listPrefix is
// the URL prefix for folder listings; "" means folders are forbidden.
func (s *Server) serveFolder(w http.ResponseWriter, r *http.Request, dir, name, listPrefix string) {
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		name = "."
	}
	if !fs.ValidPath(name) {
		writeText(w, http.StatusNotFound, "404: Not Found")
		return
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		writeText(w, http.StatusNotFound, "404: Not Found")
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		writeText(w, http.StatusNotFound, "404: Not Found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeText(w, http.StatusNotFound, "404: Not Found")
		return
	}
	if info.IsDir() {
		if listPrefix == "" {
			writeText(w, http.StatusForbidden, "403: Forbidden")
			return
		}
		entries, err := file.ReadDir(-1)
		if err != nil {
			writeText(w, http.StatusForbidden, "403: Forbidden")
			return
		}
		slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		writeHTML(w, http.StatusOK, []byte(directoryListing(listPrefix, name, entries)))
		return
	}
	setETag(w, info)
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// directoryListing is aiohttp's _directory_as_html: "Index of /<folder>" and
// a link for every entry, folders with a "/" after the name.
func directoryListing(prefix, folder string, entries []os.DirEntry) string {
	indexOf := "Index of /" + escapeHTMLAttr(folder)
	var items []string
	for _, entry := range entries { // sorted by name, as Python's sorted() did
		relative := entry.Name()
		if folder != "." {
			relative = folder + "/" + relative
		}
		link := (&url.URL{Path: prefix + "/" + relative}).EscapedPath()
		label := entry.Name()
		if entry.IsDir() {
			label += "/"
		}
		items = append(items, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, link, escapeHTMLAttr(label)))
	}
	ul := "<ul>\n" + strings.Join(items, "\n") + "\n</ul>"
	body := "<body>\n<h1>" + indexOf + "</h1>\n" + ul + "\n</body>"
	head := "<head>\n<title>" + indexOf + "</title>\n</head>"
	return "<html>\n" + head + "\n" + body + "\n</html>"
}

// serveAssets serves the files of the web build at /assets/ (the quiz page
// asks for ./assets/, the admin pages for /assets/). Their names carry a
// hash of the content, so browsers may keep them for good. A name that is
// not in the build is looked up in static_dir, as before the pages were built in.
func (s *Server) serveAssets(w http.ResponseWriter, r *http.Request) error {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !fs.ValidPath(name) {
		return s.serveStatic(w, r)
	}
	file, err := s.pages.Open(name)
	if err != nil {
		return s.serveStatic(w, r)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return s.serveStatic(w, r)
	}
	content, ok := file.(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	})
	if !ok {
		return errors.New("the web build cannot seek its files")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, info.Name(), time.Time{}, content)
	return nil
}
