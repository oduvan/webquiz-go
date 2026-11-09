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
//   - quizload.go: choosing the quiz at start, SwitchQuiz, start settings.
//   - websocket.go: the WebSocket hub (serveWebSocket, broadcastLiveStats,
//     broadcastAdmin).
//   - tunnel.go: the SSH tunnel manager and its status messages.
//   - admin_auth.go: the admin login (session cookie) and the session check.
//   - logging.go: the log file logs_dir/NNNN.log in Python's log format.
//   - Handlers that later tasks build answer 501 until then: student.go and
//     csvlog.go (Task 7), admin_quiz.go (Task 8), admin_config.go (Task 9),
//     files.go and transfer_handlers.go (Task 10), startup_log.go and
//     network.go (Task 11).
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
//   - Use s.now() for times, s.grace and s.checkInterval for time limits, and
//     s.ctx for work that must outlive a request (tunnel.Manager.Connect).
//   - Texts for students and API errors come from s.translations (or s.text).
package server
