// Command icons draws the WebQuiz icon of the desktop version: a white check
// mark on a blue (#2563eb) square with round corners. It writes every icon
// file the desktop version needs, so they can be made again after a change
// of the drawing. Run it from the root of the repository:
//
//	go run ./scripts/icons
//
// The files:
//
//	internal/desktop/icons/webquiz.ico        Windows: the tray icon and the Start menu shortcut (16, 32, 48 and 256 px)
//	internal/desktop/icons/tray.png           Linux: the tray icon (64 px)
//	internal/desktop/icons/tray-template.png  macOS: the menu bar icon, black on transparent (32 px, shown at 16 points)
//	packaging/macos/WebQuiz.iconset/*.png     macOS: the app icon; iconutil makes WebQuiz.icns from it
//
// The test checks that the committed files are the ones this script draws.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", ".", "the root of the repository")
	flag.Parse()
	for _, file := range iconFiles() {
		data, err := file.encode()
		if err == nil {
			err = os.MkdirAll(filepath.Join(*root, filepath.Dir(file.path)), 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(*root, file.path), data, 0o644)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", file.path, err)
			os.Exit(1)
		}
		fmt.Println("wrote", file.path)
	}
}

// iconFile is one file the script writes, by its path from the root of the
// repository: a PNG file of one image, or an .ico file of several sizes.
type iconFile struct {
	path    string
	image   func() *image.NRGBA // the image of a PNG file
	icoSize []int               // the sizes of an .ico file
}

// encode makes the content of the file.
func (f iconFile) encode() ([]byte, error) {
	if f.icoSize != nil {
		return encodeICO(f.icoSize...)
	}
	return encodePNG(f.image())
}

// macMargin is the empty border of the macOS app icon: Apple's icon grid
// puts the body (824 px) in the middle of 1024 px.
const macMargin = 100.0 / 1024

func iconFiles() []iconFile {
	files := []iconFile{
		{path: "internal/desktop/icons/webquiz.ico", icoSize: []int{16, 32, 48, 256}},
		{path: "internal/desktop/icons/tray.png", image: func() *image.NRGBA { return appIcon(64, 0) }},
		{path: "internal/desktop/icons/tray-template.png", image: func() *image.NRGBA { return templateIcon(32) }},
	}
	// The names and sizes iconutil needs.
	for _, s := range []struct {
		name string
		size int
	}{
		{"16x16", 16}, {"16x16@2x", 32},
		{"32x32", 32}, {"32x32@2x", 64},
		{"128x128", 128}, {"128x128@2x", 256},
		{"256x256", 256}, {"256x256@2x", 512},
		{"512x512", 512}, {"512x512@2x", 1024},
	} {
		size := s.size
		files = append(files, iconFile{
			path:  "packaging/macos/WebQuiz.iconset/icon_" + s.name + ".png",
			image: func() *image.NRGBA { return appIcon(size, macMargin) },
		})
	}
	return files
}

var (
	blue  = color.NRGBA{0x25, 0x63, 0xeb, 0xff}
	white = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	black = color.NRGBA{0, 0, 0, 0xff}
)

// The shape, in the square's own units (0 to 1 from the top left corner).
const (
	cornerRadius   = 0.22  // the round corners of the square
	checkHalfWidth = 0.068 // half the width of the check mark's line
)

// checkMark is the line of the check mark: down to the bottom point, then up.
var checkMark = [3][2]float64{{0.27, 0.52}, {0.43, 0.68}, {0.74, 0.36}}

// appIcon is the color icon: the white check mark on the blue square. margin
// is the empty border on each side, as a part of the size.
func appIcon(size int, margin float64) *image.NRGBA {
	return render(size, margin, func(inSquare, onCheck bool) (color.NRGBA, bool) {
		switch {
		case inSquare && onCheck:
			return white, true
		case inSquare:
			return blue, true
		}
		return color.NRGBA{}, false
	})
}

// templateIcon is the macOS menu bar icon: a black square with the check
// mark cut out. macOS colors it to suit a light or a dark menu bar.
func templateIcon(size int) *image.NRGBA {
	return render(size, 1.0/16, func(inSquare, onCheck bool) (color.NRGBA, bool) {
		return black, inSquare && !onCheck
	})
}

// samplesPerSide is how many points per pixel side render looks at, for
// smooth edges. Big icons need fewer: their edges are a small part of them.
func samplesPerSide(size int) int {
	switch {
	case size <= 64:
		return 8
	case size <= 256:
		return 4
	}
	return 2
}

