package server_test

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Helpers for the ported student-flow tests, part A. The names start with
// "partA" so they cannot clash with helpers of other test files.

// partAFast makes the server write the CSV files every 20 ms. The Python
// tests slept 6 seconds for the 5 second flush.
func partAFast() server.Options { return server.Options{FlushInterval: 20 * time.Millisecond} }

// partACSVFiles lists the CSV files of the server that match a pattern like
// "test_quiz_*.users.csv", sorted by name.
func partACSVFiles(t *testing.T, ts *testutil.TestServer, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(ts.CSVDir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// partAAnswerCSVs lists the answer CSV files ("quiz_NNNN.csv"), without the
// .users.csv and .answers_with_users.csv files.
func partAAnswerCSVs(t *testing.T, ts *testutil.TestServer, quiz string) []string {
	t.Helper()
	var out []string
	for _, file := range partACSVFiles(t, ts, quiz+"_*.csv") {
		if !strings.HasSuffix(file, ".users.csv") && !strings.HasSuffix(file, ".answers_with_users.csv") {
			out = append(out, file)
		}
	}
	return out
}

// partAReadCSV reads a CSV file: the header row and the rows as maps.
func partAReadCSV(t *testing.T, path string) ([]string, []map[string]string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	var rows []map[string]string
	for _, record := range records[1:] {
		row := map[string]string{}
		for i, name := range records[0] {
			if i < len(record) {
				row[name] = record[i]
			}
		}
		rows = append(rows, row)
	}
	return records[0], rows
}

// partAWaitCSV waits until the CSV file with this suffix ("" for the answers
// file, ".users" or ".answers_with_users") of the quiz has at least minRows
// data rows, then returns its header and rows.
func partAWaitCSV(t *testing.T, ts *testutil.TestServer, quiz, suffix string, minRows int) ([]string, []map[string]string) {
	t.Helper()
	var headers []string
	var rows []map[string]string
	partAWaitFor(t, "CSV rows in "+quiz+suffix, func() bool {
		var files []string
		if suffix == "" {
			files = partAAnswerCSVs(t, ts, quiz)
		} else {
			files = partACSVFiles(t, ts, quiz+"_*"+suffix+".csv")
		}
		if len(files) == 0 {
			return false
		}
		headers, rows = partAReadCSV(t, files[0])
		return len(rows) >= minRows
	})
	return headers, rows
}

func partAHas(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}

// partARegister registers a student with a body and fails the test unless
// the answer is 200. It returns the reply.
func partARegister(t *testing.T, ts *testutil.TestServer, body map[string]any) map[string]any {
	t.Helper()
	res := ts.PostJSON(t, "/api/register", body)
	if res.Status != 200 {
		t.Fatalf("register %v: %d %s", body, res.Status, res.Body)
	}
	return res.JSON(t)
}

// partAAnswer sends an answer and returns the response.
func partAAnswer(t *testing.T, ts *testutil.TestServer, userID string, question int, answer any) *testutil.Response {
	t.Helper()
	return ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": question, "selected_answer": answer})
}

// paFloatField reads a JSON number field (json.Number) as a float.
func paFloatField(t *testing.T, data map[string]any, name string) float64 {
	t.Helper()
	number, ok := data[name].(interface{ Float64() (float64, error) })
	if !ok {
		t.Fatalf("%s is not a number in %v", name, data)
	}
	value, err := number.Float64()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// partAWaitFor polls until ok returns true (at most 5 seconds).
func partAWaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
