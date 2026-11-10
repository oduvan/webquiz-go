package server

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/pyval"
)

// The CSV files of a quiz run (server.py generate_csv_path,
// flush_responses_to_csv, _append_answers_with_users_csv and
// flush_users_to_csv). Each run of a quiz writes to csv_dir:
//
//   - <quiz>_NNNN.csv: one row per answer, appended every 5 seconds;
//   - <quiz>_NNNN.users.csv: one row per student with the statistics,
//     written again every 5 seconds;
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

// newCSVFilesLocked chooses the CSV files of a new run of quizFilename
// (generate_csv_path for each kind) and makes csv_dir. The caller holds
// csvMu, so no flush writes a file while the names are chosen, and mu.
func (s *Server) newCSVFilesLocked(quizFilename string) (csvFiles, error) {
	if _, err := config.EnsureDir(s.csvDir); err != nil {
		return csvFiles{}, err
	}
	number := freeCSVNumber(s.csvDir, csvPrefix(quizFilename))
	files := csvFiles{
		answers: csvPath(s.csvDir, quizFilename, number, ""),
		users:   csvPath(s.csvDir, quizFilename, number, ".users"),
	}
	if s.cfg.ExtraAnswersWithUsersCSV {
		files.answersWithUsers = csvPath(s.csvDir, quizFilename, number, ".answers_with_users")
	}
	return files, nil
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

// freeCSVNumber is the first number from 1 up for which none of the three
// CSV files of a run exists yet.
func freeCSVNumber(dir, prefix string) int {
	for number := 1; ; number++ {
		free := true
		for _, kind := range []string{"", ".users", ".answers_with_users"} {
			if _, err := os.Lstat(filepath.Join(dir, fmt.Sprintf("%s_%04d%s.csv", prefix, number, kind))); !errors.Is(err, fs.ErrNotExist) {
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
}

// takeResponsesLocked takes the answers that wait for the CSV files (and
// clears the list, as Python did before writing). ok is false when there is
// nothing to write. The caller holds mu.
func (s *Server) takeResponsesLocked() (responsesSnapshot, bool) {
	if len(s.userResponses) == 0 {
		return responsesSnapshot{}, false
	}
	snapshot := responsesSnapshot{answersFile: s.csvFile, answersWithUsersFile: s.answersWithUsersCSVFile}
	for _, row := range s.userResponses {
		snapshot.answerRows = append(snapshot.answerRows, csvLine(row.UserID, csvField(row.Question), row.SelectedAnswer,
			csvField(row.CorrectAnswer), csvField(row.IsCorrect), row.TimeTaken.String()))
	}
	if snapshot.answersWithUsersFile != "" {
		for _, label := range s.cfg.Registration.Fields {
			snapshot.fieldNames = append(snapshot.fieldNames, registrationFieldName(label))
		}
		for _, row := range s.userResponses {
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
				csvField(row.IsCorrect), row.TimeTaken.String())
			snapshot.answersWithUsersRows = append(snapshot.answersWithUsersRows, csvLine(fields...))
		}
	}
	s.userResponses = nil
	return snapshot, true
}

// flushResponsesToCSV is flush_responses_to_csv: it appends the answers that
// wait in memory to the answers CSV (and to the answers_with_users CSV). The
// flush loop calls it every 5 seconds and Close once more.
func (s *Server) flushResponsesToCSV() error {
	s.csvMu.Lock()
	defer s.csvMu.Unlock()
	snapshot, ok := s.takeResponses()
	if !ok {
		return nil
	}
	header := append([]string{"user_id"}, answerColumns...)
	created, err := appendCSV(snapshot.answersFile, header, snapshot.answerRows)
	if err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("%s CSV file with %d responses: %s", createdOrUpdated(created), len(snapshot.answerRows), snapshot.answersFile))

	if snapshot.answersWithUsersFile == "" {
		return nil
	}
	header = append(append([]string{"user_id", "username"}, snapshot.fieldNames...), answerColumns...)
	created, err = appendCSV(snapshot.answersWithUsersFile, header, snapshot.answersWithUsersRows)
	if err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("%s answers_with_users CSV file with %d rows: %s",
		createdOrUpdated(created), len(snapshot.answersWithUsersRows), snapshot.answersWithUsersFile))
	return nil
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
	return writeSynced(path, text, os.O_CREATE|os.O_TRUNC)
}
