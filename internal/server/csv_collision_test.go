package server_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// py: tests/test_csv_collision.py::test_csv_path_collision_handling
func TestCSVPathCollisionHandling(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: partAFast()})
	// Create existing CSV files after the start, as the Python test did.
	if err := os.MkdirAll(ts.CSVDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"default_0001.csv", "default_0001.users.csv", "default_0002.csv", "default_0002.users.csv"} {
		if err := os.WriteFile(filepath.Join(ts.CSVDir, name), []byte("# Existing file\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	userID := partARegister(t, ts, map[string]any{"username": "collision_user"})["user_id"].(string)
	res := partAAnswer(t, ts, userID, 1, 1)
	if res.Status != 200 || res.JSON(t)["is_correct"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
	// The existing files are not touched.
	for _, name := range []string{"default_0001.csv", "default_0002.users.csv"} {
		if got, _ := os.ReadFile(filepath.Join(ts.CSVDir, name)); string(got) != "# Existing file\n" {
			t.Errorf("%s changed: %q", name, got)
		}
	}
}
