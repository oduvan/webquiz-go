// Package server is the WebQuiz HTTP server, a port of webquiz/server.py
// (WebQuiz 1.19, class TestingServer and create_app).
//
// Making and running a server:
//
//	srv, err := server.New(cfg, server.Options{}) // log file, tunnel keys, quiz page
//	srv.Start(ctx)                                // CSV flush and time limit watcher
//	http.Serve(listener, srv.Handler())
//	srv.Close()                                   // after the HTTP server has stopped
//
// Tests use internal/testutil, which does this with temp folders.
//
// Where things are:
//
//   - server.go: Server with every state field of TestingServer, New, Start,
//     Close, the checker hooks of internal/quiz.
//   - routes.go: the route table (every route of create_app, plus /assets/).
//   - middleware.go: the guards (localNetworkOnly, adminAuthRequired),
//     getClientIP, the error handling (handlerFunc) and the access log.
//   - json.go: request JSON (readJSON keeps numbers as json.Number) and
//     answers (writeJSON, writeError, notImplemented).
//   - pages.go: the boot data of each page (QuizBoot, AdminBoot, FilesBoot,
//     LiveStatsBoot, CSVTableBoot), renderPage, the quiz page file
//     static_dir/index.html, the admin pages.
//   - static.go: "/", /imgs/, /assets/ and the static folder.
//   - quizload.go: choosing the quiz at start, SwitchQuiz, start settings
//     (checked and applied), the log warnings for checkers that do not compile.
//   - student.go: the student API (register, update-registration,
//     verify-user, question-start, submit-answer) with _record_answer and
//     _end_whole_quiz.
//   - timing.go: the question and whole-quiz clocks, remaining times, and
//     the Python-exact values: seconds (int 0 or float), points, isoformat.
//   - stats.go: the stored answers, the final results of a student and
//     all_students_completed.
//   - csvlog.go: the CSV files of a run (shared NNNN number, flush, fsync,
//     rows written like Python's csv module).
//   - livestats.go: the live stats WebSocket and its messages.
//   - admin_socket.go: the admin WebSocket's initial state and the
//     registration fields of its messages.
//   - watcher.go: the time limit watcher (check_time_limits).
//   - admin_quiz.go: approve-user, switch-quiz and the rest of the admin
//     quiz API (list, get, create, update, delete, unite, validate, the
//     editor's lists and the explanation preview). yamldump.go: quiz files
//     written like PyYAML's yaml.dump.
//   - websocket.go: the WebSocket hub (serveWebSocket, broadcastLiveStats,
//     broadcastAdmin).
//   - tunnel.go: the SSH tunnel manager and its status messages.
//   - admin_config.go: config save with hot reload and rollback (a form save
//     is written only when config.CheckMerge accepts the merged text), the
//     version check and the tunnel buttons. download.go: quiz downloads from
//     ZIP archives (with the token rules) and /attach/ files.
//   - admin_auth.go: the admin login (session cookie) and the session check.
//   - logging.go: the log file logs_dir/NNNN.log in Python's log format.
//     startup_log.go: the environment lines the log starts with.
//   - files.go: the file manager API (list, view, download, save a quiz).
//   - transfer_handlers.go: file transfer between servers (internal/transfer).
//
// Rules for handlers:
//
//   - A handler is a handlerFunc: it returns an error instead of answering
//     500 itself. The error text becomes {"error": text}, like Python's
//     error_middleware. keyError and pyObject give Python's texts for a
//     missing key and for data.get on a value that is not an object.
//   - Read request bodies with readJSON (UseNumber: 1 and 1.0 stay
//     different, as in Python) and compare request values with pyval.Equal.
//   - mu guards all state. Do the state work under mu, and let it go before
//     slow work (HTTP calls, big file writes). Broadcast while holding mu,
//     right after the change: broadcasting only queues the message.
//   - Release mu with defer: s.withLock(func() { ... }), or a small function
//     that starts with s.mu.Lock() and defer s.mu.Unlock(). A panic is
//     recovered (500 for that request), and without the defer it would leave
//     mu locked, so every later request would wait forever.
//   - The transfer manager (s.transfer) takes its own lock and then mu (it
//     asks for the files in use). Never call it while holding mu.
//   - A new admin page (joinAdminSocket) takes the transfer manager's lock,
//     then the tunnel's status lock (tunnel.Manager.WithStatus), then mu, so
//     it misses no transfer request and no tunnel status change. The tunnel
//     takes its status lock to report a change, so never call the tunnel's
//     Connect, Disconnect or Close while holding mu.
//   - s.text, s.isTrustedIP and s.validSession take mu themselves. A Go
//     mutex cannot be locked twice by the same caller, so inside a locked
//     section read s.translations[key] and s.cfg directly instead.
//   - Lock order: configMu, csvMu, then mu. configMu lets one config save
//     run at a time. csvMu lets one CSV flush or one quiz switch write CSV
//     files at a time. SwitchQuiz takes csvMu and mu itself, so call it
//     without holding mu (a config save that restarts the quiz, a file save
//     that reloads it).
//   - Checker code may run for 2 seconds, so it never runs under mu:
//     submit-answer does its checks in a locked step, runs the checker
//     without the lock, then does the whole order of checks again in a
//     second locked step before it stores the answer. Do the same for any
//     other slow work that a stored state depends on.
//   - Use s.now() for times, s.grace and s.checkInterval for time limits, and
//     s.ctx for work that must outlive a request (tunnel.Manager.Connect).
//   - Texts for students and API errors come from s.translations (or s.text).
package server
