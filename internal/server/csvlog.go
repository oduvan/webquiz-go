package server

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// The CSV files of a quiz run (server.py generate_csv_path,
// flush_responses_to_csv, _append_answers_with_users_csv and
// flush_users_to_csv). Each run of a quiz writes to csv_dir:
//
//   - <quiz>_NNNN.csv: one row per answer, appended at every flush
//     (Options.FlushInterval, 5 seconds). An answer that waits for the
//     teacher (manual_check) has an empty is_correct; after the teacher's
//     verdict the next flush writes the whole file again from memory
//     (through a temporary file), so the row keeps its place;
//   - <quiz>_NNNN.users.csv: one row per student with the statistics,
//     written again at every flush;
//   - <quiz>_NNNN.answers_with_users.csv (extra_answers_with_users_csv only):
//     the answer rows with the student's name and registration fields.
//
// The three files share NNNN: the first number for which none of the three
// exists, so files of earlier runs (also those of WebQuiz 1.19) stay as
// they are. Each write is flushed to the disk with fsync, as in Python (SD
// cards on a Raspberry Pi lose data that is not synced).

// The columns of the answers CSV after the student columns.
var answerColumns = []string{"question", "selected_answer", "correct_answer", "is_correct", "time_taken_seconds"}

// csvFiles are the CSV files of one quiz run. answersWithUsers is "" when
// extra_answers_with_users_csv is off.
type csvFiles struct {
	answers, users, answersWithUsers string
}

// csvPrefix is the quiz name in the CSV file names: the file name without
// ".yaml" and ".yml" (Python removed them wherever they were in the name).
func csvPrefix(quizFilename string) string {
	return strings.ReplaceAll(strings.ReplaceAll(quizFilename, ".yaml", ""), ".yml", "")
}

// csvPath is <csv_dir>/<quiz>_NNNN<kind>.csv.
func csvPath(dir, quizFilename string, number int, kind string) string {
	return filepath.Join(dir, fmt.Sprintf("%s_%04d%s.csv", csvPrefix(quizFilename), number, kind))
}

// csvRunFiles are the CSV files of run number of quizFilename. The
// answers_with_users file is "" unless extra is set.
func csvRunFiles(dir, quizFilename string, number int, extra bool) csvFiles {
	files := csvFiles{
		answers: csvPath(dir, quizFilename, number, ""),
		users:   csvPath(dir, quizFilename, number, ".users"),
	}
	if extra {
		files.answersWithUsers = csvPath(dir, quizFilename, number, ".answers_with_users")
	}
	return files
}

// freeCSVNumber is the first number from 1 up for which none of the three
// CSV files of a run exists yet (generate_csv_path). Only a file that lstat
// finds takes a number: any error counts as free, like Python's
// os.path.exists, so a name that is too long or a folder the server may not
// read shows as an error when the file is written, instead of a search that
// never ends.
func freeCSVNumber(dir, prefix string, lstat func(string) (os.FileInfo, error)) int {
	for number := 1; ; number++ {
		free := true
		for _, kind := range []string{"", ".users", ".answers_with_users"} {
			if _, err := lstat(filepath.Join(dir, fmt.Sprintf("%s_%04d%s.csv", prefix, number, kind))); err == nil {
				free = false
				break
			}
		}
		if free {
			return number
		}
	}
}

// csvField is a value as Python's csv module wrote it: None as an empty
// field, True and False, numbers with str().
func csvField(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case seconds:
		return v.String()
	case points:
		return v.String()
	}
	return pyval.Str(value)
}

// csvLine is one row as Python's csv.writer wrote it: a field is quoted only
// when it holds a comma, a quote or a line break, quotes are doubled, and the
// row ends with "\r\n".
func csvLine(fields ...string) string {
	var line strings.Builder
	for i, field := range fields {
		if i > 0 {
			line.WriteByte(',')
		}
		if strings.ContainsAny(field, ",\"\r\n") {
			field = `"` + strings.ReplaceAll(field, `"`, `""`) + `"`
		}
		line.WriteString(field)
	}
	line.WriteString("\r\n")
	return line.String()
}

