# Test migration from Python

`python-tests.txt` lists all 911 test ids of WebQuiz 1.19 (Python), collected with:

```bash
python -m pytest -o addopts="" --collect-only -q tests/ | grep "::"
```

Every ported test names the Python test it ports with a tag:

```go
// py: tests/test_admin_api.py::test_admin_auth_endpoint_with_valid_key
func TestAdminAuthEndpointWithValidKey(t *testing.T) { ... }
```

In table tests the tag can be a field: `{py: "tests/test_decimal_points.py::TestDecimalPointsValidation::test_valid_points_accepted[0.5]", ...}`.
Playwright and harness tests use the same comment: `// py: tests/test_selenium_multiple_choice.py::test_...`.

`ported_test.go` fails when an id has no tag, and when a tag names an id that is not in `python-tests.txt`. It is a normal Go test, so `go test ./...` (and `make test`) runs it. To run only this check:

```bash
go test ./migration/
```

## Running the Python tests against the Go binary

The original 1.19 tests can also run against the Go program. This needs a checkout of WebQuiz 1.19 with its virtual environment, and a copy of its `tests/` folder (`pytests-go` below) with three small changes:

- `conftest.py` takes the first port from `WEBQUIZ_TEST_PORT_BASE` (default 8080). When `WEBQUIZ_GO_BINARY` is set, it starts that program with `--config <file>`, `WEBQUIZ_BINARY_DIR` set to the test's folder and `WEBQUIZ_IS_BINARY=0`.
- `test_cli_directory_creation.py` runs the same program for its command-line tests, on the ports from base + 10.
- Six files import `conftest` instead of `tests.conftest`, because the copy is its own root folder.

Run it from the 1.19 checkout:

```bash
make build    # in this repository: gives bin/webquiz
cd /path/to/webquiz-1.19 && source venv/bin/activate
WEBQUIZ_GO_BINARY=/path/to/webquiz-go/bin/webquiz WEBQUIZ_TEST_PORT_BASE=18800 SKIP_SELENIUM=true \
  python -m pytest /path/to/pytests-go -q -o addopts="" -p no:cacheprovider --rootdir=/path/to/pytests-go -n 4 -rf
```

Result for 0.1 (two runs, the same both times): **657 passed, 59 failed, 47 errors, 148 skipped** (911 tests).

- Skipped (148): the Selenium tests (`SKIP_SELENIUM`). Their ports are the Playwright tests in `e2e/`.
- 133 of the passing tests are Python unit tests that never start the Go program (for example `test_checker.py`, `test_tunnel.py`, `test_harness_checks.py`). They say nothing about Go.

Every failure and error has one of these reasons. Each test has a Go or TypeScript port that checks the same behavior.

**(A) The test reads Python internals or imports `webquiz` modules: 8 failed, 47 errors.**
- `test_transfer.py` (47 errors): unit tests of the Python `TransferManager`. Their async fixture does not run with `-o addopts=""`, and they never start the Go program.
- `test_multiple_answers.py` (2) and `test_time_limits_unit.py` (2): async tests of the Python `TestingServer` object.
- `test_startup_logging.py` (3): expect log lines about the Python runtime ("Python version:", "Python executable:", "aiohttp version:"). The Go program logs its Go version, program file and Go modules there.
- `test_version_check.py` (1): compares with `webquiz.__version__` of the Python package (1.19) and edits that package's `__init__.py`.

**(B) The test reads the old HTML templates or their JavaScript: 51 failed.** The new pages are built with React. They carry the same values in their boot JSON, and the Go ports check them there.
- Old quiz page (34): the variables `let questions`, `let showRightAnswer`, `let showFinalList`, `const quizTimeLimit`; the `<h1>` title, the "WebQuiz v" footer, `<!DOCTYPE html>`, the inline `user-select` style, the registration table labels with a colon, the quiz list of the selection page.
- Old files page (11): `CONFIG_CONTENT`, `CONFIG_DATA`, the transfer panel markup, ids and JavaScript code of the old page.
- Old CSV table page (4): ids, CSS class names and the `parseCSV` function.
- Old admin page (2): `const NETWORK_INFO` and the "File Manager" link text (the new page draws the link with React).
- A check that nothing else hides behind these failures: with a small shim that adds the old variable lines, made from the boot JSON, to each page, 23 of the 51 run to the end and pass, including their later API checks. The other 28 fail only on old markup or old JavaScript code.

**(D) Real differences: none.**
