package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
)

// Rows are written like Python's csv.writer (expected text taken from
// Python 3.13): quotes only for commas, quotes and line breaks; None empty;
// True/False; floats with repr.
func TestCSVLineLikePython(t *testing.T) {
	got := csvLine("a", " lead", "tab\tx", `q"x`, "c,x", "n\nx", "r\rx", "", csvField(nil), csvField(true),
		csvField(1.0), csvField(0), "é", "#x", `\x`, "sp ")
	want := "a, lead,tab\tx,\"q\"\"x\",\"c,x\",\"n\nx\",\"r\rx\",,,True,1.0,0,é,#x,\\x,sp \r\n"
	if got != want {
		t.Errorf("csvLine:\n%q\nwant\n%q", got, want)
	}
}

// total_time in the users CSV is f"{int(t // 60)}:{int(t % 60):02d}".
func TestMinutesAndSecondsLikePython(t *testing.T) {
	for total, want := range map[float64]string{
		0: "0:00", 0.5: "0:00", 59.999999: "0:59", 60: "1:00", 125.5: "2:05", 3600: "60:00", 119.99999999999999: "1:59",
	} {
		if got := minutesAndSeconds(total); got != want {
			t.Errorf("minutesAndSeconds(%v) = %q, want %q", total, got, want)
		}
	}
}

// generate_csv_path: the three files of a run share the first number that
// is free for all three; ".yaml" and ".yml" are removed from the quiz name.
func TestCSVFileNumbers(t *testing.T) {
	dir := t.TempDir()
	if got := freeCSVNumber(dir, "quiz", os.Lstat); got != 1 {
		t.Errorf("empty folder: %d", got)
	}
	for _, name := range []string{"quiz_0001.answers_with_users.csv", "quiz_0002.users.csv", "other_0003.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := freeCSVNumber(dir, "quiz", os.Lstat); got != 3 {
		t.Errorf("number %d, want 3 (one file of a run is enough to take the number)", got)
	}
	if got := csvPrefix("week.yaml.final.yml"); got != "week.final" {
		t.Errorf("csvPrefix = %q", got)
	}
	if got := csvPath(dir, "Тест.yaml", 12, ".users"); got != filepath.Join(dir, "Тест_0012.users.csv") {
		t.Errorf("csvPath = %q", got)
	}
}

// Times and points go to JSON and the CSV files as Python wrote them.
func TestSecondsAndPointsLikePython(t *testing.T) {
	cases := map[string]any{
		"0":                      zeroSeconds,
		"3.0":                    seconds{3, true},
		"1e-06":                  seconds{0.000001, true},
		"2.5":                    seconds{2.5, true},
		"30":                     remainingSeconds(30, zeroSeconds),           // max(0.0, 30 - 0)
		"29.5":                   remainingSeconds(30, seconds{0.5, true}),    // max(0.0, 30 - 0.5)
		"0.0":                    remainingSeconds(1, seconds{2, true}),       // max(0.0, 1 - 2.0)
		"4.0":                    seconds{1.5, true}.plus(seconds{2.5, true}), // a sum of floats
		"1.5":                    zeroSeconds.plus(seconds{1.5, true}),        // 0 + 1.5
		"3":                      points(3),
		"0.5":                    points(0.5),
		"0.3":                    points(0.1 + 0.2),
		"1000000000000000000000": points(1e21),
	}
	for want, value := range cases {
		data, err := encodeJSON(value)
		if err != nil || string(data) != want {
			t.Errorf("JSON of %#v: %s %v, want %s", value, data, err, want)
		}
		if got := csvField(value); got != want {
			t.Errorf("CSV of %#v: %s, want %s", value, got, want)
		}
	}
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	if got := secondsBetween(start, start.Add(1500*time.Millisecond+999)); got != (seconds{1.5, true}) {
		t.Errorf("secondsBetween keeps whole microseconds: %#v", got)
	}
	if got := isoformat(start); got != "2026-11-09T12:00:00" {
		t.Errorf("isoformat: %s", got)
	}
	if got := isoformat(start.Add(5 * time.Microsecond)); got != "2026-11-09T12:00:00.000005" {
		t.Errorf("isoformat: %s", got)
	}
}