// writeSynced writes text to a file opened with flag, then syncs and closes it.
func writeSynced(path, text string, flag int) error {
	file, err := os.OpenFile(path, flag|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// appendCSV adds rows to a CSV file, with the header first when the file
// does not exist yet. created tells which of the two happened.
func appendCSV(path string, header []string, rows []string) (created bool, err error) {
	_, statErr := os.Stat(path)
	created = errors.Is(statErr, fs.ErrNotExist)
	var text strings.Builder
	if created {
		text.WriteString(csvLine(header...))
	}
	for _, row := range rows {
		text.WriteString(row)
	}
	return created, writeSynced(path, text.String(), os.O_CREATE|os.O_APPEND)
}

// responsesSnapshot is what one flush of the answers writes, copied under
// mu so that the files are written without it.
type responsesSnapshot struct {
	answersFile          string
	answersWithUsersFile string
	answerRows           []string
	fieldNames           []string // the registration field columns of answers_with_users
	answersWithUsersRows []string
	// rewrite: a teacher's verdict changed a row that may be in the file
	// already, so the rows are every answer of the run and the files are
	// written again in full (manual check).
	rewrite bool
}

// takeResponsesLocked takes the answers that wait for the CSV files (and
// clears the list, as Python did before writing). After a teacher's verdict
// it takes every answer row of the run instead, for a rewrite of the files.
// ok is false when there is nothing to write. The caller holds mu.
func (s *Server) takeResponsesLocked() (responsesSnapshot, bool) {
	rows := s.userResponses
	rewrite := s.csvRewriteNeeded && s.csvFile != ""
	if rewrite {
		rows = s.allResponses
		s.csvRewriteNeeded = false
	} else if len(s.userResponses) == 0 {
		return responsesSnapshot{}, false
	}
	snapshot := responsesSnapshot{answersFile: s.csvFile, answersWithUsersFile: s.answersWithUsersCSVFile, rewrite: rewrite}
	for _, row := range rows {
		snapshot.answerRows = append(snapshot.answerRows, csvLine(row.UserID, csvField(row.Question), row.SelectedAnswer,
			csvField(row.CorrectAnswer), row.isCorrectField(), row.TimeTaken.String()))
	}
	if snapshot.answersWithUsersFile != "" {
		for _, label := range s.cfg.Registration.Fields {
			snapshot.fieldNames = append(snapshot.fieldNames, registrationFieldName(label))
		}
		for _, row := range rows {
			// The student as now known: a student of an earlier run is gone,
			// then the name stored with the answer is used.
			username := row.Username
			user := s.users[row.UserID]
			if user != nil {
				username = user.Username
			}
			fields := []string{row.UserID, username}
			for _, name := range snapshot.fieldNames {
				value := ""
				if user != nil {
					value = user.Fields[name]
				}
				fields = append(fields, value)
			}
			fields = append(fields, csvField(row.Question), row.SelectedAnswer, csvField(row.CorrectAnswer),
				row.isCorrectField(), row.TimeTaken.String())
			snapshot.answersWithUsersRows = append(snapshot.answersWithUsersRows, csvLine(fields...))
		}
	}
	s.userResponses = nil
	return snapshot, true
}

// flushResponsesToCSV is flush_responses_to_csv: it appends the answers that
// wait in memory to the answers CSV (and to the answers_with_users CSV), or
// writes both files again after a teacher's verdict. The flush loop calls it
// every s.flushInterval (5 s) and Close once more.
func (s *Server) flushResponsesToCSV() error {
	s.csvMu.Lock()
	defer s.csvMu.Unlock()
	snapshot, ok := s.takeResponses()
	if !ok {
		return nil
	}
	err := s.writeResponses(snapshot)
	if err != nil && snapshot.rewrite {
		// Try the rewrite again at the next flush, if the run is the same
		s.withLock(func() {
			if s.csvFile == snapshot.answersFile {
				s.csvRewriteNeeded = true
			}
		})
	}
	return err
}

// writeResponses appends taken answers to their CSV files, or writes the
// files again for a rewrite. The caller holds csvMu (not mu).
func (s *Server) writeResponses(snapshot responsesSnapshot) error {
	header := append([]string{"user_id"}, answerColumns...)
	withUsersHeader := append(append([]string{"user_id", "username"}, snapshot.fieldNames...), answerColumns...)
	if snapshot.rewrite {
		if err := replaceCSV(snapshot.answersFile, header, snapshot.answerRows); err != nil {
			return err
		}
		s.log.Info(fmt.Sprintf("Rewrote CSV file with %d responses after a teacher's verdict: %s", len(snapshot.answerRows), snapshot.answersFile))
		if snapshot.answersWithUsersFile == "" {
			return nil
		}
		return replaceCSV(snapshot.answersWithUsersFile, withUsersHeader, snapshot.answersWithUsersRows)
	}

	created, err := appendCSV(snapshot.answersFile, header, snapshot.answerRows)
	if err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("%s CSV file with %d responses: %s", createdOrUpdated(created), len(snapshot.answerRows), snapshot.answersFile))

	if snapshot.answersWithUsersFile == "" {
		return nil
	}
	created, err = appendCSV(snapshot.answersWithUsersFile, withUsersHeader, snapshot.answersWithUsersRows)
	if err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("%s answers_with_users CSV file with %d rows: %s",
		createdOrUpdated(created), len(snapshot.answersWithUsersRows), snapshot.answersWithUsersFile))
	return nil
}

