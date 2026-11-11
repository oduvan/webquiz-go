package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// root is the root of the repository, where the committed icons are.
const root = "../.."

// The committed icon files are the ones the script draws. After a change of
// the drawing, run "go run ./scripts/icons" from the root of the repository.
func TestCommittedIconsMatchTheScript(t *testing.T) {
	for _, file := range iconFiles() {
		committed, err := os.ReadFile(filepath.Join(root, file.path))
		if err != nil {
			t.Errorf("%s: %v (run go run ./scripts/icons)", file.path, err)
			continue
		}
		if file.icoSize != nil {
			sameICO(t, file.path, committed, file.icoSize)
		} else {
			samePNG(t, file.path, committed, file.image())
		}
	}
}

// The files that the script writes can be read back as the same images.
func TestEncode(t *testing.T) {
	for _, file := range iconFiles() {
		if file.path != "internal/desktop/icons/webquiz.ico" && file.path != "internal/desktop/icons/tray.png" {
			continue // the big PNG files are slow to compress with the race detector
		}
		data, err := file.encode()
		if err != nil {
			t.Fatalf("%s: %v", file.path, err)
		}
		if file.icoSize != nil {
			sameICO(t, file.path, data, file.icoSize)
		} else {
			samePNG(t, file.path, data, file.image())
		}
	}
}

// The files and sizes each system needs.
func TestIconSizes(t *testing.T) {
	sizes := map[string]int{
		"internal/desktop/icons/tray.png":                     64,
		"internal/desktop/icons/tray-template.png":            32,
		"packaging/macos/WebQuiz.iconset/icon_16x16.png":      16,
		"packaging/macos/WebQuiz.iconset/icon_16x16@2x.png":   32,
		"packaging/macos/WebQuiz.iconset/icon_32x32.png":      32,
		"packaging/macos/WebQuiz.iconset/icon_32x32@2x.png":   64,
		"packaging/macos/WebQuiz.iconset/icon_128x128.png":    128,
		"packaging/macos/WebQuiz.iconset/icon_128x128@2x.png": 256,
		"packaging/macos/WebQuiz.iconset/icon_256x256.png":    256,
		"packaging/macos/WebQuiz.iconset/icon_256x256@2x.png": 512,
		"packaging/macos/WebQuiz.iconset/icon_512x512.png":    512,
		"packaging/macos/WebQuiz.iconset/icon_512x512@2x.png": 1024,
	}
	files := map[string]bool{}
	for _, file := range iconFiles() {
		files[file.path] = true
	}
	for path, size := range sizes {
		if !files[path] {
			t.Errorf("the script does not make %s", path)
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Error(err)
			continue
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Errorf("%s is %dx%d, want %dx%d", path, b.Dx(), b.Dy(), size, size)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "internal/desktop/icons/webquiz.ico"))
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, entry := range readICO(t, data) {
		got = append(got, entry.size)
	}
	if want := []int{16, 32, 48, 256}; !reflect.DeepEqual(got, want) {
		t.Errorf("webquiz.ico has the sizes %v, want %v", got, want)
	}
}

// The macOS menu bar icon is a template: only black, with any transparency,
// so macOS can color it for a light or a dark menu bar. It has both opaque
// and clear parts.
func TestTemplateIconIsBlackOnTransparent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "internal/desktop/icons/tray-template.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	opaque, clear := 0, 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if r != 0 || g != 0 || bl != 0 {
				t.Fatalf("pixel %d,%d is not black: %v", x, y, img.At(x, y))
			}
			switch a {
			case 0xffff:
				opaque++
			case 0:
				clear++
			}
		}
	}
	if opaque == 0 || clear == 0 {
		t.Errorf("%d opaque and %d clear pixels; want both", opaque, clear)
	}
}

// samePNG checks that a PNG file holds the image the script draws.
func samePNG(t *testing.T, path string, data []byte, drawn *image.NRGBA) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	if !sameImage(img, drawn) {
		t.Errorf("%s differs from what the script draws: run go run ./scripts/icons", path)
	}
}

// sameICO checks that an .ico file holds the images the script draws: BMP
// images byte by byte, the PNG image by its pixels.
func sameICO(t *testing.T, path string, data []byte, sizes []int) {
	t.Helper()
	entries := readICO(t, data)
	if len(entries) != len(sizes) {
		t.Errorf("%s has %d images, the script makes %d", path, len(entries), len(sizes))
		return
	}
	for i, entry := range entries {
		drawn := appIcon(sizes[i], 0)
		switch {
		case entry.size != sizes[i] || entry.isPNG != (sizes[i] >= 256):
			t.Errorf("%s image %d: %d px (PNG %v), the script makes %d px", path, i, entry.size, entry.isPNG, sizes[i])
		case entry.isPNG:
			samePNG(t, path, entry.data, drawn)
		case !bytes.Equal(entry.data, encodeBMP(drawn)):
			t.Errorf("%s: the %d px image differs from what the script draws: run go run ./scripts/icons", path, entry.size)
		}
	}
}

func sameImage(a image.Image, b *image.NRGBA) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	if nrgba, ok := a.(*image.NRGBA); ok {
		return bytes.Equal(nrgba.Pix, b.Pix)
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return false
			}
		}
	}
	return true
}

type icoEntry struct {
	size  int
	isPNG bool
	data  []byte
}

// readICO reads the images of an .ico file. A BMP image must have the
// header of a 32-bit image with an AND mask (height counted twice).
func readICO(t *testing.T, data []byte) []icoEntry {
	t.Helper()
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:]) != 0 || binary.LittleEndian.Uint16(data[2:]) != 1 {
		t.Fatalf("not an .ico file")
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	var entries []icoEntry
	for i := 0; i < count; i++ {
		dir := data[6+16*i:]
		size := int(dir[0])
		if size == 0 {
			size = 256
		}
		if int(dir[1]) != int(dir[0]) || binary.LittleEndian.Uint16(dir[6:]) != 32 {
			t.Fatalf("image %d: height %d, width %d, %d bits", i, dir[1], dir[0], binary.LittleEndian.Uint16(dir[6:]))
		}
		length := int(binary.LittleEndian.Uint32(dir[8:]))
		offset := int(binary.LittleEndian.Uint32(dir[12:]))
		if offset+length > len(data) {
			t.Fatalf("image %d is outside the file", i)
		}
		image := data[offset : offset+length]
		entry := icoEntry{size: size, isPNG: bytes.HasPrefix(image, []byte("\x89PNG")), data: image}
		if !entry.isPNG {
			width := int(int32(binary.LittleEndian.Uint32(image[4:])))
			height := int(int32(binary.LittleEndian.Uint32(image[8:])))
			bits := binary.LittleEndian.Uint16(image[14:])
			maskRow := (size + 31) / 32 * 4
			if binary.LittleEndian.Uint32(image[0:]) != 40 || width != size || height != 2*size || bits != 32 || length != 40+size*size*4+maskRow*size {
				t.Fatalf("image %d has a wrong BMP header: width %d height %d bits %d length %d", i, width, height, bits, length)
			}
		}
		entries = append(entries, entry)
	}
	return entries
}
