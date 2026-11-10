# CLAUDE.md

## CRITICAL: Every Code Change MUST Include

1. **Update CLAUDE.md** - Reflect architectural changes, new features, API endpoints, technical decisions. Keep this file MINIMAL - detailed user-facing info belongs in README.md
2. **Update README.md** - User-facing documentation. Only for new or changed features: a bug fix does not need a README change
3. **Update docs/** - Both Ukrainian (`docs/uk/`) and English (`docs/en/`) in sync, chapter by chapter. Only for new or changed features: a bug fix does not need a docs change. If a page looks different, retake the screenshots with `scripts/screenshots.ts`
4. **Write tests FIRST** - All functionality needs automated tests: Go tests for the server and packages, Vitest for logic in `web/src`, Playwright (`e2e/`) for what a person sees in a browser. A test that ports a Python 1.19 test carries its `py:` tag (see Testing). New tests do not need a `py:` tag
5. **Mobile support** - All UI must be responsive (<=768px viewport, `width: 100%; max-width: [size]`), and the Playwright mobile specs must pass

---

## Project Overview
WebQuiz 2.0 is the Go rewrite of the Python/aiohttp WebQuiz 1.19 (<https://github.com/oduvan/webquiz>): a quiz system with multi-quiz management, real-time WebSocket stats, admin approval workflow and question randomization. One static binary per platform, no Python. Same HTTP API, config keys, quiz format, CSV files and folders as 1.19, except what `docs` and `.github/release-notes/v2.0.md` list as differences.

## Architecture
- **Backend**: Go 1.26, `net/http` (Go 1.22 route patterns), middleware, `github.com/coder/websocket`. Module `github.com/oduvan/webquiz-go`, built with `CGO_ENABLED=0`
- **Frontend**: React 19 + TypeScript + Vite + Tailwind CSS v3 (not v4) in `web/`, built to `web/dist` and embedded in the binary (`web/embed.go`). Mobile layout at <=768px. Vite `base: './'`, `build.target: ['es2019', 'safari13']`
- **Storage**: In-memory -> CSV (5s flush), YAML configs, quiz state resets on switch
- **Auth**: Session cookies for admin (master key at login), automatic local network restriction
- **Runtime dependencies** (only these): `gopkg.in/yaml.v3`, `github.com/coder/websocket`, `github.com/yuin/goldmark`, `go.starlark.net`, `golang.org/x/crypto`. Anything else needs a reason in the commit message
- **Testing**: Go unit and integration tests (in-process servers), Vitest, Playwright

## Key Files and Packages
- `cmd/webquiz/` - CLI (flags, `--help` text of 1.19), daemon mode (not on Windows), start steps (base folder, browser opening)
- `internal/server/` - HTTP server. Read its `doc.go` first: file map and the rules for handlers
- `internal/quiz/` - read, check and score quizzes
- `internal/checker/` - checker code of text questions with Starlark. Its `doc.go` has the exact rules and differences from Python
- `internal/config/` - config structs and loading (`webquiz.yaml`)
- `internal/pyval/` - Python/PyYAML value rules (`LoadYAML`, `Equal`, `Str`, `Truthy`)
- `internal/i18n/` - student texts and API error texts (uk/en)
- `internal/timelimits/` - quiz time limit rules
- `internal/explanations/` - question explanations: Markdown -> safe HTML (goldmark)
- `internal/tunnel/` - SSH tunnel manager; `internal/transfer/` - file transfer between servers
- `internal/netinfo/`, `internal/version/` - network addresses; version and repository address (`RepoURL`, `PagesURL` live only here)
- `internal/testutil/` - `StartServer` (in-process server on a free port, temp folders), `BuildBinary`
- `web/` - five pages: `index.html` (quiz), `admin.html`, `files.html`, `live-stats.html`, `csv-table.html`; sources in `web/src/{quiz,admin,files,livestats,csvtable,shared}`; `web/build/gzip.ts` is the Vite plugin that writes the gzip copies
- `e2e/` - Playwright tests; `e2e/fixtures/server.ts` starts the built binary in a temp folder
- `scripts/screenshots.ts` - takes the screenshots of `docs/imgs` from the real binary
- `migration/` - `python-tests.txt` (911 ids of the 1.19 tests) and the check that every id is ported (a normal Go test)
- `harness/` - classroom test harness in TypeScript: a real server, real browsers, a scripted teacher, scripted and AI agent students, and teacher scenarios where an AI agent teacher uses `harness/teacher.ts` and starts automatic students (`npx tsx harness/run.ts <scenario>`, ports 8199 and 8198). `harness/matrix.ts` takes scripted students through all pairs of quiz settings and config and checks every screen against 1.19 (free ports, output in `harness/runs/<stamp>-matrix/`); checks of features new in 2.0 (the answer verdict) say "new in 2.0" and are skipped against a 1.19 server. Only the `harness/*.test.ts` files run as tests (`npm --prefix harness test`). See `harness/README.md`
- `installer/webquiz.iss` - Inno Setup script for the Windows installer
- `docs/uk/`, `docs/en/`, `docs/imgs/` - user guide (compiled to PDF with the version)
- `gh-pages/index.html` - download page template (version placeholder: `{{VERSION}}`); `gh-pages/docs/` - Docsify config (en/, uk/)
- `VERSION` - the version; `Makefile` - build, test, dist

## API Endpoints

Same paths, methods, JSON fields, status codes and error texts as 1.19.

**Public:**
- `POST /api/register` (optional `registration_token`: a repeat with the same token returns the existing user), `PUT /api/update-registration`, `POST /api/submit-answer` (repeat with the same answer returns the stored result; a different answer to an answered question -> 409; after a timeout any answer to that question returns the stored timeout; response has `test_completed` and, for a timeout, `timed_out: true`; `explanation_html` when the running quiz has `show_right_answer` and `show_explanation` on and the question has an explanation, never on a whole-quiz timeout), `POST /api/question-start` (ignored for an answered question; response adds `remaining_seconds` / `quiz_remaining_seconds` when a time limit applies), `GET /api/verify-user/{user_id}`
- `GET /attach/{filename}` - Download quiz file attachment (Content-Disposition: attachment)

**Admin (session cookie required, local network only):**
- `POST /api/admin/auth` - Authenticate with master key in request body (`{"master_key": "..."}`)
- `GET /api/admin/check-session`, `GET /api/admin/version-check`, `PUT /api/admin/approve-user`, `POST /api/admin/force-show-answers`, `GET /api/admin/list-quizzes`, `POST /api/admin/switch-quiz` (optional `settings`: start settings for this run; 400 with `errors` and no switch when invalid; response has the running `settings`), `PUT /api/admin/config` (accepts `{content: yaml}` or `{data: {section: {...}}}` for JSON partial update; response includes `config_content` and `config_data`)
- `POST /api/admin/preview-explanation` - Render explanation Markdown for the editor preview (`{"markdown": "..."}` -> `{"html": "..."}`)
- Quiz management: `GET /api/admin/quiz/{filename}`, `POST /api/admin/create-quiz`, `PUT /api/admin/quiz/{filename}`, `DELETE /api/admin/quiz/{filename}`, `POST /api/admin/download-quiz`, `POST /api/admin/unite-quizzes`, `POST /api/admin/validate-quiz` (checks quiz data without saving). A name with a folder part is refused
- Editor pickers: `GET /api/admin/list-images` (images in quizzes/imgs/), `GET /api/admin/list-files` (list files in quizzes/attach/)
- Checker templates: `GET /api/admin/list-checker-templates`
- File management: `GET /api/files/list`, `GET /api/files/{type}/view/{filename}`, `GET /api/files/{type}/download/{filename}`, `PUT /api/files/quizzes/save/{filename}`
- Tunnel management: `POST /api/admin/tunnel/connect`, `POST /api/admin/tunnel/disconnect`
- File transfer: `GET /api/admin/transfer`, `POST /api/admin/transfer/send` (`{to, files: [{type, name}]}`), `POST /api/admin/transfer/{request_id}/accept` (optional `{files: [{type, name, action: save|replace|rename|skip, new_name}]}`; 400 keeps the request open), `POST /api/admin/transfer/{request_id}/reject`

**Server-to-server (through the tunnel, protected by the offer token, not by IP):**
- `POST /api/transfer/offer`, `GET /api/transfer/download/{token}/{type}/{filename}`, `POST /api/transfer/result`. The JSON must stay byte-compatible with the Python server (1.19 and 2.0 servers can send files to each other)

**Pages (admin pages: local network only):**
- `GET /` quiz page; `GET /admin/`, `/live-stats/`, `/files/` (also without the final slash), `GET /files/csv/{filename}` (`?question=`, `?user=`, `?embedded=1`); `/assets/`, `/imgs/`

**WebSockets:**
- `WS /ws/live-stats` (local network only), `WS /ws/admin` (local network only: admin approval, tunnel status, `transfer_request`, `transfer_request_removed`, `transfer_result`)

## Boot Data Contract
Every HTML entry in `web/` has `<title>__WEBQUIZ_TITLE__</title>` and `<script id="webquiz-boot" type="application/json">__WEBQUIZ_BOOT__</script>`. The server replaces the title with HTML-escaped text and the marker with JSON where `<`, `>` and `&` are written as `\u003c`, `\u003e` and `\u0026` (so `</script>` in quiz text cannot end the script). Page data lives in `QuizBoot`, `AdminBoot`, `FilesBoot`, `LiveStatsBoot`, `CSVTableBoot` (`internal/server/pages.go`) and is read in `web/src/shared/boot.ts`. Asset URLs are relative on the quiz page (`./assets/`, `api/...`, `imgs/...`) because it is also served under `{base_url}/{socket_name}/`; admin pages may use absolute `/api/...` and the server rewrites their `./assets/` to `/assets/`.

## Dev Commands

```bash
make web          # npm ci + build the pages into web/dist (needed before any go build/test)
make build        # web + bin/webquiz
make test         # go vet + go test -race ./...
make test-web     # tsc --noEmit + Vitest
make e2e          # Playwright against the built binary (e2e/.bin/webquiz)
make dist         # all release binaries and zips in dist/
make release-check
go test ./migration/   # only the check that every Python test id is ported (make test runs it too)
gofmt -l .        # must print nothing (CI checks it)

./bin/webquiz --master-key secret123    # run
./bin/webquiz -d                        # daemon (not on Windows)
```

Screenshots of the docs: `make build && npm --prefix scripts ci && npm --prefix scripts run screenshots`.

### Environment Requirements
- Go 1.26+, Node.js 22+
- Minimum systems of the binaries: macOS 12+, Windows 10+ (Go requirement). Linux: amd64, arm64, 32-bit ARM (`GOARM=6`)
- Environment variables: `WEBQUIZ_MASTER_KEY`, `WEBQUIZ_BINARY_DIR`, `WEBQUIZ_IS_BINARY`, and for tests only `WEBQUIZ_TIME_LIMIT_GRACE_SECONDS`, `WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL`

## Testing
- **Ports**: no test uses a fixed port. `testutil.StartServer` and the Playwright fixture pick a free port. CLI tests that run the real binary also use a free port
- **Go**: in-process servers with `internal/testutil` (temp folders, `ServerOptions.FlushInterval` instead of sleeping 5 s for CSV). Run with `-race`
- **`py:` tags**: a test that ports a Python 1.19 test has `// py: <id>` right above it (or a `py:` field in a table case), with the id copied exactly from `migration/python-tests.txt`. It checks what the Python test checked; where the new UI makes that impossible, it checks the same behavior another way and a comment says why. TypeScript tests use the same comment. `migration/ported_test.go` fails when an id has no tag
- **Playwright** (`e2e/`): each test starts its own server through `fixtures/server.ts` (`WEBQUIZ_BINARY_DIR` = a temp folder, `WEBQUIZ_IS_BINARY=0`), so tests run in parallel. `@playwright/test` is pinned to 1.63.0
- Tests must not depend on the developer's shell: the fixture removes `WEBQUIZ_MASTER_KEY` and the time limit variables

## Technical Decisions
- **Concurrency**: one `sync.Mutex` (`s.mu`) guards quiz state. Release it with defer (`s.withLock`) so a recovered panic cannot leave it locked. Never hold it during WebSocket sends, HTTP calls or slow file work. Lock order: `configMu`, `csvMu`, then `mu`. `SwitchQuiz` takes `csvMu` and `mu` itself. `s.text()`, `s.isTrustedIP()`, `s.validSession()` take `mu` themselves, so inside a locked section use `s.translations[...]`. Checker code runs without the lock: submit-answer checks, runs the checker, then repeats the checks under the lock before it stores. `go test -race` must pass. Details: `internal/server/doc.go`
- **Retries are safe**: a student's request can arrive twice at once. One user per `registration_token`, one stored answer per question, identical replies (tests: `TestConcurrentSubmitSameAnswer`, `TestConcurrentRegisterSameToken`)
- **Python-compatible values**: request JSON is decoded with `UseNumber` so `1` and `1.0` differ like in Python; YAML is read with `pyval.LoadYAML` (PyYAML / YAML 1.1 rules); quiz files are written like PyYAML's `yaml.dump`; a config value of the wrong type is read as the intended text or number
- **Checker code runs in Starlark** (`go.starlark.net`) after a rewrite step that turns Python-only syntax into Starlark (`assert` with message, f-strings with format specs, `**`, chained comparisons). Not supported: `try`, `raise`, `class`, `import`, `with`, `global`; `len()` and indexing of text count bytes (use `list(s)`). A checker stops after 2 seconds or when it uses too much memory, and the answer is then wrong. Loading a quiz logs a warning for each checker that does not compile. Helpers: `to_int`, `distance`, `direction_angle`, `math`. Rules: `internal/checker/doc.go`
- **Base folder**: `WEBQUIZ_BINARY_DIR` if set, else the folder of the executable. The binary sets `WEBQUIZ_BINARY_DIR`, changes into it, and sets `WEBQUIZ_IS_BINARY=1` when unset (then the browser opens `/admin/` after start). Config, quizzes, logs, CSV and `static/` live there
- **Middleware-based error handling**: handlers return an error; the error text becomes `{"error": text}` like 1.19
- **Configuration**: `internal/config`, all sections optional, validated before save. Quiz-wide on/off settings must be real booleans
- **6 digits user ID** stored by user_id as key
- **Server-side timing** for accuracy. A question's clock starts at `POST /api/question-start` (when the page shows it), also for the first question; registration and approval do not start it. A repeated question-start does not restart it. `current_question_ids[user_id]` names the question whose clock runs; `quiz_start_times[user_id]` is the whole-quiz clock. question-start returns 404 for an unknown question and starts clocks only for the student's next unanswered question. `question_ready_times[user_id]` is the fallback start for the time limit check when an answer arrives without question-start
- **Quiz time limits** - `internal/timelimits`: `per_question` or `whole_quiz` seconds (0 = none), questions may set their own in per-question mode. Submit order: stored timeout -> repeat/order/409 checks -> whole quiz -> empty answer -> late answer past `L + grace` -> timeout -> normal. Grace 5 s. A timeout is stored as `selected_answer` "" in CSV, `is_correct` false, 0 points; live stats state `"timeout"`. A watcher goroutine runs `check_time_limits` every second for pages that went quiet
- **Multi-file quiz system** in `quizzes/` dir, auto-created with a default quiz if missing; names with a folder part are refused
- **Smart CSV naming** with quiz prefix + shared `NNNN` suffix (never overwrites; numbering continues after files left by 1.19). Files: `{quiz}_{NNNN}.csv`, `.users.csv`, optional `.answers_with_users.csv` (`extra_answers_with_users_csv: true`)
- **Quiz state reset** on switch for complete isolation; a failed switch keeps the running quiz. Pending CSV rows are written on switch and on shutdown
- **In-memory -> CSV** with 5s periodic flush; every file write uses `flush()` + `fsync`
- **Randomization**: server-side order per student at registration (`question_order`), client reorders. `stick_to_the_previous: true` keeps questions adjacent (groups shuffled as units)
- **Test completion** is detected by answer count, not question ID
- **Points**: default 1, decimals with at most 2 places, > 0; sums are rounded to 2 decimals and shown as whole numbers when whole (`normalize_points`/`sum_points` on the server, `formatPoints` in `web/src/shared/points.ts`)
- **Start settings** - the admin start pop-up sends `settings` (show_right_answer, randomize_questions, show_answers_on_completion, show_final_list, show_explanation, time_limit) with `switch-quiz`; they are kept for that file's run and never written to the file. A restart of the same file keeps them; another file drops them
- **Question explanations** - Markdown with goldmark, raw HTML shown as text, harmful links as plain text; sent only in the answer reply, never in the boot data
- **Answer verdict** (new in 2.0) - while right answers are shown, `#answer-verdict` says "✓ Правильно" / "✗ Неправильно" (`answer_verdict_*` texts) left of "Continue", from the reply's `is_correct`. None for a timeout; gone on the next question and the explanation step
- **Plain text, never HTML** - quiz text and student input are shown as plain text on every page (React escapes text; no `dangerouslySetInnerHTML` for them); explanation HTML comes only from the server renderer
- **Dynamic answer visibility** - `show_answers_on_completion: true` reveals correct answers after all approved students complete; admin can force it (`force-show-answers`, resets on quiz switch)
- **Student request retry** - the quiz page retries on network errors, 15 s timeouts and 502/503/504 (1 s, 2 s, 4 s, then every 5 s) with a banner; the answer is locked while it is sent
- **Local network restriction** - admin API, pages and WebSockets only from private networks (RFC 1918 + loopback). `getClientIP` trusts `X-Forwarded-For` / `X-Real-IP` only when the TCP peer is loopback, and takes the **last** `X-Forwarded-For` entry
- **Config hot-reload** - `PUT /api/admin/config` applies registration settings, `admin.trusted_ips`, downloadable quizzes, tunnel config at once; server host/port, paths, master_key need a restart (message lists them). The quiz is always restarted. On failure the file is rolled back. Partial JSON updates keep comments and blank lines (`config.MergeYAML`, a line editor on a yaml.v3 node tree); the merged text is written only when it reads back as the old values with the update in them (`config.CheckMerge`), else the answer is a 500 and the file stays
- **Config form editor** - File manager Config tab has Form/YAML toggle; Form sends `{data: {...}}`. `registration.disabled_fields` keeps field names without showing them
- **SSH tunnel** - optional, admin clicks connect (no auto-connect); ED25519 keys auto-generated next to the program; auto-reconnect with backoff (5 s -> 300 s); status over `WS /ws/admin`; `tunnel.config` bypasses the server fetch; HTTP for IP addresses, HTTPS for names when fetching `tunnel_config.yaml` (IPv6 works)
- **File transfer between servers** - "ask first, then send" over the public tunnel URLs; offer token (30 min), receiver admin accepts per file (save/replace/rename/skip); only `.yaml/.yml`, `.log`, `.csv`, plus images and attachments of offered quizzes; SHA-256 per file; `_build_plan()` validates all choices before any download. State in memory
- **Private quiz archives** - a `quizzes` entry may carry `auth_header_token`; the config's `download_path` and `folder` are used, never the request's; the token goes only to https URLs (or http on localhost). GitHub: use `https://api.github.com/repos/OWNER/REPO/zipball/REF`. If `folder` is missing at the archive top level and exactly one directory was extracted, it is retried inside it
- **Quiz download folder validation** - path traversal protection (no `..`, no absolute paths, stays inside `quizzes/`)
- **Version check** - the admin panel compares the running version with `--version` of the file on disk ("restart required"), and asks GitHub releases of `oduvan/webquiz-go` for a newer one
- **Quiz interface language** - `language` (`uk` or `en`, default `uk`) from `internal/i18n`; the pages get the texts in the boot data; hot-reloadable
- **Dark/Light theme** - toggle on every page, `theme` key in localStorage, CSS variables. The button (and the student's name badge on the quiz page) sit at the top of the page with `position: absolute` and scroll away with it, as in 1.19; never `position: fixed`, which covered the text on a phone
- **Quiz file attachments** - `file` field, files in `quizzes/attach/`, served at `/attach/` with forced download
- **Gzip copies of the page files** - `vite build` writes `name.gz` (best compression, only when smaller) next to every .js, .css, .svg and .json in `web/dist`. `serveAssets` sends the copy when Accept-Encoding allows gzip, with `Content-Encoding: gzip`, the type of the file itself and `Vary: Accept-Encoding`. Pictures (`/imgs/`) and the HTML pages (made per request) are sent as they are
- **Question text copy prevention** - questions have `user-select: none` and a `copy` handler; text inputs stay selectable
- **File manager** shows the whole `webquiz.yaml` (also `master_key`) to every computer on the local network, as in 1.19

## Key Flows

**Quiz**: Load YAML -> boot data in HTML -> register user -> cookie session -> question-start -> submit answers -> validate -> in-memory -> CSV flush
**Approval**: Register -> `approved: false` -> admin WebSocket notified -> approve -> student checks -> quiz begins -> timing starts at question-start
**Admin**: Switch quiz -> reset all state -> new CSV files
**Config Hot-Reload**: validate YAML -> back up -> write -> reload -> detect restart-required changes -> apply safe ones -> reload templates -> disconnect tunnel -> restart quiz. On failure: roll back the file
**Startup**: base folder -> load config -> create server -> log file -> log environment info -> tunnel init -> load questions -> start CSV flush and time limit watcher -> routes

## Important Notes
- Config (`webquiz.yaml`): all sections optional, editable in `/files/`, hot-reloaded on save. Options are documented in `docs/en/040_config.md`
- Questions use **0-indexed** `correct_answer` and optional `points`; text questions are detected by `checker` (no `type`, `options` or `correct_answer`)
- Usernames are unique per quiz session
- Release notes: if `.github/release-notes/vX.Y.md` exists at the release tag, the workflow puts it at the top of the GitHub release notes

## Release
**Trigger**: GitHub Actions -> "Release" -> enter version (X.Y.Z); tick "pre-release" for a test build that is not shown as the latest release

**Process**: write `VERSION` and tag -> build the web pages once -> `make dist-binaries` (Linux amd64/arm64/arm, macOS Intel/Apple Silicon, Windows) -> Windows installer (Inno Setup) -> PDFs of the docs (pandoc + xelatex, uk/en) -> GitHub release with the zips, the installer and the PDFs -> deploy GitHub Pages

**GitHub Pages**: https://oduvan.github.io/webquiz-go/ - download page and Docsify docs (no build step). `deploy-pages.yml` is manual or called by the release workflow
