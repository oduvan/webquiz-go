package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oduvan/webquiz-go/internal/checker"
	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/i18n"
	"github.com/oduvan/webquiz-go/internal/quiz"
	"github.com/oduvan/webquiz-go/internal/timelimits"
	"github.com/oduvan/webquiz-go/internal/tunnel"
	"github.com/oduvan/webquiz-go/web"
)

// The quiz rules (internal/quiz) run checker code through these hooks. They
// are set once, when the program starts, before any server exists.
func init() {
	quiz.RunChecker = func(code, answer string) (bool, string) {
		result := checker.Check(code, answer)
		return result.Correct, result.Error
	}
	quiz.ValidateChecker = func(code string) (string, int, bool) {
		if err := checker.Validate(code); err != nil {
			return err.Msg, err.Line, false
		}
		return "", 0, true
	}
}

// NoGrace is the Options.Grace for "no grace at all": an answer that comes
// after the time limit is late.
const NoGrace time.Duration = -1

// DefaultFlushInterval is how often answers and users are written to the
// CSV files (periodic_flush).
const DefaultFlushInterval = 5 * time.Second

// Options are settings of a server that do not come from webquiz.yaml.
type Options struct {
	// Grace is how long after a time limit an answer still counts. Zero means
	// the default: timelimits.GraceSeconds(), which reads
	// WEBQUIZ_TIME_LIMIT_GRACE_SECONDS (5 s when it is not set). NoGrace (or
	// any negative value) means no grace.
	Grace time.Duration
	// CheckInterval is how often the time limit watcher runs. Zero means
	// timelimits.CheckInterval() (WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL, 1 s).
	CheckInterval time.Duration
	// FlushInterval is how often waiting answers and the users are written to
	// the CSV files. Zero means DefaultFlushInterval (5 s, as in Python);
	// tests make it short so they need not wait.
	FlushInterval time.Duration
	// Now is the clock (nil: time.Now). Tests can move time with it.
	Now func() time.Time
	// Logger gets every log line in addition to the log file in logs_dir.
	// Nil means the console (stderr), in the same format as the file.
	Logger *slog.Logger
	// Pages are the built web pages (web/dist). Nil means the pages built
	// into the program (package web).
	Pages fs.FS
}

// User is a registered student: an entry of Python's users dict.
type User struct {
	ID            string            // user_id: 6 digits
	Username      string            // username
	RegisteredAt  string            // registered_at: when the student registered, ISO format
	Approved      bool              // approved: false while the student waits for the teacher
	Fields        map[string]string // the extra registration fields: field name -> value
	QuestionOrder []int             // question_order: the student's own order of question ids; nil when not shuffled
}

// answerResult is an entry of Python's answer_results: the stored reply to
// an answer, so that a repeated request gets the same reply.
type answerResult struct {
	SelectedAnswer any            // the answer as the request sent it
	Response       map[string]any // the reply
	TimedOut       bool           // the answer was a timeout
}

// liveState is the state of one question of one student on the live stats
// page: "think", "ok", "fail" or "timeout", and the seconds it took (null
// while the student thinks).
type liveState struct {
	State     string   `json:"state"`
	TimeTaken *seconds `json:"time_taken"`
}

