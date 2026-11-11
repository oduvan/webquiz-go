package desktop

import _ "embed"

// The icons are drawn by scripts/icons ("go run ./scripts/icons" from the
// root of the repository).
var (
	//go:embed icons/webquiz.ico
	iconICO []byte
	//go:embed icons/tray.png
	iconPNG []byte
	//go:embed icons/tray-template.png
	templatePNG []byte
)

// Icon is the tray icon for an operating system (runtime.GOOS): an .ico
// file for Windows, a PNG image for the others.
func Icon(goos string) []byte {
	if goos == "windows" {
		return iconICO
	}
	return iconPNG
}

// TemplateIcon is the macOS menu bar icon: black on transparent, so macOS
// can color it for a light or a dark menu bar.
func TemplateIcon() []byte {
	return templatePNG
}
