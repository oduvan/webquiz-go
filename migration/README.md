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
