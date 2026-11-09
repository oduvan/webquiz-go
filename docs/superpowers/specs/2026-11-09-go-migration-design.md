# WebQuiz in Go — migration design

Date: 2026-11-09. Source: `oduvan/webquiz` at v1.19 (commit `367a99d`). Target: `oduvan/webquiz-go`.

## Why and what

Oleksandr wants WebQuiz rewritten in Go, in the new repository `oduvan/webquiz-go`:

- The server becomes Go.
- The pages become **React + Tailwind**. The look may change; the features stay.
- **Zero tests lost.** Every one of the 911 Python test ids (`migration/python-tests.txt`) gets a ported test.
- The other parts come along: the user docs and the PDF books (uk/en), GitHub Pages, and the CI/CD that tests and builds the release binaries.

The Python repository stays as it is. Nothing in it changes.

## Goals

1. Same features as WebQuiz 1.19 (see the Python `CLAUDE.md` and `README.md` — they are the feature list).
2. Same HTTP API: the same paths, methods, JSON fields, status codes and error texts (uk/en). A page or script written for the Python API keeps working.
3. Same files on disk: the same `webquiz.yaml` keys, the same quiz YAML format, the same CSV file names and columns, logs in the same place. A teacher can stop the Python version, start the Go binary in the same folder, and keep going.
4. One binary per platform with the pages built in. No Python, no Node at run time.
5. Every Python test id has a ported test, and a test in CI proves it.

## Non-goals

