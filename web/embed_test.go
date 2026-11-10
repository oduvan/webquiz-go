package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"path"
	"strings"
	"testing"
)

// The build writes name.gz next to the scripts and styles (web/build/gzip.ts),
// and the server sends that copy to browsers that accept gzip. Gzip always
// makes a script or style of 1 KB or more smaller, so such a file without a
// copy means that the build step did not run. Every copy must be smaller than
// its file and unpack to exactly the same bytes.
func TestBuildHasGzipCopies(t *testing.T) {
	dist, err := fs.Sub(Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	bigFiles := 0
	err = fs.WalkDir(dist, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		switch path.Ext(name) {
		case ".js", ".css":
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() < 1024 {
				return nil
			}
			bigFiles++
			if _, err := fs.Stat(dist, name+".gz"); err != nil {
				t.Errorf("%s (%d bytes) has no gzip copy %s.gz", name, info.Size(), name)
			}
		case ".gz":
			plain, err := fs.ReadFile(dist, strings.TrimSuffix(name, ".gz"))
			if err != nil {
				t.Errorf("%s is a copy of a file that is not in the build: %v", name, err)
				return nil
			}
			packed, err := fs.ReadFile(dist, name)
			if err != nil {
				return err
			}
			if len(packed) >= len(plain) {
				t.Errorf("%s has %d bytes, its file only %d", name, len(packed), len(plain))
			}
			reader, err := gzip.NewReader(bytes.NewReader(packed))
			if err != nil {
				t.Errorf("%s is not gzip: %v", name, err)
				return nil
			}
			unpacked, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(unpacked, plain) {
				t.Errorf("%s does not unpack to its file (error %v)", name, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if bigFiles == 0 {
		t.Fatal("the build has no scripts or styles: run make web")
	}
}