// Server is the WebQuiz server, Python's TestingServer.
//
// Concurrency: mu guards every field below it. Handlers do their state work
// while holding mu and let it go before slow work (HTTP calls, big file
// writes). WebSocket messages are only queued under mu (see broadcastLiveStats);
// the sending happens in other goroutines. Lock order: mu, then a hub's lock.
type Server struct {
	// Fixed when the server is made.
	grace         time.Duration
	checkInterval time.Duration
	flushInterval time.Duration
	now           func() time.Time
	pages         fs.FS
	log           *slog.Logger
	logOut        *lockedWriter
	quizzesDir    string // paths.quizzes_dir
	logsDir       string // paths.logs_dir
	csvDir        string // paths.csv_dir
	staticDir     string // paths.static_dir
	logFile       string // log_file: logs_dir/NNNN.log
	masterKey     string // master_key: a change in the config needs a restart
	handler       http.Handler

	// ctx ends when Close is called. Use it, not a request's context, for
	// work that must outlive the request: aiohttp did not stop a handler when
	// its client went away. tunnel.Manager.Connect is such work.
	ctx    context.Context
	cancel context.CancelFunc

	liveStatsClients *wsHub // websocket_clients: the live stats pages
	adminClients     *wsHub // admin_websocket_clients: the admin pages

	// tunnel is the SSH tunnel (tunnel_manager); nil when the config has no
	// tunnel server. Its status callback must not take mu.
	tunnel      *tunnel.Manager
	tunnelNames atomic.Pointer[tunnelNames] // tunnel.server and socket_name for its status messages

	loops     sync.WaitGroup // the flush loop and the time limit watcher
	startOnce sync.Once
	closeOnce sync.Once

	// csvMu lets one CSV flush or one choice of new CSV file names (a quiz
	// switch) happen at a time, so a new run never picks the number of files
	// that a flush is still writing. Lock order: csvMu, then mu.
	csvMu sync.Mutex

	mu sync.Mutex

	// Settings that the admin can change while the server runs (config hot reload).
	cfg          *config.Config    // config: the server's own copy
	translations map[string]string // translations of cfg.Language; never changed in place
	// adminSessions holds the admin session tokens (cookie admin_session)
	// with their creation time. Sessions stay until the server stops.
	adminSessions map[string]time.Time

	// The running quiz.
	currentQuizFile         string     // current_quiz_file: path in quizzes_dir; "" until a quiz runs
	csvFile                 string     // csv_file: the answers CSV of this run
	userCSVFile             string     // user_csv_file: the users CSV of this run
	answersWithUsersCSVFile string     // answers_with_users_csv_file; "" when not enabled
	quiz                    *quiz.Quiz // the running quiz with the start settings applied; a switch sets a new one, the old one is never changed
	startSettings           map[string]any
	startSettingsQuiz       string // start_settings_quiz: the quiz file the start settings belong to

	// The state of one quiz run (reset_server_state clears it).
	users              map[string]*User                 // users: user_id -> student
	userOrder          []string                         // the user_ids in the order the students registered (Python's dict order)
	userResponses      []*answerRow                     // user_responses: answers not yet written to the CSV files
	userProgress       map[string]int                   // user_progress: user_id -> last answered question id
	questionStartTimes map[string]time.Time             // question_start_times: user_id -> start of the running question clock
	currentQuestionIDs map[string]int                   // current_question_ids: user_id -> question whose clock runs
	quizStartTimes     map[string]time.Time             // quiz_start_times: user_id -> first question-start (whole-quiz clock)
	questionReadyTimes map[string]time.Time             // question_ready_times: user_id -> when the next question became available
	userStats          map[string]*userStats            // user_stats: user_id -> final results of a student who finished
	userAnswers        map[string][]*answerRecord       // user_answers: user_id -> answers for the statistics
	answerResults      map[string]map[int]*answerResult // answer_results: user_id -> question id -> stored reply
	registrationTokens map[string]string                // registration_tokens: token -> user_id
	liveStats          map[string]map[int]liveState     // live_stats: user_id -> question id -> state
	forceAllCompleted  bool                             // force_all_completed: the admin pressed "Show answers"

	// answersBeingChecked counts, per user_id, the answers whose checker runs
	// right now without the lock (see submitAnswer). The time limit watcher
	// leaves those students alone until their answer is stored, as Python's
	// watcher could not run while a checker ran. A quiz switch does not clear
	// it: it counts requests, not quiz state.
	answersBeingChecked map[string]int
}

// tunnelNames are the config values a tunnel status message carries.
type tunnelNames struct {
	server     string
	socketName string
}