// The keys of the start settings are taken in the order of the request.
func TestJSONKeyOrder(t *testing.T) {
	for body, want := range map[string][]string{
		`{"quiz_filename": "a.yaml", "settings": {"z": 1, "b": {"c": 2}, "z": 3}}`: {"z", "b"},
		`{"settings": {"a": 1}, "settings": {"y": 1, "x": [1, {"q": 2}]}}`:         {"y", "x"},
		`{"settings": {}}`:     {},
		`{"settings": [1, 2]}`: nil,
		`{"quiz_filename": 1}`: nil,
		`[1]`:                  nil,
	} {
		if got := jsonKeyOrder([]byte(body), "settings"); !slices.Equal(got, want) || (got == nil) != (want == nil) {
			t.Errorf("%s: %q, want %q", body, got, want)
		}
	}
}

// timedServer is a server whose one-question quiz has the given time_limit
// line, with a test clock, no grace and one student "u1".
func timedServer(t *testing.T, limit string) (*Server, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	quiz := "title: Timed\ntime_limit: " + limit + "\nquestions:\n- {question: 'Q1', checker: 'assert True', correct_value: x}\n- {question: 'Q2', options: [A, B], correct_answer: 0}\n"
	s := newTestServer(t, map[string]string{"timed.yaml": quiz}, func(_ *config.Config, opts *Options) {
		opts.Grace = NoGrace
		opts.Now = func() time.Time { return clock }
	})
	s.withLock(func() { s.addUserLocked(&User{ID: "u1", Username: "Ann", Approved: true}) })
	return s, &clock
}

// While a student's answer is being checked (the checker runs without the
// lock), the time limit watcher leaves that student alone; afterwards it
// stores the timeout as usual.
func TestWatcherWaitsForAnswerBeingChecked(t *testing.T) {
	for _, limit := range []string{"{per_question: 10}", "{whole_quiz: 10}"} {
		s, clock := timedServer(t, limit)
		if _, reply, err := s.startQuestion("u1", 1); err != nil || reply["status"] != "success" {
			t.Fatalf("%s: question-start: %v %v", limit, reply, err)
		}
		*clock = clock.Add(11 * time.Second)
		s.withLock(func() { s.answersBeingChecked["u1"] = 1 })
		if err := s.checkTimeLimits(*clock); err != nil {
			t.Fatal(err)
		}
		s.withLock(func() {
			if len(s.userAnswers["u1"]) != 0 {
				t.Errorf("%s: a timeout was stored while the answer was being checked", limit)
			}
			delete(s.answersBeingChecked, "u1")
		})
		if err := s.checkTimeLimits(*clock); err != nil {
			t.Fatal(err)
		}
		s.withLock(func() {
			if answers := s.userAnswers["u1"]; len(answers) == 0 || !answers[0].TimedOut || answers[0].TimeTaken != (seconds{11, true}) {
				t.Errorf("%s: no timeout after the check ended: %+v", limit, answers)
			}
		})
	}
}

// An answer whose checker ran without the lock is checked again under the
// lock with what happened meanwhile: a retry that was stored first, a quiz
// switch. The count of answers being checked goes back to zero.
func TestCheckedAnswerSeesChangesMadeMeanwhile(t *testing.T) {
	s, _ := timedServer(t, "{per_question: 0}")
	first := &submission{rawUserID: "u1", rawQuestionID: 1, answer: "x"}
	if _, _, job, err := s.submitStep(first); err != nil || job == nil {
		t.Fatalf("the first step must ask for the checker: %v %v", job, err)
	}
	s.withLock(func() {
		if s.answersBeingChecked["u1"] != 1 {
			t.Errorf("answersBeingChecked = %v", s.answersBeingChecked)
		}
	})

	// A retry with the same answer runs through completely meanwhile.
	retry := &submission{rawUserID: "u1", rawQuestionID: 1, answer: "x"}
	for {
		_, reply, job, err := s.submitStep(retry)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			if reply["is_correct"] != true {
				t.Fatalf("retry reply: %v", reply)
			}
			break
		}
		retry.grade = gradeAnswer(job.quiz, job.question, job.answer)
	}
	first.grade = gradeAnswer(s.quiz, s.quiz.Questions[0], "x")
	status, reply, job, err := s.submitStep(first)
	if err != nil || job != nil || status != 200 || reply["is_correct"] != true {
		t.Fatalf("the first request after its checker: %d %v %v %v", status, reply, job, err)
	}
	s.withLock(func() {
		if len(s.userAnswers["u1"]) != 1 || len(s.answersBeingChecked) != 0 {
			t.Errorf("answers %d, being checked %v", len(s.userAnswers["u1"]), s.answersBeingChecked)
		}
	})

	// A quiz switch while the checker runs: the student is gone.
	s.withLock(func() { s.addUserLocked(&User{ID: "u2", Username: "Bob", Approved: true}) })
	late := &submission{rawUserID: "u2", rawQuestionID: 1, answer: "x"}
	if _, _, job, _ := s.submitStep(late); job == nil {
		t.Fatal("the first step must ask for the checker")
	}
	if err := s.SwitchQuiz("timed.yaml", nil); err != nil {
		t.Fatal(err)
	}
	late.grade = gradeAnswer(s.quiz, s.quiz.Questions[0], "x")
	if status, reply, _, _ := s.submitStep(late); status != 404 || reply["error"] != s.translations["server_user_not_found"] {
		t.Errorf("after a switch: %d %v", status, reply)
	}
	s.withLock(func() {
		if len(s.answersBeingChecked) != 0 {
			t.Errorf("being checked after the switch: %v", s.answersBeingChecked)
		}
	})
}