- No new features. Bugs found on the way are fixed only when the fix is obvious and small; each one is listed in the final report.
- No PyPI package (Oleksandr's choice: binaries only).
- The Python repository and the `webquiz-ansible` repository are not changed (the second is out of reach). The final report tells Oleksandr that the Ansible playbook must switch from the pip package to the Linux ARM binary.

## Anti-goals (it fails even if it "works")

- A ported test that no longer checks what the Python test checked (for example, a test that only checks a status code where the Python test checked the stored CSV row).
- An existing quiz or config file that the Go binary reads differently from the Python version without saying so.
- Quiz text or student names shown as HTML (the "plain text, never HTML" rule).
- Pages that only work at `/` and break under the tunnel prefix `{base_url}/{socket_name}/`.

## Decisions

### Checker code runs in Starlark (Oleksandr's choice)

Text questions have a `checker` that today is Python code run with `exec`. The Go version runs it with **starlark-go** (`go.starlark.net`), a Python dialect written in Go.

Starlark lacks some Python syntax that real checkers use (`assert` everywhere, f-strings in the docs). So a small rewrite step turns Python-only syntax into Starlark before parsing. It works on tokens from a small Python tokenizer (strings, comments, brackets, line joins), never on raw text:

| Python | Becomes |
|---|---|
| `assert X` / `assert X, Y` (a whole logical line, also after `:` on one line) | `_assert_(X)` / `_assert_(X, lambda: Y)` — the message is only evaluated when the check fails, as in Python |
| f-strings (`f'...{x!r:>8.2f}...'`, all prefixes, nested) | string concatenation with `_fmt_(value, conversion, spec)`; `_fmt_` implements the Python format-spec mini-language (fill, align, sign, width, `,`/`_`, precision, types `d f e g % s x o b`) |
| `a ** b` | `pow(a, b)` (right-associative, binds tighter than unary minus on its left, as in Python) |
| `a < b < c` (any chain of comparisons, also `in`, `not in`, `is`, `is not`) | `(a < b and b < c)` |

The checker runs inside a wrapper function, so `user_answer = user_answer.strip()`, top-level `if`/`for`/`while` and helper `def`s work as in Python:

```
def _check_(user_answer):
    <checker code, every line indented one level, lines inside multi-line strings untouched>
_check_(_answer_)
```

Names available to the code: `user_answer`; `math` (`go.starlark.net/lib/math` plus `isclose`); the helpers `to_int`, `distance`, `direction_angle` (same results and the same Ukrainian/Russian error texts as `webquiz/checker.py`); the Python builtins that Starlark lacks: `sum`, `round`, `pow`, `map`, `filter`, `isinstance`, `format`. `print` does nothing. Options on: `while`, `set`, recursion. Limits: a step limit and a 2-second timeout, so `while True: pass` cannot hang the server (the Python version could hang).

Result: the answer is correct when the code ends without an error. `checker_error` is the assert message, or the error text without Starlark's file/line prefix. An `assert` without a message gives an empty error, as `str(AssertionError())` did. An empty checker keeps the exact-match rule with `correct_value`.

Saving a quiz runs the same rewrite plus `syntax.Parse`. The error text keeps the Python form: `Question N checker has invalid Python syntax: <message> (line L)`.

Known differences, written in the docs: no `try`/`except`, `raise`, `class`, `import`, `with`, `global`; `len()` and indexing of text count bytes, so non-ASCII text needs `.codepoints()`; `%` and `.format()` have no width or precision (f-strings do).

The docs example `assert abs(result - sqrt(16)) < 0.01` fails in Python too (`sqrt` is not defined); the docs change it to `math.sqrt`.

### Backend

- Go 1.26, module `github.com/oduvan/webquiz-go`, binary `webquiz`, entry `cmd/webquiz`.
- Libraries: `net/http` with Go 1.22 patterns, `gopkg.in/yaml.v3` (with `yaml.Node` for comment-keeping config edits, replacing ruamel), `github.com/coder/websocket`, `github.com/yuin/goldmark` (GFM tables and strikethrough, replacing mistune), `go.starlark.net`, `golang.org/x/crypto/ssh` (tunnel, replacing asyncssh). Everything else from the standard library.
- Package layout (the builder may refine it):

  | Package | Holds | Python source |
  |---|---|---|
  | `internal/config` | config structs, defaults, loading, validation, comment-keeping partial updates | `config.py`, `_validate_config_data` |
  | `internal/checker` | Starlark runner, Python rewrite, helpers | `checker.py`, `_execute_checker` |
  | `internal/quiz` | quiz loading and validation, points rounding, random order with sticky groups, answer checking | parts of `server.py` |
  | `internal/timelimits` | time limit rules | `time_limits.py` |
  | `internal/explanations` | Markdown to safe HTML, image names and renames | `explanations.py` |
  | `internal/i18n` | uk/en texts | `translations.py` |
  | `internal/tunnel` | SSH tunnel | `tunnel.py` |
  | `internal/transfer` | file transfer between servers | `transfer.py` |
  | `internal/server` | state, handlers, WebSockets, CSV, pages | `server.py` |
  | `internal/version` | version, "binary was replaced" check | `__init__.py`, `get_file_version` |
  | `cmd/webquiz` | CLI, daemon | `cli.py`, `binary_entry.py` |
  | `web` | React app; `embed.go` embeds `web/dist` | `templates/` |

- **Concurrency.** aiohttp ran everything on one thread. Go handlers run in parallel, so one mutex guards the quiz state. Handlers do their state work under the lock and release it before slow work (WebSocket sends, HTTP downloads, file writes that may be slow). The Python rule "store the answer result before the live stats broadcast" stays.
- **JSON numbers.** Request bodies are decoded with `UseNumber`, so `1` and `1.0` stay different where Python's checks tell them apart (`correct_answer`, `time_limit`, `points`).
- **Files next to the binary.** The Go binary does what the Python binary did in `binary_entry.py`: it sets `WEBQUIZ_BINARY_DIR` to the executable's folder and changes into it, so `webquiz.yaml`, `quizzes/`, `data/`, `logs/` and `static/` sit next to the program. One small difference for tests and services: when `WEBQUIZ_BINARY_DIR` is already set, the binary uses that folder instead (the Python config code already reads this variable).
- **Browser.** As in `cli.py`, the admin page opens in a browser after start when `WEBQUIZ_IS_BINARY=1`. Like `binary_entry.py`, the Go binary sets it to `1`, but only when it is unset, so tests set `WEBQUIZ_IS_BINARY=0`.
- **CLI.** The same flags as `cli.py` (`-d/--daemon`, `--stop`, `--status`, `--config`, `--port`, `--quizzes-dir`, `--master-key`, `--logs-dir`, `--csv-dir`, `--static`, `--url-format`, `--version`), no new ones. `--version` prints the real version (Python printed a fixed `1.0.0`). The daemon starts a detached copy of itself (no `fork` in Go); on Windows `-d` says it is not supported.
- **Version.** A `VERSION` file at the repo root (starts at `2.0`) is embedded at build time; the release workflow updates it. "Restart required" (the package-updated check) compares the running version with the version found in the executable file on disk, so replacing the binary while it runs shows the banner. The admin "new version" check asks the GitHub releases API of `oduvan/webquiz-go` instead of PyPI.
- **Settings for tests.** `WEBQUIZ_TIME_LIMIT_GRACE_SECONDS` and `WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL` stay as environment variables for the binary, but the server also takes them as options, so in-process tests can run in parallel.

### Frontend

- `web/`: Vite + React 19 + TypeScript + Tailwind CSS v3 (see "Calls made after the design review"). One Vite build with five HTML entries: quiz (`index.html`), admin, files, live stats, CSV table. Shared code for the theme switch, the API client, texts and UI parts.
- **Data in the page.** Where the Python templates had `{{PLACEHOLDERS}}`, the Go server puts one JSON object into `<script id="webquiz-boot" type="application/json">` (with `<` written as `<`, like `json_for_script`), plus an escaped `<title>`. The server owns the exact shape (one Go struct per page) and documents it in `CLAUDE.md`.
- **The quiz page stays a generated file.** As now, loading a quiz writes `static_dir/index.html` (the built page with the quiz's boot data, questions without answers). `GET /` serves it.
- **Relative URLs on the quiz page.** The quiz page uses relative URLs for its API calls, assets and images (`api/...`, `./assets/...`, `imgs/...`), because the tunnel serves it under `{base_url}/{socket_name}/`. Admin pages (local network only) may use absolute URLs. The Go server serves the built assets for every page path that needs them.
- **The same ids where tests rely on them.** The new pages keep the element ids and classes the old tests use (`#submit-answer-btn`, `.quiz-option`, `[data-option-index]`, `#quiz-select`, `#admin-panel`, ...) wherever that costs nothing. Where the new page has no such element, the Playwright port uses the new one.
- All rules from the Python `CLAUDE.md` stay: mobile layout at ≤768px, dark/light theme in `localStorage` key `theme` shared by all pages, plain text for quiz text and student names, copy blocking on question text, image preloading, request retry with the connection banner, double-click guards.

### Tests

- **The list.** `migration/python-tests.txt` holds the 911 pytest ids, collected from v1.19 with `pytest --collect-only`.
- **The tag.** Every ported test carries the Python id it ports, in a comment or a table field: `// py: tests/test_admin_api.py::test_admin_auth_endpoint_with_valid_key`. One Go/TS test may port several ids (each tagged) when the Python tests were near copies; then the ported test checks everything each of them checked.
- **The proof.** `migration/ported_test.go` reads the list, scans `**/*_test.go`, `e2e/**/*.ts` and `harness/**/*.test.ts`, and fails for any id without a `py:` tag. It runs with `go test ./...`, so CI enforces it.
- **Where they go.**
  - Unit tests of Python modules → Go unit tests in the matching package.
  - HTTP tests (`custom_webquiz_server` + `requests`) → Go tests against an in-process `httptest` server with its own temp folders (`internal/server/testutil` mirrors `conftest.py`: `customServer(t, config, quizzes, env)`, `adminSession`, `registerStudent`). Tests that start the real CLI build the binary once in `TestMain`.
  - WebSocket tests → Go tests with a WebSocket client.
  - Selenium tests (`test_selenium_*.py`, `test_user_journey_selenium.py`, the browser tests in `test_admin_unite_quizzes.py` and `test_auto_advance.py`) → Playwright tests in TypeScript under `e2e/`, against the real binary started per test with a temp folder and a free port. Pin `@playwright/test` 1.63.0 (it matches the browsers installed in this environment).
  - `test_harness_checks.py` → tests of the TypeScript harness checks.
- **Extra check while building (not kept in CI):** the original Python HTTP tests, pointed at the Go binary by changing only the server start command in `conftest.py`, must pass too, except tests that read Python internals or the old HTML. This catches API differences the ports might miss.

### Classroom harness

`harness/` is ported to TypeScript with Playwright (`npx tsx harness/run.ts <scenario>`, `npx tsx harness/student.ts <name> <cmd>`), with the same scenarios, records (`events.jsonl`, screenshots), checks and report. `AGENT_PROMPT.md` and `harness/README.md` change to the new commands. It stays out of normal CI except its check tests.

### Docs and books

- `docs/uk` and `docs/en` stay the source of the PDF books and GitHub Pages. They change where the Go version differs: download and first start (no `pip`; Linux ARM for Raspberry Pi), the new screens (screenshots taken again from the new UI with Playwright), checker rules (Starlark notes above), release URLs (`oduvan/webquiz-go`). Both languages stay in sync.
- `README.md` and `CLAUDE.md` are rewritten for the Go project. `CLAUDE.md` keeps its "every change must include" rules, adapted (Go tests, Playwright tests, `go`/`npm` commands).
- `gh-pages/` (download page + Docsify) moves over with the new release URLs and the ARM downloads.

### CI/CD

- `test.yml` (pull requests and pushes to `master`): build the web app; `go vet` and `go test -race ./...` (includes the migration check); web type check and unit tests; Playwright e2e tests; harness check tests.
- `release.yml` (manual, version input): update `VERSION`, commit, tag; build the web app once; cross-compile with `CGO_ENABLED=0` for linux amd64/arm64/arm (`GOARM=6`), macOS amd64/arm64, Windows amd64; zip them with the old asset names (`webquiz-linux.zip`, `webquiz-macos-intel.zip`, `webquiz-macos-apple-silicon.zip`, `webquiz-windows.exe.zip`) plus `webquiz-linux-arm64.zip` and `webquiz-linux-arm.zip`; build the Windows installer with Inno Setup on a Windows runner; build the two PDF books with pandoc; create the GitHub release with the hand-written notes file if present; deploy GitHub Pages. No PyPI, no Ansible step.
- `deploy-pages.yml`: as now, version read from `VERSION`.

## Calls made after the design review

Rule from Oleksandr: the Python version answers how the system works. The Go version translates it and does not ask or invent. The points below are the few places where Go needs a decision, each taken from the Python behavior:

- **Where files live and the browser.** As in the Python binary (`binary_entry.py`), see "Files next to the binary" and "Browser" above. No new options.
- **Student browsers.** The 1.19 quiz page works on about Safari 13 and Chrome 80 (old classroom iPads and phones). Tailwind CSS v4 needs Safari 16.4+, so the frontend uses **Tailwind CSS v3**, and Vite builds JavaScript for `es2019`/`safari13`.
- **Public address.** The Go releases live in `github.com/oduvan/webquiz-go` (pages at `oduvan.github.io/webquiz-go/`). Code keeps this address in one constant (`internal/version`). The admin "new version" check asks the GitHub releases of that repository, because the Go binary is not on PyPI.
- **Checkers that Starlark cannot run.** Python checks checker syntax only when a quiz is saved. Go does the same with its own parser. When a quiz loads, Go also writes a warning to the log for each checker it cannot compile (Python would have run them). Nothing else changes.
- **Comparison set for checkers.** The checker task runs a set of checkers in the style teachers and AI write through the Python 1.19 sandbox, stores the results as a JSON fixture, and a Go test checks that Go gives the same results.
- **Platforms.** Go 1.26 needs macOS 12+ and Windows 10+. Linux builds: amd64, arm64, and 32-bit ARM with `GOARM=6` (`webquiz-linux-arm.zip`), so every Raspberry Pi that ran the pip package can run the binary.
- **Raspberry Pi chapter.** The pip package is gone, so the chapter shows the ARM binary in place of `pip install`, keeping the rest of the chapter.

## Approaches considered

- **Checkers:** a Python subset interpreter written for this project (most Python-like, most code to own); real CPython in WebAssembly (exact, +30 MB, slow first start); JavaScript (breaks every existing checker). Oleksandr chose **starlark-go**; the rewrite step above closes the gaps that real checkers hit.
- **Tests:** (a) port every test to Go/Playwright with tags and a CI check — chosen, because the new repo must stand alone; (b) keep running the Python suite against the Go binary — kept only as an extra check while building, because it needs Python and cannot test the new pages.
- **Frontend:** one single-page app with a router (one bundle, but the quiz page must stay small and work under a path prefix) versus Vite multi-page entries with shared code — chosen, because each page keeps its own URL and the quiz page stays light.

## Left to the builder

- Exact Go types, file split inside packages, helper names.
- The boot JSON shape per page (documented in `CLAUDE.md` once fixed).
- How the new pages look, as long as the rules above hold.
- Ordering of work and how it is split.
