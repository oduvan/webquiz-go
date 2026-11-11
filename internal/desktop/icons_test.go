package desktop

import (
	"bytes"
	"image/png"
	"testing"
)

// Windows takes an .ico file for the tray icon; Linux and macOS take a PNG
// image. The macOS menu bar uses the template icon.
func TestIcons(t *testing.T) {
	if ico := Icon("windows"); !bytes.HasPrefix(ico, []byte{0, 0, 1, 0}) {
		t.Errorf("the Windows icon is not an .ico file: % x", ico[:min(len(ico), 8)])
	}
	for _, goos := range []string{"linux", "darwin"} {
		img, err := png.Decode(bytes.NewReader(Icon(goos)))
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
			t.Errorf("%s icon is %v, want 64x64", goos, b)
		}
	}
	img, err := png.Decode(bytes.NewReader(TemplateIcon()))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Errorf("template icon is %v, want 32x32", b)
	}
}