// New makes a server and does what create_app did before serving: it makes
// the log file (logs_dir/NNNN.log) and the logger, prepares the SSH tunnel
// keys (it does not connect), loads the quiz (writing static_dir/index.html)
// and makes the quizzes/imgs and quizzes/attach folders. Like the Python
// version it fails when the quiz it has to start cannot be loaded.
//
// The server works on its own copy of cfg. Call Start to run the background
// work, serve Handler, and call Close at the end.
func New(cfg *config.Config, opts Options) (*Server, error) {
	own := copyConfig(cfg)
	s := &Server{
		grace:         opts.Grace,
		checkInterval: opts.CheckInterval,
		flushInterval: opts.FlushInterval,
		now:           opts.Now,
		pages:         opts.Pages,
		quizzesDir:    own.Paths.QuizzesDir,
		logsDir:       own.Paths.LogsDir,
		csvDir:        own.Paths.CSVDir,
		staticDir:     own.Paths.StaticDir,
		cfg:           own,
		masterKey:     own.Admin.MasterKey,
		translations:  i18n.Get(own.Language),
		adminSessions: map[string]time.Time{},
		quiz:          emptyQuiz(),
		startSettings: map[string]any{},

		answersBeingChecked: map[string]int{},
	}
	if s.grace == 0 {
		s.grace = time.Duration(timelimits.GraceSeconds() * float64(time.Second))
	}
	if s.grace < 0 {
		s.grace = 0
	}
	if s.checkInterval <= 0 {
		s.checkInterval = timelimits.CheckInterval()
	}
	if s.flushInterval <= 0 {
		s.flushInterval = DefaultFlushInterval
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.pages == nil {
		dist, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			return nil, err
		}
		s.pages = dist
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())

	var err error
	s.logFile, s.logOut, s.log, err = openLog(s.logsDir, opts.Logger)
	if err != nil {
		s.cancel()
		return nil, fmt.Errorf("create the log file in %s: %w", s.logsDir, err)
	}
	s.log.Info(fmt.Sprintf("=== Server Started - New Log File Created: %s ===", s.logFile))
	s.liveStatsClients = newWSHub("WebSocket", "live stats WebSocket", s.log)
	s.adminClients = newWSHub("admin WebSocket", "admin WebSocket", s.log)
	s.resetStateLocked()

	s.logStartupEnvironment()
	s.initTunnel()
	if err := s.loadQuestions(); err != nil {
		s.log.Error(fmt.Sprintf("Failed to load the quiz: %v", err))
		s.shutdown()
		return nil, err
	}
	for _, dir := range []string{"imgs", "attach"} {
		if err := os.MkdirAll(filepath.Join(s.quizzesDir, dir), 0o755); err != nil {
			s.shutdown()
			return nil, err
		}
	}
	s.handler = s.routes()
	return s, nil
}

// copyConfig copies the parts of cfg that the server changes, so the
// caller's value stays as it was.
func copyConfig(cfg *config.Config) *config.Config {
	own := *cfg
	own.Admin.TrustedIPs = slices.Clone(cfg.Admin.TrustedIPs)
	own.Registration.Fields = slices.Clone(cfg.Registration.Fields)
	own.Registration.DisabledFields = slices.Clone(cfg.Registration.DisabledFields)
	own.Quizzes = slices.Clone(cfg.Quizzes)
	own.CheckerTemplates = slices.Clone(cfg.CheckerTemplates)
	own.LoadWarnings = slices.Clone(cfg.LoadWarnings)
	if cfg.Tunnel.Config != nil {
		tunnelConfig := *cfg.Tunnel.Config
		own.Tunnel.Config = &tunnelConfig
	}
	return &own
}

