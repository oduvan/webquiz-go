package server

import "net/http"

// Config, downloads, version check, tunnel buttons and file attachments.
// Task 9 builds these handlers (server.py admin_update_config,
// admin_download_quiz, admin_version_check, tunnel_connect,
// tunnel_disconnect, serve_quiz_file).
//
// For the tunnel buttons: pass s.ctx (or context.WithoutCancel of the
// request's context) to tunnel.Manager.Connect, never the request's context
// itself, because aiohttp did not stop a handler when the client left.
// After a config change, give the new "tunnel" section to applyTunnelConfig.

// adminVersionCheck is GET /api/admin/version-check (local network only).
func (s *Server) adminVersionCheck(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminDownloadQuiz is POST /api/admin/download-quiz.
func (s *Server) adminDownloadQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminUpdateConfig is PUT /api/admin/config.
func (s *Server) adminUpdateConfig(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// tunnelConnect is POST /api/admin/tunnel/connect.
func (s *Server) tunnelConnect(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// tunnelDisconnect is POST /api/admin/tunnel/disconnect.
func (s *Server) tunnelDisconnect(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// serveQuizFile is GET /attach/{filename} (public): a file of
// quizzes/attach, sent as a download.
func (s *Server) serveQuizFile(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}