// replaceCSV writes a whole CSV file (header and rows) through a temporary
// file: the text goes to <path>.tmp (synced), which then replaces the file,
// so a power cut leaves either the old file or the new one.
func replaceCSV(path string, header []string, rows []string) error {
	var text strings.Builder
	text.WriteString(csvLine(header...))
	for _, row := range rows {
		text.WriteString(row)
	}
	temp := path + ".tmp"
	if err := writeSynced(temp, text.String(), os.O_CREATE|os.O_TRUNC); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// takeResponses is takeResponsesLocked with the lock.
func (s *Server) takeResponses() (responsesSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.takeResponsesLocked()
}

func createdOrUpdated(created bool) string {
	if created {
		return "Created"
	}
	return "Updated"
}

// usersCSVLocked is the whole users CSV of the running quiz: a row per
// student in the order they registered, with their statistics. ok is false
// when there are no students (no file is written then). The caller holds mu.
func (s *Server) usersCSVLocked() (path, text string, ok bool) {
	if len(s.users) == 0 || s.userCSVFile == "" {
		return "", "", false
	}
	var names []string
	for _, label := range s.cfg.Registration.Fields {
		names = append(names, registrationFieldName(label))
	}
	header := append(append([]string{"user_id", "username"}, names...),
		"registered_at", "total_questions_asked", "correct_answers", "earned_points", "total_points", "total_time")
	var csv strings.Builder
	csv.WriteString(csvLine(header...))
	for _, userID := range s.userOrder {
		user, ok := s.users[userID]
		if !ok {
			continue
		}
		row := []string{userID, user.Username}
		for _, name := range names {
			row = append(row, user.Fields[name])
		}
		totals := totalsOf(s.userAnswers[userID])
		row = append(row, user.RegisteredAt, fmt.Sprint(totals.count), fmt.Sprint(totals.correct),
			points(totals.earnedPoints).String(), points(totals.totalPoints).String(), minutesAndSeconds(totals.time.value))
		csv.WriteString(csvLine(row...))
	}
	return s.userCSVFile, csv.String(), true
}

// usersCSV is usersCSVLocked with the lock.
func (s *Server) usersCSV() (path, text string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usersCSVLocked()
}

// minutesAndSeconds writes a time as Python's f"{int(t // 60)}:{int(t % 60):02d}".
func minutesAndSeconds(total float64) string {
	minutes, rest := pyDivmod(total, 60)
	return fmt.Sprintf("%d:%02d", int(minutes), int(rest))
}

// pyDivmod is Python's divmod for floats (float_divmod in CPython): the
// floor of x / y and the rest, with the sign of y.
func pyDivmod(x, y float64) (float64, float64) {
	mod := math.Mod(x, y)
	div := (x - mod) / y
	if mod != 0 {
		if (y < 0) != (mod < 0) {
			mod += y
			div--
		}
	}
	if div != 0 {
		floor := math.Floor(div)
		if div-floor > 0.5 {
			floor++
		}
		div = floor
	}
	return div, mod
}

// flushUsersToCSV is flush_users_to_csv: it writes the users CSV again with
// every student of the run and their statistics.
func (s *Server) flushUsersToCSV() error {
	s.csvMu.Lock()
	defer s.csvMu.Unlock()
	path, text, ok := s.usersCSV()
	if !ok {
		return nil
	}
	return writeUsersCSV(path, text)
}

// writeUsersCSV writes the users CSV of a run again.
func writeUsersCSV(path, text string) error {
	return writeSynced(path, text, os.O_CREATE|os.O_TRUNC)
}

// runCSVRows are the rows of a run that wait for the CSV files: the taken
// answers and the users CSV. A quiz switch takes them together with the
// reset, so no answer of the old run is lost, and writes them before the
// new run chooses its CSV number.
type runCSVRows struct {
	responses    responsesSnapshot
	hasResponses bool
	usersPath    string
	usersText    string
	hasUsers     bool
}

// takeRunRowsLocked takes the rows of the running quiz that wait for the
// CSV files. The caller holds csvMu and mu.
func (s *Server) takeRunRowsLocked() runCSVRows {
	var rows runCSVRows
	rows.responses, rows.hasResponses = s.takeResponsesLocked()
	rows.usersPath, rows.usersText, rows.hasUsers = s.usersCSVLocked()
	return rows
}

// writeRunRows writes rows taken by takeRunRowsLocked, like one flush. An
// error is logged as the flush loop logs it. The caller holds csvMu (not mu).
func (s *Server) writeRunRows(rows runCSVRows) {
	if rows.hasResponses {
		s.runSafely("flush_responses_to_csv", func() error { return s.writeResponses(rows.responses) })
	}
	if rows.hasUsers {
		s.runSafely("flush_users_to_csv", func() error { return writeUsersCSV(rows.usersPath, rows.usersText) })
	}
}
