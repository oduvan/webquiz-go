# WebQuiz Go Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild WebQuiz 1.19 as a Go server with a React + Tailwind frontend in `oduvan/webquiz-go`, porting all 911 Python tests.

**Architecture:** Go packages under `internal/` (checker, config, quiz, timelimits, explanations, i18n, tunnel, transfer, server, version) and a CLI in `cmd/webquiz`. A Vite multi-page React app in `web/` is embedded into the binary. The Python tests are the acceptance tests while building (run against the Go binary from `/home/user/pytests-go`) and are ported to Go tests (`go test`) and Playwright tests (`e2e/`), each tagged with its Python id.

**Tech Stack:** Go 1.26, net/http, gopkg.in/yaml.v3, github.com/coder/websocket, github.com/yuin/goldmark, go.starlark.net, golang.org/x/crypto/ssh; React 19, TypeScript, Vite, Tailwind CSS v3, Vitest, @playwright/test 1.63.0.

**Spec:** `docs/superpowers/specs/2026-11-09-go-migration-design.md` (read its "Calls made after the design review" section too).

## Global Constraints

- Go module `github.com/oduvan/webquiz-go`, `go 1.26` in `go.mod`, binary name `webquiz`, entry `cmd/webquiz`, builds with `CGO_ENABLED=0`.
- Runtime Go dependencies only: `gopkg.in/yaml.v3`, `github.com/coder/websocket`, `github.com/yuin/goldmark`, `go.starlark.net`, `golang.org/x/crypto`. Anything else needs a reason in the commit message.
- Frontend: React 19 + TypeScript + Vite + **Tailwind CSS v3** (not v4); Vite `build.target: ['es2019', 'safari13']`; `@playwright/test` pinned to `1.63.0`; Vitest for unit tests.
- Source of truth for behavior: the Python project at `/home/user/webquiz` (v1.19, commit `367a99d`): `webquiz/*.py`, `webquiz/templates/*.html`, `CLAUDE.md`, `README.md`, `docs/`. Same HTTP paths, methods, JSON fields, status codes, error texts (uk and en), YAML keys, CSV file names and columns, log location.
- Test tags: every ported test carries `py: <id>` with the id copied exactly from `migration/python-tests.txt` (Go: a `// py: ...` comment right above the test or a `py:` field in a table case; TS: a `// py: ...` comment above the test). A port checks what the Python test checked. Where the new UI or Go internals make a check impossible, the port checks the same behavior through the new surface and a comment says why.
- Python-like semantics that tests rely on: decode request JSON with `UseNumber` so `1` and `1.0` differ like in Python; points are rounded to 2 decimals and shown as whole numbers when whole.
- Translate, do not invent: when unsure how something should work, read the Python code and do what it does. No new CLI options. Base folder as in `binary_entry.py`: `WEBQUIZ_BINARY_DIR` if already set, else the executable's folder; the binary sets `WEBQUIZ_BINARY_DIR` and changes into it at start. The browser opens `/admin/` after start when `WEBQUIZ_IS_BINARY=1`; the binary sets it to `1` only when unset (tests set `0`).
- Environment variables, all from Python: `WEBQUIZ_MASTER_KEY`, `WEBQUIZ_BINARY_DIR`, `WEBQUIZ_IS_BINARY`, `WEBQUIZ_TIME_LIMIT_GRACE_SECONDS`, `WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL`.
- Repository address in code only in `internal/version` (`RepoURL = "https://github.com/oduvan/webquiz-go"`, `PagesURL = "https://oduvan.github.io/webquiz-go/"`).
- Boot data contract: every HTML entry in `web/` has `<title>__WEBQUIZ_TITLE__</title>` and `<script id="webquiz-boot" type="application/json">__WEBQUIZ_BOOT__</script>`. The Go server replaces the title with HTML-escaped text and the boot marker with JSON where `<`, `>` and `&` are written as `<`, `>`, `&`.
- Asset URLs: Vite `base: './'`; the quiz page keeps relative URLs (`./assets/`, `api/...`, `imgs/...`) because it is also served under `{base_url}/{socket_name}/`; admin pages may use absolute `/api/...` and the server rewrites their `./assets/` to `/assets/`.
- Concurrency: one `sync.Mutex` guards quiz state; no slow I/O (WebSocket sends, HTTP calls) while holding it. `go test -race` must pass.
- Differential runs (acceptance while building): from `/home/user/webquiz` with `source venv/bin/activate`, run `WEBQUIZ_GO_BINARY=<abs path to built binary> WEBQUIZ_TEST_PORT_BASE=<task's base> python -m pytest /home/user/pytests-go/<file> -q -o addopts="" -p no:cacheprovider --rootdir=/home/user/pytests-go`. Never use another task's port base.
- Writing style for comments, commit messages and docs: plain, simple English; short sentences are fine, dense ones are not.
- Commits end with the two attribution lines given to you in the dispatch. No model names in commits, code or docs.
- Work only in your own worktree and branch (given in the dispatch). Never push. Never dispatch subagents.

