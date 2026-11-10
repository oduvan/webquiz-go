package server

import "net/http"

// routes is the route table of create_app (server.py of WebQuiz 1.19), in
// the same order, plus /assets/ for the built web pages. Every route has its
// Python method and guard. A handler that a later task builds answers 501
// until then; its name is the Python method's name.
//
// The static routes at the end behave like aiohttp's add_static: GET on a
// path that no route has goes to static_dir (404 when there is no such
// file), and any other method on a path without that method gets 405.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	handle := func(pattern string, g guard, h handlerFunc) { mux.Handle(pattern, s.wrap(g, h)) }

	// Student API (student.go)
	handle("POST /api/register", public, s.registerUser)
	handle("PUT /api/update-registration", public, s.updateRegistration)
	handle("POST /api/submit-answer", public, s.submitAnswer)
	handle("POST /api/question-start", public, s.questionStart)
	handle("GET /api/verify-user/{user_id}", public, s.verifyUserID)

	// Admin pages and API
	handle("GET /admin/{$}", localOnly, s.serveAdminPage)                  // pages.go
	handle("POST /api/admin/auth", localOnly, s.adminAuth)                 // admin_auth.go
	handle("GET /api/admin/check-session", localOnly, s.adminCheckSession) // admin_auth.go
	handle("GET /api/admin/version-check", localOnly, s.adminVersionCheck) // admin_config.go
	handle("PUT /api/admin/approve-user", adminOnly, s.adminApproveUser)   // admin_quiz.go from here
	handle("POST /api/admin/force-show-answers", adminOnly, s.adminForceShowAnswers)
	handle("GET /api/admin/list-quizzes", adminOnly, s.adminListQuizzes)
	handle("POST /api/admin/switch-quiz", adminOnly, s.adminSwitchQuiz)
	handle("GET /api/admin/quiz/{filename}", adminOnly, s.adminGetQuiz)
	handle("POST /api/admin/preview-explanation", adminOnly, s.adminPreviewExplanation)
	handle("POST /api/admin/create-quiz", adminOnly, s.adminCreateQuiz)
	handle("PUT /api/admin/quiz/{filename}", adminOnly, s.adminUpdateQuiz)
	handle("DELETE /api/admin/quiz/{filename}", adminOnly, s.adminDeleteQuiz)
	handle("POST /api/admin/unite-quizzes", adminOnly, s.adminUniteQuizzes)
	handle("POST /api/admin/validate-quiz", adminOnly, s.adminValidateQuiz)
	handle("GET /api/admin/list-images", adminOnly, s.adminListImages)
	handle("GET /api/admin/list-files", adminOnly, s.adminListFiles)
	handle("GET /api/admin/list-checker-templates", adminOnly, s.adminListCheckerTemplates)
	handle("POST /api/admin/download-quiz", adminOnly, s.adminDownloadQuiz) // admin_config.go from here
	handle("PUT /api/admin/config", adminOnly, s.adminUpdateConfig)

	// Tunnel (admin_config.go)
	handle("POST /api/admin/tunnel/connect", adminOnly, s.tunnelConnect)
	handle("POST /api/admin/tunnel/disconnect", adminOnly, s.tunnelDisconnect)

	// File transfer between servers on the same tunnel server (transfer_handlers.go)
	handle("GET /api/admin/transfer", adminOnly, s.adminTransferState)
	handle("POST /api/admin/transfer/send", adminOnly, s.adminTransferSend)
	handle("POST /api/admin/transfer/{request_id}/accept", adminOnly, s.adminTransferAccept)
	handle("POST /api/admin/transfer/{request_id}/reject", adminOnly, s.adminTransferReject)
	// Called by other servers through the tunnel: protected by the offer token, not by the address.
	handle("POST /api/transfer/offer", public, s.transferOffer)
	handle("GET /api/transfer/download/{token}/{type}/{filename}", public, s.transferDownload)
	handle("POST /api/transfer/result", public, s.transferResult)

	handle("GET /ws/admin", localOnly, s.websocketAdmin) // websocket.go

	// Files
	handle("GET /files/{$}", localOnly, s.serveFilesPage)               // pages.go
	handle("GET /files/csv/{filename}", localOnly, s.serveCSVTablePage) // pages.go
	handle("GET /attach/{filename}", public, s.serveQuizFile)           // admin_config.go
	handle("GET /api/files/list", adminOnly, s.filesList)               // files.go from here
	handle("GET /api/files/{type}/view/{filename}", adminOnly, s.filesView)
	handle("GET /api/files/{type}/download/{filename}", adminOnly, s.filesDownload)
	handle("PUT /api/files/quizzes/save/{filename}", adminOnly, s.filesSaveQuiz)

	// Live stats
	handle("GET /live-stats/{$}", localOnly, s.serveLiveStatsPage) // pages.go
	handle("GET /ws/live-stats", localOnly, s.websocketLiveStats)  // websocket.go

	// The quiz page and the static folders (static.go)
	handle("GET /{$}", public, s.serveIndexPage)
	handle("GET /imgs", public, s.serveImages)
	handle("GET /imgs/", public, s.serveImages)
	handle("GET /assets/", public, s.serveAssets)
	handle("GET /", public, s.serveStatic)

	return s.accessLog(mux)
}