// render draws size x size pixels. paint gives the color of a point from
// whether the point is in the square and on the check mark; false means
// the point is clear. A pixel near an edge gets the average of several
// points; any other pixel is one color, so one point is enough.
func render(size int, margin float64, paint func(inSquare, onCheck bool) (color.NRGBA, bool)) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	samples := samplesPerSide(size)
	border := margin * float64(size)
	side := float64(size) - 2*border
	// Every point of a pixel is less than 0.75 px from its middle. The edge
	// distances change by at most as much as the point moves, so a pixel
	// whose middle is farther from every edge than that is one color.
	nearEdge := 0.75 / side
	toSquare := func(v float64) float64 { return (v - border) / side }
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x, y := toSquare(float64(px)+0.5), toSquare(float64(py)+0.5)
			n := samples
			if math.Abs(squareEdge(x, y)) > nearEdge && math.Abs(checkEdge(x, y)) > nearEdge {
				n = 1
			}
			var r, g, b, covered float64
			for j := 0; j < n; j++ {
				for i := 0; i < n; i++ {
					x := toSquare(float64(px) + (float64(i)+0.5)/float64(n))
					y := toSquare(float64(py) + (float64(j)+0.5)/float64(n))
					c, ok := paint(squareEdge(x, y) <= 0, checkEdge(x, y) <= 0)
					if !ok {
						continue
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					covered++
				}
			}
			if covered == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(r / covered)),
				G: uint8(math.Round(g / covered)),
				B: uint8(math.Round(b / covered)),
				A: uint8(math.Round(255 * covered / float64(n*n))),
			})
		}
	}
	return img
}

// squareEdge is the distance from a point to the edge of the square with
// round corners: below zero inside, above zero outside.
func squareEdge(x, y float64) float64 {
	qx := math.Abs(x-0.5) - (0.5 - cornerRadius)
	qy := math.Abs(y-0.5) - (0.5 - cornerRadius)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - cornerRadius
}

// checkEdge is the distance from a point to the edge of the check mark's
// line (with round ends): below zero on the line, above zero off it.
func checkEdge(x, y float64) float64 {
	return math.Min(distance(x, y, checkMark[0], checkMark[1]), distance(x, y, checkMark[1], checkMark[2])) - checkHalfWidth
}

// distance is the distance from a point to the line segment a-b.
func distance(x, y float64, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	t := ((x-a[0])*dx + (y-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(x-(a[0]+t*dx), y-(a[1]+t*dy))
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeICO writes a Windows .ico file with the color icon in the given
// sizes. Sizes below 256 are 32-bit BMP images, which every Windows tool
// reads; 256 is a PNG image, as Windows Vista and newer expect.
func encodeICO(sizes ...int) ([]byte, error) {
	var images [][]byte
	for _, size := range sizes {
		img := appIcon(size, 0)
		var data []byte
		if size >= 256 {
			var err error
			if data, err = encodePNG(img); err != nil {
				return nil, err
			}
		} else {
			data = encodeBMP(img)
		}
		images = append(images, data)
	}

	var buf bytes.Buffer
	write := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	write(uint16(0))          // reserved
	write(uint16(1))          // 1: an icon
	write(uint16(len(sizes))) // the number of images
	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		side := uint8(size)
		if size >= 256 {
			side = 0 // 0 means 256
		}
		write(side)                   // width
		write(side)                   // height
		write(uint8(0))               // no palette
		write(uint8(0))               // reserved
		write(uint16(1))              // color planes
		write(uint16(32))             // bits per pixel
		write(uint32(len(images[i]))) // the size of the image data
		write(uint32(offset))         // where the image data starts
		offset += len(images[i])
	}
	for _, data := range images {
		buf.Write(data)
	}
	return buf.Bytes(), nil
}

// encodeBMP writes img as the BMP image of an icon: a BITMAPINFOHEADER with
// twice the height (the color rows and the AND mask), the color rows from
// the bottom up in blue, green, red, alpha order, then the AND mask (a set
// bit marks a clear pixel), each mask row filled up to 4 bytes.
func encodeBMP(img *image.NRGBA) []byte {
	size := img.Bounds().Dx()
	maskRow := (size + 31) / 32 * 4
	var buf bytes.Buffer
	write := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	write(uint32(40))                         // the header size
	write(int32(size))                        // width
	write(int32(2 * size))                    // height of the colors and the mask together
	write(uint16(1))                          // color planes
	write(uint16(32))                         // bits per pixel
	write(uint32(0))                          // no compression
	write(uint32(size*size*4 + maskRow*size)) // the size of the image data
	write(int32(0))                           // pixels per meter, across
	write(int32(0))                           // pixels per meter, down
	write(uint32(0))                          // no palette
	write(uint32(0))                          // all colors are important
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			c := img.NRGBAAt(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	for y := size - 1; y >= 0; y-- {
		row := make([]byte, maskRow)
		for x := 0; x < size; x++ {
			if img.NRGBAAt(x, y).A == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		buf.Write(row)
	}
	return buf.Bytes()
}