## Review Focus

1. **A student's request arrives twice at the same time** (retry after a timeout, double click): Go runs handlers in parallel, Python never did. Expect exactly one stored user per `registration_token`, one stored answer per question, and identical replies. Test owner: Task 7 (`TestConcurrentSubmitSameAnswer`, `TestConcurrentRegisterSameToken`: 20 goroutines each).
2. **Quiz page under a path prefix** (tunnel serves it at `{base_url}/{socket_name}/`): assets, API calls and images must load. Test owner: Task 14 (e2e through a small proxy that strips `/abc123/`).
3. **A folder left by 1.19** (commented `webquiz.yaml`, Ukrainian quiz file names, existing CSV and log files): read as before, config form saves keep comments, new CSV/log numbers continue after the old ones. Test owners: Task 3 (`TestPartialUpdateKeepsComments` with a 1.19-style config), Task 7 (`TestCSVNumberingContinuesAfterExistingFiles`).
4. **A broken or hostile checker** (`while True: pass`, huge strings, deep recursion, syntax Starlark cannot parse): the answer is marked wrong within about 2 seconds and other requests keep working; loading such a quiz writes a warning to the log. Test owners: Task 2 (`TestInfiniteLoopTimesOut`), Task 7 (`TestSlowCheckerDoesNotBlockOtherStudents`, `TestQuizLoadWarnsAboutBadChecker`).
5. **A Go 2.0 server and a Python 1.19 server on the same tunnel server send files to each other**: the offer, download and result messages must stay byte-compatible in JSON shape. Test owner: Task 10 (`TestTransferJSONMatchesPython` with fixtures captured from the Python `TransferManager`).

## Phases

| Phase | Tasks (run in parallel inside a phase, each in its own worktree) |
|---|---|
| 0 | 1 |
| 1 | 2, 3, 4, 5 |
| 2 | 6 |
| 3 | 7 |
| 4 | 8, 9, 10, 11, 12, 13, 14 |
| 5 | 15, 16, 17, 18 |
| 6 | 19 |
| 7 | 20 |

Merges happen only on `claude/gracious-edison-r3buih`, done by the controller, in task order inside a phase.

---

### Task 1: Repository bootstrap

**Files:**
- Create: `go.mod`, `go.sum`, `VERSION` (`2.0`), `Makefile`, `.gitignore`, `README.md` (short stub), `migration/python-tests.txt` (911 ids), `migration/ported_test.go`, `migration/README.md`, empty package dirs with `doc.go` files.
- Copy from Python repo unchanged for later tasks: `docs/en/`, `docs/uk/`, `docs/imgs/`, `gh-pages/`, `installer/webquiz.iss`, `.github/ISSUE_TEMPLATE/`, `.github/release-notes/`, `tunnel.md`, `webquiz/server_config.yaml.example` → `internal/config/server_config.yaml.example`.

**Interfaces:**
- Produces: `make web` (npm ci + build in `web/`), `make build` (web + `go build -o bin/webquiz ./cmd/webquiz`), `make test` (`go test -race ./...`), `make e2e`.
- Produces: `migration/ported_test.go` — `TestEveryPythonTestIsPorted` reads `python-tests.txt`, scans `**/*_test.go`, `e2e/**/*.ts`, `harness/**/*.test.ts` (skipping `node_modules`), and fails listing every id with no `py: <id>` tag. While the migration runs, it is skipped unless `WEBQUIZ_REQUIRE_ALL_PORTED=1`; Task 20 removes the skip.

- [ ] Step 1: write `migration/ported_test.go` and a unit test of its scanner (`TestScannerFindsTags` with a temp tree).
- [ ] Step 2: `go test ./migration/` passes (skip active), `WEBQUIZ_REQUIRE_ALL_PORTED=1 go test ./migration/` fails listing 911 ids.
- [ ] Step 3: commit and push.

### Task 2: Checker (Starlark)

**Files:**
- Create: `internal/checker/checker.go` (run), `rewrite.go` (Python → Starlark), `tokenize.go`, `format.go` (`_fmt_` format spec), `builtins.go` (math, helpers, missing builtins), `helpers.go` (`ToInt`, `Distance`, `DirectionAngle`), tests next to them, `testdata/python_results.json`, `testdata/gen_python_results.py`.
- Python sources: `webquiz/checker.py`, `TestingServer._execute_checker` (server.py:1731-1802), checker syntax check in `_validate_quiz_data` (server.py:3540-3560).

