package server

import "net/http"

// The file manager API. Task 10 builds these handlers (server.py
// files_list, files_view, files_download, files_save_quiz). The page itself
// (/files/) is in pages.go.

// filesList is GET /api/files/list.
func (s *Server) filesList(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// filesView is GET /api/files/{type}/view/{filename}.
func (s *Server) filesView(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// filesDownload is GET /api/files/{type}/download/{filename}.
func (s *Server) filesDownload(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// filesSaveQuiz is PUT /api/files/quizzes/save/{filename}.
func (s *Server) filesSaveQuiz(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}