// Only a file that lstat finds takes a CSV number. Any other error counts as
// free, like Python's os.path.exists: a name that is too long or a folder
// the server may not read must not make the search go on forever (it runs
// while csvMu is held). The problem shows when the file is written.
func TestFreeCSVNumberCountsOnlyFoundFiles(t *testing.T) {
	found := map[string]bool{"quiz_0001.csv": true, "quiz_0002.users.csv": true}
	cases := map[string]struct {
		lstat func(string) (os.FileInfo, error)
		want  int
	}{
		"name too long": {func(string) (os.FileInfo, error) { return nil, syscall.ENAMETOOLONG }, 1},
		"no permission": {func(string) (os.FileInfo, error) { return nil, fs.ErrPermission }, 1},
		"found files": {func(path string) (os.FileInfo, error) {
			if found[filepath.Base(path)] {
				return nil, nil
			}
			return nil, fs.ErrNotExist
		}, 3},
	}
	for name, c := range cases {
		result := make(chan int, 1)
		go func() { result <- freeCSVNumber("data", "quiz", c.lstat) }()
		select {
		case got := <-result:
			if got != c.want {
				t.Errorf("%s: number %d, want %d", name, got, c.want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: the search does not end", name)
		}
	}
}

// A quiz whose name is so long that <name>_0001.answers_with_users.csv is
// longer than a file name may be still starts, and its answers are written.
func TestVeryLongQuizNameStarts(t *testing.T) {
	name := strings.Repeat("ж", 115) + ".yaml" // 235 bytes; the answers_with_users name would be 258
	started := make(chan *Server, 1)
	go func() {
		started <- newTestServer(t, map[string]string{name: quizText("Long")}, nil)
	}()
	var s *Server
	select {
	case s = <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not start: the CSV number search does not end")
	}
	s.withLock(func() {
		if got := filepath.Base(s.csvFile); got != strings.Repeat("ж", 115)+"_0001.csv" {
			t.Errorf("answers CSV %s", got)
		}
	})
}

// The time limit check of the next question counts from the moment the
// answer arrived (Python set question_ready_times at the start of
// _record_answer), not from the end of a slow checker.
func TestReadyTimeIsTheArrivalOfTheAnswer(t *testing.T) {
	s, clock := timedServer(t, "{per_question: 0}")
	arrived := *clock
	sub := &submission{rawUserID: "u1", rawQuestionID: 1, answer: "x"}
	if _, _, job, err := s.submitStep(sub); err != nil || job == nil {
		t.Fatalf("the first step must ask for the checker: %v %v", job, err)
	}
	*clock = clock.Add(3 * time.Second) // the checker takes its time
	sub.grade = gradeAnswer(s.quiz, s.quiz.Questions[0], "x")
	if status, _, _, err := s.submitStep(sub); err != nil || status != 200 {
		t.Fatalf("second step: %d %v", status, err)
	}
	s.withLock(func() {
		if got := s.questionReadyTimes["u1"]; !got.Equal(arrived) {
			t.Errorf("question_ready_times = %v, want the arrival %v", got, arrived)
		}
	})
}