// emptyQuiz is the quiz before any quiz file is loaded, with the defaults
// of TestingServer.__init__.
func emptyQuiz() *quiz.Quiz {
	return &quiz.Quiz{
		Data:               map[string]any{},
		Title:              quiz.DefaultTitle,
		ShowRightAnswer:    true,
		ShowFinalList:      true,
		TimeLimit:          timelimits.NoLimit,
		QuestionTimeLimits: map[int]int{},
		ExplanationsHTML:   map[int]string{},
	}
}

// Handler is the HTTP handler with every route of the server.
func (s *Server) Handler() http.Handler { return s.handler }

// LogFile is the path of this server's log file.
func (s *Server) LogFile() string { return s.logFile }

// Start runs the background work of create_app: the CSV flush every
// Options.FlushInterval (periodic_flush) and the time limit watcher
// (time_limit_watcher).
// It returns at once. The work stops when ctx ends or Close is called. A
// second Start does nothing.
func (s *Server) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		s.loops.Go(func() { s.every(ctx, s.flushInterval, s.flushCSV) })
		s.loops.Go(func() {
			s.every(ctx, s.checkInterval, func() { s.runSafely("check_time_limits", func() error { return s.checkTimeLimits(s.now()) }) })
		})
	})
}

// every calls work every interval until ctx ends or the server closes.
func (s *Server) every(ctx context.Context, interval time.Duration, work func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			work()
		}
	}
}

// flushCSV writes waiting answers and the users to the CSV files, like one
// round of periodic_flush. The two steps fail on their own.
func (s *Server) flushCSV() {
	s.runSafely("flush_responses_to_csv", s.flushResponsesToCSV)
	s.runSafely("flush_users_to_csv", s.flushUsersToCSV)
}

// runSafely runs one piece of background work and logs its error or panic
// ("Error during <name>: ..."), so the loop goes on, as in Python.
func (s *Server) runSafely(name string, work func() error) {
	defer func() {
		if value := recover(); value != nil {
			s.log.Error(fmt.Sprintf("Error during %s: %v", name, value))
		}
	}()
	if err := work(); err != nil {
		s.log.Error(fmt.Sprintf("Error during %s: %v", name, err))
	}
}

// Close stops the server's own work: the background loops, the WebSocket
// clients and the tunnel. Answers that wait for the 5-second flush are
// written to the CSV files first (Python lost them on exit). Then the log
// file is closed. Close does not stop the HTTP server that serves Handler;
// stop that first. Calling Close again does nothing.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.loops.Wait()
		s.flushCSV()
		s.shutdown()
	})
}

// shutdown closes the clients, the tunnel and the log file.
func (s *Server) shutdown() {
	s.cancel()
	if s.liveStatsClients != nil {
		s.liveStatsClients.closeAll()
	}
	if s.adminClients != nil {
		s.adminClients.closeAll()
	}
	if s.tunnel != nil {
		s.tunnel.Close()
	}
	if s.logOut != nil {
		_ = s.logOut.close()
	}
}

// withLock runs fn while holding mu. The deferred unlock keeps mu free when
// fn panics: the panic then fails only this request (a 500), as an exception
// did in Python, instead of leaving every later request waiting for mu.
func (s *Server) withLock(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

// resetStateLocked is reset_server_state: it forgets every student of the
// previous run. The caller holds mu (or the server is not shared yet).
func (s *Server) resetStateLocked() {
	s.users = map[string]*User{}
	s.userOrder = nil
	s.userResponses = nil
	s.userProgress = map[string]int{}
	s.questionStartTimes = map[string]time.Time{}
	s.currentQuestionIDs = map[string]int{}
	s.quizStartTimes = map[string]time.Time{}
	s.questionReadyTimes = map[string]time.Time{}
	s.userStats = map[string]*userStats{}
	s.userAnswers = map[string][]*answerRecord{}
	s.answerResults = map[string]map[int]*answerResult{}
	s.registrationTokens = map[string]string{}
	s.liveStats = map[string]map[int]liveState{}
	s.forceAllCompleted = false
}

// pyBool writes a boolean like Python's str(): True or False.
func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}
