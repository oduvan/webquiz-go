# WebQuiz

WebQuiz is a quiz system for a local network. A teacher runs one program on a computer. Students open a web page in their browsers and answer the questions. Nothing goes to the internet, so all results stay in your own network.

This is version 2.0, written in Go. It is one program file with no other parts to install. The older Python version (1.19) is at [oduvan/webquiz](https://github.com/oduvan/webquiz).

## What it can do

- Quizzes are plain YAML files. You can edit them by hand or in the visual editor of the admin panel.
- Question types: one correct answer, several correct answers, and a text answer checked by a small piece of code.
- Pictures, files for download, explanations in Markdown, points (also decimals), and time limits.
- Random question order for each student.
- Optional approval of each student by the teacher.
- Live statistics page that shows how every student is doing.
- Results are saved as CSV files. You can open them in Excel or Google Sheets.
- Quiz page in Ukrainian or English.
- Small downloads: the scripts and styles of the pages are sent gzip-compressed, about 70% smaller. This helps on slow Wi-Fi, for example with a Raspberry Pi.
- Optional SSH tunnel to reach the quiz from outside the local network, and sending files to another WebQuiz server.

## Download and run

WebQuiz comes in two versions of the same program:

- **Desktop version**, for a teacher's computer. WebQuiz shows an icon in the system tray (on macOS, in the menu bar). Its menu shows the address for students and opens the admin panel, live stats and the file manager; **Quit** stops WebQuiz and writes the answers that are not saved yet. There is no console window.
- **Console version**, for servers, a Raspberry Pi without a screen and the command line. It runs in a terminal and stops with Ctrl+C.

1. Open the [Releases](https://github.com/oduvan/webquiz-go/releases) page and download the file for your computer.
   - Desktop version:
     - Windows 10 or newer: `webquiz-windows-setup.exe` (installer, Start menu shortcut).
     - macOS 12 or newer: `webquiz-macos-app.zip` (`WebQuiz.app`, one app for Apple Silicon and Intel).
     - Linux: `webquiz-linux-desktop.zip` (64-bit PC; needs a desktop with a system tray, such as KDE, XFCE, Cinnamon, MATE or Ubuntu; plain GNOME needs the AppIndicator extension).
   - Console version:
     - Windows 10 or newer: `webquiz-windows.exe.zip` (portable).
     - macOS 12 or newer: `webquiz-macos-apple-silicon.zip` (M1 and newer) or `webquiz-macos-intel.zip`.
     - Linux: `webquiz-linux.zip` (64-bit PC), `webquiz-linux-arm64.zip` (Raspberry Pi 3, 4, 5 with a 64-bit system) or `webquiz-linux-arm.zip` (older Raspberry Pi boards).
2. Unzip a zip file (run the Windows installer as it is). On Linux and for the macOS console version, run `chmod +x` on the file. On macOS, move `WebQuiz.app` into a folder of its own (for example `Documents/WebQuiz`) and open it the first time with right-click > **Open**, because it is not signed by Apple.
3. Start the program. The admin panel opens at `http://localhost:8080/admin/`. Students open `http://<your-computer-address>:8080/`; the desktop version shows this address in the first line of its menu.

WebQuiz keeps its files (`webquiz.yaml`, `quizzes/`, `data/`, `logs/`, `static/`) next to the program. `WebQuiz.app` keeps them in the folder that holds it, or in `~/Documents/WebQuiz` when it runs from the Applications folder or from a read-only copy that macOS made. Set the environment variable `WEBQUIZ_BINARY_DIR` to use another folder.

Useful options: `--port`, `--master-key`, `--config`, `-d` (run in the background; console version only, not on Windows), `--stop`, `--status`, `--version`. Run `webquiz --help` for the full list.

The full user guide is in [docs/en](docs/en) (English) and [docs/uk](docs/uk) (Ukrainian). It is also online at <https://oduvan.github.io/webquiz-go/>.

## Build from source

You need Go 1.26 or newer and Node.js 22 or newer.

```bash
make build        # builds the web pages, then bin/webquiz
./bin/webquiz --master-key secret123
make build-desktop   # the desktop version (tray icon) for this computer: bin/webquiz-desktop
```

The desktop version is the same code built with the Go build tag `desktop`. Linux and Windows build it without cgo; macOS needs cgo, so `make dist-macos-app` builds `WebQuiz.app` only on a Mac.

The Go program embeds the built web pages, so `make web` must run before any Go build or test. `make build` and `make test` do this for you.

## Tests

```bash
make test         # go vet and go test -race ./...
make test-web     # TypeScript check and Vitest unit tests of web/
make e2e          # Playwright tests of the real pages against the real program
go test ./migration/   # only the check that every Python test id is ported (make test runs it too)
npm --prefix harness ci && npm --prefix harness test   # unit tests of the classroom harness checks
```

Go tests start servers in the same process on a free port. Playwright tests start the built program on a free port in a new temporary folder. No test uses a fixed port.

To retake the screenshots of the docs: `make build`, then `npm --prefix scripts ci` and `npm --prefix scripts run screenshots`.

## Project layout

| Folder | What is in it |
|--------|---------------|
| `cmd/webquiz/` | The program: command-line options, daemon mode, start steps; `main_console.go` and `main_desktop.go` for the two versions |
| `internal/desktop/` | The desktop version: the tray menu, the data folder of `WebQuiz.app`, the icons |
| `internal/server/` | The HTTP server: student API, admin API, WebSockets, CSV, pages |
| `internal/quiz/` | Reading, checking and scoring quizzes |
| `internal/checker/` | Checker code of text questions (Starlark) |
| `internal/config/` | `webquiz.yaml` |
| `internal/pyval/` | Python-like YAML and value rules, so 1.19 files read the same |
| `internal/i18n/` | Texts in Ukrainian and English |
| `internal/timelimits/`, `internal/explanations/` | Time limits, Markdown explanations |
| `internal/tunnel/`, `internal/transfer/` | SSH tunnel and file transfer between servers |
| `internal/netinfo/`, `internal/version/` | Network addresses, version and repository address |
| `internal/testutil/` | Helpers that start servers for tests |
| `web/` | React and Tailwind pages (quiz, admin, files, live stats, CSV table) |
| `e2e/` | Playwright tests |
| `harness/` | Classroom test harness: a real server, real browsers, scripted and AI agent students (see `harness/README.md`) |
| `scripts/` | Screenshot script for the docs; `scripts/icons` draws the icons of the desktop version |
| `migration/` | List of the 911 Python tests and the check that all are ported |
| `installer/` | Inno Setup script of the Windows installer |
| `packaging/macos/` | `Info.plist` and the icon set of `WebQuiz.app` |
| `docs/` | User guide in `en/` and `uk/`, and `imgs/` |
| `gh-pages/` | Download page and online docs (Docsify) |