**Interfaces:**
- Produces:
  - `func Check(code, answer string) Result` with `type Result struct { Correct bool; Error string }`. The caller handles an empty checker with `CheckExact`; `Check("", x)` returns `Correct: true`.
  - `func CheckExact(answer, correctValue string) bool` (trimmed equality, the empty-checker rule).
  - `func Validate(code string) *SyntaxError` with `type SyntaxError struct { Msg string; Line int }` and `Error() string`; nil when the code compiles.
  - `func ToInt(s string) (int, error)`, `func Distance(s string) (int, error)`, `func DirectionAngle(s string) (int, error)` with the exact Python error texts.
  - `var Timeout = 2 * time.Second` and a step limit constant.

- [ ] Step 1: port `tests/test_checker.py` (31 ids) to `helpers_test.go`.
- [ ] Step 2: write rewrite tests: assert (with/without message, one-line `if x: assert y`, multi-line, `assert(x)`), f-strings (all prefixes, `{{ }}`, `!r`, `:.2f`, `:>8`, `:,`, nested quotes, nested f-string, triple quotes), `**` (`2 ** 3 ** 2`, `-2 ** 2`, `a.b(c)[d] ** 2`), chained comparisons (`0 < x < 10`, `a in b not in c`, inside comprehensions and lambdas), `user_answer = user_answer.strip()`, lines inside multi-line strings not indented.
- [ ] Step 3: write `TestInfiniteLoopTimesOut` (`while True: pass` → `Correct: false` within 3 s), `TestDeepRecursionFails`, `TestHugeStringFails`.
- [ ] Step 4: write `gen_python_results.py` (runs each case through the Python 1.19 `_execute_checker` sandbox; ~60 checker × answer cases in the style teachers and AI write) and commit its output `python_results.json`; `TestMatchesPythonResults` checks Go gives the same `Correct` and, for assert failures, the same message.
- [ ] Step 5: implement until all pass; `go test -race ./internal/checker/` passes.
- [ ] Step 6: commit.

### Task 3: Core libraries

**Files:**
- Create: `internal/config/` (structs, defaults, `Load`, overrides, `Validate` from `_validate_config_data`, comment-keeping `MergeYAML` from the partial update path of `admin_update_config`, binary-relative paths), `internal/i18n/` (texts from `translations.py`), `internal/timelimits/` (from `time_limits.py`), `internal/explanations/` (goldmark; from `explanations.py`), `internal/quiz/` (quiz YAML loading, `_validate_quiz_data` incl. points/time limits/explanations/checker syntax via `checker.Validate`, `normalize_points`/`sum_points`, `generate_random_question_order` with sticky groups, `_validate_answer`, `_format_answer_text`, question ids), `internal/version/` (version from embedded `VERSION`, `RepoURL`, `PagesURL`, `FileVersion(path)` that finds the version marker in a binary file).
- Python sources: `webquiz/config.py`, `translations.py`, `time_limits.py`, `explanations.py`, server.py functions named above, `get_package_version`/`get_file_version`.

