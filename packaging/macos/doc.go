// Package macos holds the files of the macOS desktop app, WebQuiz.app:
// Info.plist (a template: the Makefile fills in __VERSION__) and
// WebQuiz.iconset (drawn by scripts/icons; iconutil makes WebQuiz.icns from
// it). "make dist-macos-app" builds the app from them; it runs only on a
// Mac. The Go code here is only the test of Info.plist.
package macos
