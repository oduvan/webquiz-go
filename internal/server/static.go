package server

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
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
// hash of the content, so browsers may keep them for good. The build has a
// gzip copy (name.gz) of most scripts and styles (web/build/gzip.ts): a
// browser that accepts gzip gets the copy, with Content-Encoding: gzip and
// the type of the file itself. A name that is not in the build is looked up
// in static_dir, as before the pages were built in.
func (s *Server) serveAssets(w http.ResponseWriter, r *http.Request) error {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !fs.ValidPath(name) {
		return s.serveStatic(w, r)
	}
	file, _, ok := openBuildFile(s.pages, name)
	if !ok {
		return s.serveStatic(w, r)
	}
	defer file.Close()
	content, err := seekable(file)
	if err != nil {
		return err
	}
	header := w.Header()
	if packed, packedSize, ok := openBuildFile(s.pages, name+".gz"); ok {
		defer packed.Close()
		header.Add("Vary", "Accept-Encoding")
		// ServeContent takes the type from the extension of the file's own
		// name, so the answer has the type of the file, not of gzip data.
		// When the extension names no type, ServeContent would look at the
		// gzip data instead, so such a file is sent as it is.
		if mime.TypeByExtension(path.Ext(name)) != "" && acceptsGzip(r.Header) {
			if content, err = seekable(packed); err != nil {
				return err
			}
			header.Set("Content-Encoding", "gzip")
			// ServeContent leaves Content-Length out when Content-Encoding is set.
			header.Set("Content-Length", strconv.FormatInt(packedSize, 10))
		}
	}
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, path.Base(name), time.Time{}, content)
	return nil
}

// openBuildFile opens a file (not a folder) of the web build and gives its size.
func openBuildFile(pages fs.FS, name string) (fs.File, int64, bool) {
	file, err := pages.Open(name)
	if err != nil {
		return nil, 0, false
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		file.Close()
		return nil, 0, false
	}
	return file, info.Size(), true
}

// seekable gives the file as an io.ReadSeeker, which ServeContent needs.
// The files of embed.FS and fstest.MapFS can seek.
func seekable(file fs.File) (io.ReadSeeker, error) {
	content, ok := file.(io.ReadSeeker)
	if !ok {
		return nil, errors.New("the web build cannot seek its files")
	}
	return content, nil
}

// acceptsGzip tells whether a request's Accept-Encoding allows gzip
// (RFC 9110, section 12.5.3). A "gzip" (or "x-gzip") entry decides with its
// weight: "gzip;q=0" means no. Without such an entry, a "*" entry decides the
// same way. No header, or only other codings such as "identity", means no.
func acceptsGzip(header http.Header) bool {
	gzipWeight, anyWeight := -1.0, -1.0 // -1: not in the header
	for _, value := range header.Values("Accept-Encoding") {
		for _, entry := range strings.Split(value, ",") {
			coding, params, _ := strings.Cut(entry, ";")
			switch strings.ToLower(strings.TrimSpace(coding)) {
			case "gzip", "x-gzip":
				gzipWeight = max(gzipWeight, codingWeight(params))
			case "*":
				anyWeight = max(anyWeight, codingWeight(params))
			}
		}
	}
	if gzipWeight >= 0 {
		return gzipWeight > 0
	}
	return anyWeight > 0
}

// codingWeight reads the weight ("q=0.5") from the parameters of one
// Accept-Encoding entry: 1 without a weight, 0 for a weight that is not a
// number above 0.
func codingWeight(params string) float64 {
	for _, param := range strings.Split(params, ";") {
		key, value, found := strings.Cut(param, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
			continue
		}
		weight, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || !(weight > 0) {
			return 0
		}
		return weight
	}
	return 1
}