**Interfaces:**
- Consumes: `checker.Validate` (Task 2) — until Task 2 merges, import it through a small interface var `quiz.ValidateChecker func(code string) (msg string, line int, ok bool)` set by the server at start. Tests in this task set it to a fake.
- Produces (exported, documented in each package's `doc.go`): `config.Config` with fields matching `WebQuizConfig` (yaml tags = the YAML keys), `config.Load(path string) (*Config, error)`, `config.Default() *Config`, `config.ApplyOverrides`, `config.Validate(data map[string]any) []string`, `config.MergeYAML(original []byte, partial map[string]any) ([]byte, error)`; `i18n.Get(lang string) map[string]string`, `i18n.Languages`; `timelimits.Settings`, `Parse`, `Validate`, `QuestionLimits`, `IsEmptyAnswer`, `GraceSeconds()`, `CheckInterval()`; `explanations.Render(md, imagesURL string) string`, `Has`, `ImageNames`, `RewriteImages`, `Validate`, `StudentImagesURL = "imgs/"`, `AdminImagesURL = "../imgs/"`; `quiz.Validate(data map[string]any) []string`, `quiz.Load(path string) (*Quiz, error)`, `quiz.NormalizePoints`, `quiz.SumPoints`, `quiz.RandomOrder(questions, rnd) []int`, `quiz.CheckAnswer(question, answer) (correct bool, checkerError string)`, `quiz.FormatAnswer`.

- [ ] Step 1: port tests: `test_config_loading.py` (20), `test_time_limits_unit.py` (9), `test_explanations_unit.py` (15), from `test_language.py` the 9 ids `test_uk_and_en_have_same_keys`, `test_get_translations_uk`, `test_get_translations_en`, `test_get_translations_fallback`, `test_no_empty_translations`, `test_default_language_is_uk`, `test_load_language_from_yaml`, `test_load_language_uk_from_yaml`, `test_missing_language_defaults_to_uk`; from `test_tunnel.py` the 10 config ids (`test_tunnel_config_*`, `test_load_config_with_tunnel*`, `test_load_config_without_tunnel`, `test_load_config_with_nested_tunnel_config`); from `test_multiple_answers.py` the classes `TestMultipleAnswersValidation` (5) and `TestQuizValidation` (4).
- [ ] Step 2: add `TestPartialUpdateKeepsComments` (1.19-style commented config, update `registration` and `language`, comments and blank lines stay).
- [ ] Step 3: implement until all pass; `go test -race ./internal/...` passes.
- [ ] Step 4: commit.

### Task 4: SSH tunnel

**Files:**
- Create: `internal/tunnel/` (manager, key generation ED25519 in OpenSSH format, config fetch over HTTP/HTTPS with the IP rule, connect with `x/crypto/ssh`, remote Unix socket forward to the local port via `streamlocal-forward@openssh.com`, auto-reconnect 5 s → 300 s, status callback, `PublicEndpoint() (baseURL, socketID string, ok bool)`), tests.
- Python source: `webquiz/tunnel.py`, `tunnel.md`.

**Interfaces:**
- Consumes: `config.TunnelConfig` shape — define the struct here as `tunnel.Config` with the same fields; Task 6 converts.
- Produces: `tunnel.New(cfg Config, localPort int, onStatus func(Status)) *Manager`, `(*Manager).EnsureKeys() (publicKey string, err error)`, `Connect(ctx) error`, `Disconnect()`, `Status() Status`, `PublicEndpoint()`, `Close()`.

- [ ] Step 1: port the 21 `TunnelManager` ids of `tests/test_tunnel.py` (all ids except the 10 config ids owned by Task 3). Use an in-process SSH server (`x/crypto/ssh`) that accepts `streamlocal-forward@openssh.com` for connect tests, and `httptest` for the config fetch.
- [ ] Step 2: implement until all pass with `-race`.
- [ ] Step 3: commit.

### Task 5: Web scaffold and e2e scaffold

**Files:**
- Create: `web/package.json`, `web/vite.config.ts` (5 entries: `index.html`, `admin.html`, `files.html`, `live-stats.html`, `csv-table.html`; `base: './'`; target es2019/safari13), `web/tailwind.config.js`, `web/postcss.config.js`, `web/tsconfig.json`, `web/src/shared/` (`boot.ts` `readBoot<T>()`, `api.ts` with `fetchWithRetry` (1 s, 2 s, 4 s, then every 5 s; 15 s timeout; retry on network error and 502/503/504), `theme.ts` + `ThemeToggle` (localStorage `theme`, `storage` event), `Modal`, `Button`, `Banner`, `escape`-free text rendering helpers, `formatPoints`/`roundPoints`), one placeholder page per entry, `web/embed.go` (`package web`, `//go:embed all:dist`, `var Dist embed.FS`), `web/src/**/*.test.ts` (Vitest).
- Create: `e2e/package.json`, `e2e/playwright.config.ts` (workers from CPU count, `trace: 'off'`), `e2e/global-setup.ts` (builds web and `go build -o e2e/.bin/webquiz ./cmd/webquiz`), `e2e/fixtures/server.ts` (`startServer({config, quizzes, env})` → `{url, port, dir, stop()}`: temp dir, YAML files, free port, spawns `.bin/webquiz --config <tmp>/webquiz.yaml --port <port>` with env `WEBQUIZ_BINARY_DIR=<tmp>` and `WEBQUIZ_IS_BINARY=0`, waits for the port), `e2e/fixtures/helpers.ts` (admin login, register student, waits).

**Interfaces:**
- Produces the boot data contract (Global Constraints) on the frontend side and the `startServer` fixture used by Tasks 14-17.

- [ ] Step 1: Vitest tests for `fetchWithRetry` (retry schedule, timeout, no retry on 400), `readBoot`, `formatPoints`.
- [ ] Step 2: `npm --prefix web run build` produces `web/dist/{index,admin,files,live-stats,csv-table}.html` with the markers intact and `assets/`.
- [ ] Step 3: commit.

### Task 6: Server skeleton

**Files:**
- Create: `internal/server/server.go` (`Server` struct with every state field of `TestingServer.__init__` and `reset_server_state`), `routes.go` (every route of `create_app`, server.py:4843-4949, plus `/assets/`), `middleware.go` (JSON error middleware, `adminAuthRequired`, `localNetworkOnly`, `getClientIP` with the loopback-only header rule, `trusted_ips`), `pages.go` (boot data structs per page, HTML rendering of each `web/dist` entry, `static_dir/index.html` generation, `quiz_selection_required` and `template_error` states), `json.go` (UseNumber decoding, writing helpers), one file per area with stub handlers returning 501: `student.go`, `admin_quiz.go`, `admin_config.go`, `files.go`, `transfer_handlers.go`, `websocket.go`, `csvlog.go`; `internal/testutil/` (`StartServer(t, Options{Config map[string]any; Quizzes map[string]any; Env map[string]string}) *TestServer` mirroring `conftest.custom_webquiz_server` with temp dirs and the same default config, `(*TestServer).AdminCookies(t, key)`, `RegisterStudent(t, name) string`, `PostJSON/GetJSON/Put/Delete` helpers, `BuildBinary(t) string`); `cmd/webquiz/main.go` (minimal: flags `--config` and `--port`, the `binary_entry.py` start steps; starts the server) so the binary runs.
- Python sources: server.py lines 1-720, 1048-1367, 4296-4842.

**Interfaces:**
- Consumes: packages of Tasks 2-5.
- Produces: `server.New(cfg *config.Config, opts Options) (*Server, error)`, `(*Server).Handler() http.Handler`, `(*Server).Start(ctx)` (flush loop, time limit watcher), `(*Server).Close()`; `Options{Grace, CheckInterval time.Duration; Now func() time.Time; Logger *slog.Logger}`; `testutil` as above; boot structs documented in `internal/server/pages.go` and mirrored in `web/src/shared/boot.ts`.

- [ ] Step 1: tests: every route exists (404 vs 501), admin/local-network guards on the stubs, boot JSON escaping (`</script>` in a title), `static_dir/index.html` written on start.
- [ ] Step 2: implement; `go test -race ./internal/server/...` and `make build` pass; `WEBQUIZ_BINARY_DIR=<tmp> WEBQUIZ_IS_BINARY=0 bin/webquiz` serves `/`.
- [ ] Step 3: commit.

### Task 7: Student flow

**Files:**
- Modify/Create in `internal/server/`: `student.go` (register with `registration_token`, update-registration, verify-user, question-start, submit-answer with the full order from `CLAUDE.md` "Quiz time limits", `_record_answer`, `_end_whole_quiz`), `timing.go`, `stats.go` (`calculate_and_store_user_stats`, `all_students_completed`, `get_user_final_results`, force flag), `csvlog.go` (CSV paths with shared `NNNN`, 5 s flush, users CSV, answers_with_users CSV, fsync), `livestats.go` (live stats WebSocket, two groups, broadcast), `quizload.go` (load questions, ids, randomization per user, explanations HTML per question, checker load warnings), `watcher.go`.
- Python sources: server.py 721-1047, 1148-1244, 1368-1910, 1911-2840.

**Interfaces:**
- Consumes: Task 6 skeleton and helpers.
- Produces: working student API and live stats; `(*Server).SwitchQuiz(filename string, settings map[string]any) error` (used by admin tasks); a warning in the log for each checker that does not compile when a quiz loads.

- [ ] Step 0: port the 2 ids Task 3 could not port without the server: `tests/test_time_limits_unit.py::test_whole_quiz_end_twice_at_once_stores_each_question_once` and `tests/test_time_limits_unit.py::test_whole_quiz_end_returns_response_for_the_submitted_question`.
- [ ] Step 1: Review Focus tests: `TestConcurrentSubmitSameAnswer`, `TestConcurrentRegisterSameToken`, `TestSlowCheckerDoesNotBlockOtherStudents`, `TestQuizLoadWarnsAboutBadChecker`, `TestCSVNumberingContinuesAfterExistingFiles`.
- [ ] Step 2: implement; differential run with port base `18100` of: `test_registration_approval.py test_registration_fields.py test_registration_disabled_fields.py test_username_label.py test_randomize_questions.py test_stick_to_previous.py test_connection_retry.py test_csv_collision.py test_answers_with_users_csv.py test_text_input_questions.py test_multiple_answers.py test_integration_multiple_choice.py test_question_points.py test_decimal_points.py test_show_right_answer.py test_show_final_list.py test_show_answers_on_completion.py test_time_limits_submit.py test_time_limits_timing.py test_time_limits_watcher.py test_time_limits_validation.py test_live_stats_two_groups.py test_live_stats_websocket.py` — all pass except tests that read Python internals, the old HTML, or admin endpoints not built yet; list each exception with its reason in the report.
- [ ] Step 3: `go test -race ./...` passes; commit.

### Task 8: Admin API — quizzes

**Files:**
- Modify/Create in `internal/server/`: `admin_quiz.go` (auth + session cookies, check-session, approve-user + admin WebSocket notifications, force-show-answers, list-quizzes with titles, switch-quiz with start settings `_validate_start_settings`/`_apply_start_settings`, get/create/update/delete quiz with rename and backups, unite, validate-quiz, preview-explanation, list-images, list-files, list-checker-templates), `websocket.go` admin socket.
- Python sources: server.py 939-1047, 2840-3500, 3780-3847, 4802-4842.

**Interfaces:**
- Consumes: `SwitchQuiz` and state from Task 7.

- [ ] Step 1: port tests (Go, in-process): `test_admin_api.py` (15), `test_admin_session_auth.py` (8), `test_admin_quiz_management.py` (20), `test_admin_quiz_editor.py` (7), `test_admin_reveal_answers.py` (9), `test_admin_unite_quizzes.py` except the 4 browser ids (16), `test_quiz_rename.py` (11), `test_quiz_start_settings.py` (7), `test_quiz_validation_edge_cases.py` (26), `test_explanations.py` (17).
- [ ] Step 2: implement until they pass; differential run of the same files with port base `18200` passes (browser tests skipped).
- [ ] Step 3: commit.

### Task 9: Admin API — config, downloads, version, tunnel

**Files:**
- Modify/Create in `internal/server/`: `admin_config.go` (PUT config: full YAML or JSON partial with `config.MergeYAML`; validation; backup; hot reload; restart-required list; rollback; response with `config_content` and `config_data`), `download.go` (download-quiz with folder checks, archive root folder rule, `auth_header_token` rules, `can_send_token_to`), `/attach/{filename}`, version-check, tunnel connect/disconnect + tunnel status over the admin WebSocket.
- Python sources: server.py 804-938, 3848-4243.

- [ ] Step 1: port tests: `test_config_management.py` (26), `test_config_hot_reload.py` (13), `test_admin_download_quiz.py` (5), `test_download_quiz_token.py` (6), `test_quiz_file_download.py` (8), `test_version_check.py` (6), and from `test_language.py` the 12 ids from `test_default_language_shows_ukrainian` to `test_russian_language_in_yaml_file_falls_back_to_uk`.
- [ ] Step 2: implement; differential run with port base `18300` passes.
- [ ] Step 3: commit.

### Task 10: Files API and file transfer

**Files:**
- Create: `internal/transfer/` (from `transfer.py`: offers with 30-minute tokens, name and type rules, SHA-256, media files referenced by quizzes incl. explanations, plan building with cases and actions, downloads to temp files, reference rewriting keeping YAML style via `yaml.Node`, results); `internal/server/files.go` (list/view/download/save quiz with validation, backup and active quiz reload; `_is_safe_filename`), `transfer_handlers.go`.
- Python sources: `webquiz/transfer.py`, server.py 4243-4600.

**Interfaces:**
- Produces: `transfer.New(Deps{Directories func() map[string]string; Endpoint func() (baseURL, name string, ok bool); Notify func(msg map[string]any); FilesInUse func() []string}) *Manager`, handlers `HandleOffer`, `HandleDownload`, `HandleResult`, admin methods `State`, `Send`, `Accept`, `Reject`.

- [ ] Step 1: port tests: `test_transfer.py` (53, two managers behind an `httptest` fake tunnel server like the Python `FakeTunnelServer`), `test_transfer_api.py` (8), `test_files_management.py` (38), `test_csv_table_viewer.py` (5). Add `TestTransferJSONMatchesPython` (fixtures captured from the Python manager: offer body, result body, state JSON).
- [ ] Step 2: implement; differential run with port base `18400` passes.
- [ ] Step 3: commit.

### Task 11: CLI, daemon, startup log, network

**Files:**
- Create: `cmd/webquiz/` (exactly the flags of `cli.py`; the start steps of `binary_entry.py`; `-d` starts a detached copy with `setsid` and writes `webquiz.pid` (Go has no `fork`); `--stop`, `--status`; Windows: `-d` prints "not supported"), `internal/server/startup_log.go` (`log_startup_environment` with Go facts instead of Python ones), `internal/netinfo/` (`get_network_interfaces` with `include_ipv6`, `is_loopback_address`, `normalize_url`, URL format).
- Python sources: `webquiz/cli.py`, `binary_entry.py`, server.py 273-503.

- [ ] Step 1: port tests: `test_cli_directory_creation.py` (8, real binary via `testutil.BuildBinary`), `test_startup_logging.py` (13), `test_network_interfaces.py` (21), `test_network_access.py` (22).
- [ ] Step 2: implement; differential run with port base `18500` passes.
- [ ] Step 3: commit.

### Task 12: Port student-flow tests, part A

**Files:**
- Create: `internal/server/*_test.go` files named after the Python files.

- [ ] Step 1: port `test_registration_approval.py` (20), `test_registration_fields.py` (13), `test_registration_disabled_fields.py` (7), `test_username_label.py` (5), `test_randomize_questions.py` (19), `test_stick_to_previous.py` (14), `test_index_generation.py` (18; checks read the boot JSON in `static_dir/index.html`), `test_connection_retry.py` (12), `test_csv_collision.py` (1), `test_answers_with_users_csv.py` (5), and from `test_multiple_answers.py` the class `TestMultipleAnswersAPI` (2).
- [ ] Step 2: fix server bugs the ports find (each fix with its own test, listed in the report); `go test -race ./...` passes.
- [ ] Step 3: commit.

### Task 13: Port student-flow tests, part B

- [ ] Step 1: port `test_text_input_questions.py` (19), `test_integration_multiple_choice.py` (6), `test_question_points.py` (7), `test_decimal_points.py` (22), `test_show_right_answer.py` (5), `test_show_final_list.py` (6), `test_show_answers_on_completion.py` (10), `test_time_limits_submit.py` (19), `test_time_limits_timing.py` (8), `test_time_limits_watcher.py` (5), `test_time_limits_validation.py` (4), `test_live_stats_two_groups.py` (9), `test_live_stats_websocket.py` (9).
- [ ] Step 2: fix server bugs the ports find (each with its own test, listed in the report); `go test -race ./...` passes.
- [ ] Step 3: commit.

### Task 14: Quiz page and live stats page

**Files:**
- Create: `web/src/quiz/**` (registration with extra fields and `registration_token`, approval waiting with "Check", questions: single, multiple with hint, text with default value, image questions and image options, file attachment links, points badge, progress, timers for per-question and whole-quiz limits, explanations step, results with or without the final list, waiting for answers on completion, connection banner, double-click guards, copy blocking, image preloading, uk/en texts from boot data), `web/src/livestats/**` (WebSocket, in-progress and completed groups, per-question states incl. timeout, points), `e2e/tests/quiz/*.spec.ts`, `e2e/tests/livestats/*.spec.ts`.
- Python sources: `templates/index.html`, `templates/live_stats.html`.
- DOM contract (keep): `#registration`, `#username`, `.register-btn`, `.registration-field[data-field-name]`, `#registration-error`, `#waiting-username`, `#user-info`, `#username-display`, `#quiz-title`, `#test-section`, `#current-question-container`, `.question-text`, `.question-image`, `.quiz-option[data-option-index]`, `.option-text`, `.multiple-choice-hint`, `#text-answer-input`, `#submit-answer-btn`, `#continue-btn`, `#explanation-step`, `.explanation-content`, `#explanation-next-btn`, `#progress-text`, `#progress-fill`, `.points-indicator`, `#question-timer`, `#quiz-timer`, `#quiz-timer-box`, `.time-up-message`, `#checker-feedback .correct-value-display`, `#results`, `#results-content`, `.results-table`, `#connection-banner`, `.theme-toggle`, `#loading`.

- [ ] Step 1: port to Playwright: `test_selenium_multiple_choice.py` (35), `test_user_journey_selenium.py` (15), `test_selenium_registration_fields.py` (6), `test_selenium_time_limits.py` (9), `test_selenium_explanations.py` (11), `test_selenium_connection_retry.py` (4), `test_selenium_decimal_points.py` (3), `test_selenium_hidden_correctness.py` (2), `test_selenium_html_escaping.py` (6), `test_selenium_live_stats_decimal_points.py` (2), `test_auto_advance.py` (6). Add the Review Focus prefix test.
- [ ] Step 2: build the pages until all pass; mobile layout checked at 375 px in one test per page.
- [ ] Step 3: commit.

### Task 15: Admin page

**Files:**
- Create: `web/src/admin/**` (login, quiz list `<select multiple>` with titles and current marker, single and multi actions, start settings modal, quiz editor wizard: questions, options, correct answers, min_correct, points with `validateWizardPoints`, images and file pickers, checker and templates, explanations with fold, image insert and preview, time limits, sticky flag, drag reorder; unite, duplicate, delete, rename; pending users and approval; force show answers; network URLs incl. tunnel URL; tunnel panel with public key copy; transfer requests with per-file actions; version and "restart required" banners — the "new version" check asks `https://api.github.com/repos/oduvan/webquiz-go/releases/latest` instead of PyPI), `e2e/tests/admin/*.spec.ts`.
- Python source: `templates/admin.html`.
- DOM contract (keep): `#master-key`, `#admin-panel`, `#quiz-select`, `#single-quiz-actions`, `#multi-quiz-actions`, `#switch-btn`, `#edit-btn`, `#delete-btn`, `#unite-btn`, `#start-quiz-modal`, `#start-quiz-confirm`, `#start-quiz-cancel`, `#start-show-right-answer`, `#start-show-final-list`, `#start-show-answers-on-completion`, `#start-show-explanation`, `#start-show-explanation-group`, `#start-show-explanation-note`, `#start-time-limit-mode`, `#start-time-limit-seconds`, `#quiz-editor-modal`, `#modal-title`, `#quiz-filename`, `#save-continue-btn`, `.question-item`, `.question-header`, `.question-number`, `.question-preview`, `.question-body`, `.question-points-badge`, `.points-input`, `.drag-handle`, `.question-time-limit-group`, `.question-time-limit-input`, `#time-limit-mode`, `#time-limit-seconds`, `#show-explanation-group`, `#show-explanation`, `textarea.question-explanation`, `.explanation-toggle-btn`, `.explanation-image-btn`, `.explanation-preview-btn`, `.explanation-preview`, `#image-grid .image-item`, `#message-area .message.error`, `.pending-user-name`, `option_0`/`option_N` ids for editor options.

- [ ] Step 1: port to Playwright: `test_selenium_quiz_editor.py` (9), `test_selenium_admin_explanations.py` (10), `test_selenium_admin_start_settings.py` (4), `test_selenium_admin_time_limits.py` (3), `test_selenium_admin_decimal_points.py` (7), and the 4 browser ids of `test_admin_unite_quizzes.py`.
- [ ] Step 2: build until all pass; commit.

### Task 16: Files page and CSV table page

**Files:**
- Create: `web/src/files/**` (tabs: quizzes, logs, CSV, config; view, download, YAML edit with validate and save; config Form/YAML toggle with language and registration fields incl. disabled fields; CSV pop-up with the table page in an iframe and "↗ New window" link; transfer send panel shown only when available), `web/src/csvtable/**` (`?question=`, `?user=`, `?embedded=1`, quoted-field parser, wrapped cells, theme sync), `e2e/tests/files/*.spec.ts`.
- Python sources: `templates/files.html`, `templates/csv_table.html`.
- DOM contract (keep): `#csv-files .file-item[data-name]`, `#csv-table-frame`, `a.btn-new-window`, `#open-new-window`, `#question-filter`, `#question-filter-group`, `#user-filter`, `#user-filter-group`, `#row-count`, `#csv-message`, `#table-container tbody tr`, `th .question-text`.

- [ ] Step 1: port to Playwright: `test_selenium_csv_table_viewer.py` (12); add one Playwright test each for config form save and YAML validate.
- [ ] Step 2: build until all pass; commit.

### Task 17: Classroom harness in TypeScript

**Files:**
- Create: `harness/` in TypeScript (`run.ts`, `student.ts`, `classroom.ts`, `scripted.ts`, `scenarios.ts`, `checks.ts`, `steps.ts`, `checks.test.ts`, `README.md`, `AGENT_PROMPT.md`, `package.json`), runs with `npx tsx`.
- Python source: `harness/`.

- [ ] Step 1: port `tests/test_harness_checks.py` (14) to `harness/checks.test.ts`.
- [ ] Step 2: port the harness; `npx tsx harness/run.ts time-edges --no-agents` completes and its report has no app bugs; commit.

### Task 18: CI/CD, installer, GitHub Pages

**Files:**
- Create: `.github/workflows/test.yml`, `release.yml`, `deploy-pages.yml` (per the spec's CI/CD section and the platform call: linux amd64/arm64/arm with `GOARM=6`, macOS amd64/arm64, Windows amd64; asset names `webquiz-linux.zip`, `webquiz-linux-arm64.zip`, `webquiz-linux-arm.zip`, `webquiz-macos-intel.zip`, `webquiz-macos-apple-silicon.zip`, `webquiz-windows.exe.zip`, `webquiz-windows-setup.exe`, two PDFs); `installer/webquiz.iss` updated; `gh-pages/` with webquiz-go URLs and ARM downloads; `Makefile` release targets (`make dist` cross-compiles all six).

- [ ] Step 1: `make dist` builds all six binaries locally; each runs `--version` where the platform allows (linux amd64 here).
- [ ] Step 2: workflows pass `actionlint` (download it) and dry-run checks; commit.

### Task 19: Docs, books, README, CLAUDE.md

**Files:**
- Modify: `docs/en/*.md`, `docs/uk/*.md` (in sync), `docs/imgs/*.png` (new screenshots from the new UI with Playwright: `scripts/screenshots.ts`), `README.md`, `CLAUDE.md` (rewritten for Go: architecture, packages, API list, boot data, test rules with `py:` tags, commands, port rules, the "every change must include" rules adapted), `gh-pages/docs/*/_sidebar.md` if titles change.

- [ ] Step 1: update content (download/first start without pip, platform minimums, Raspberry Pi with the ARM binary in place of pip, checker rules incl. Starlark notes and the `math.sqrt` fix, new UI descriptions, webquiz-go URLs).
- [ ] Step 2: build both PDFs locally with pandoc if available (else check Markdown renders); commit.

### Task 20: Final verification

- [ ] Step 1: remove the skip in `migration/ported_test.go`; `go test -race ./...` passes with all 911 ids tagged.
- [ ] Step 2: `make e2e`, web unit tests, harness check tests pass.
- [ ] Step 3: full differential run of `/home/user/pytests-go` against the final binary; every failure is explained (Python internals, old HTML) in `migration/README.md`.
- [ ] Step 4: whole-branch review (most capable model) and its fix wave; push.
