// Package desktop is the desktop version of WebQuiz (new in 2.0): the same
// server as the console version, with an icon in the system tray (the menu
// bar on macOS) instead of the console.
//
// The program is built in two versions from the same code:
//
//   - console (no build tag): the program as it always was. It does not link
//     the tray library (cmd/webquiz/versions_test.go checks this).
//   - desktop ("go build -tags desktop"): cmd/webquiz/main_desktop.go runs
//     the server with the tray icon. It never looks for a console.
//
// Everything that can be tested without a desktop has no build tag:
//
//   - menu.go: the menu as a function of the server state (Menu, Tooltip,
//     MessageText, ProblemText, StudentURL). The texts are in i18n.Tray.
//   - actions.go: the Tray interface, the Events the tray sends, and Actions,
//     which does what a picked item says (open a page, or stop the server
//     the way Ctrl+C does and remove the icon).
//   - folder.go: the data folder (DataFolder): next to the program, except
//     WebQuiz.app on macOS, whose data lives next to the app or, when that
//     is not possible, in ~/Documents/WebQuiz.
//   - recorder.go: log lines kept until the log file exists (Recorder), and
//     Go's standard log sent to the log file (UseLog).
//   - icons.go: the icons drawn by scripts/icons.
//
// Only the real tray (systray.go, fyne.io/systray) and the Windows message
// box (message_windows.go) need the build tag. The flow of the desktop
// version (start, menu, quit) is cmd/webquiz/desktop.go; its tests run it
// with a fake tray.
package desktop
