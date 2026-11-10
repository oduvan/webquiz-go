package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/oduvan/webquiz-go/internal/transfer"
)

// File transfer between servers on the same tunnel server (server.py
// admin_transfer_* and the TransferManager's public handlers). The transfer
// itself is internal/transfer.
//
// Locking: the transfer manager calls filesInUse, which takes mu, while it
// holds its own lock. So the order is: the transfer manager's lock, then mu.
// Never call the transfer manager while holding mu.

// loggerTransfer is the logger name of transfer.py.
const loggerTransfer = "webquiz.transfer"

// newTransferManager makes the manager of the TestingServer: its folders,
// its public address from the tunnel, the admin WebSocket for its messages
// and the files the running server uses.
func (s *Server) newTransferManager() *transfer.Manager {
	directories := map[string]string{
		"quizzes":     s.quizzesDir,
		"logs":        s.logsDir,
		"csv":         s.csvDir,
		"images":      filepath.Join(s.quizzesDir, "imgs"),
		"attachments": filepath.Join(s.quizzesDir, "attach"),
	}
	return transfer.New(transfer.Deps{
		Directories: func() map[string]string { return directories },
		Endpoint:    s.publicEndpoint,
		Notify:      func(message map[string]any) { s.broadcastAdmin(message) },
		FilesInUse:  s.filesInUse,
		Logger:      s.log.With(loggerKey, loggerTransfer),
	})
}

// publicEndpoint is the tunnel's public address while it is connected:
// the base URL of the tunnel server and this server's socket name.
func (s *Server) publicEndpoint() (string, string, bool) {
	if endpoint := s.endpointForTests.Load(); endpoint != nil {
		return (*endpoint)()
	}
	if s.tunnel == nil {
		return "", "", false
	}
	return s.tunnel.PublicEndpoint()
}

// filesInUse are the files the running server writes or uses: the log, the
// CSV files of this run and the active quiz. A transfer cannot replace them.
func (s *Server) filesInUse() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []string{s.logFile, s.csvFile, s.userCSVFile, s.answersWithUsersCSVFile, s.currentQuizFile}
}

// writeTransferError answers with a transfer error's status and message.
// Any other error is returned for the 500 answer.
func writeTransferError(w http.ResponseWriter, err error) error {
	var transferError *transfer.Error
	if errors.As(err, &transferError) {
		return writeError(w, transferError.Status, transferError.Message)
	}
	return err
}

// adminTransferState is GET /api/admin/transfer: this server's name, the
// sent offers and the incoming requests.
func (s *Server) adminTransferState(w http.ResponseWriter, r *http.Request) error {
	state, err := s.transfer.State()
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, state)
}

// adminTransferSend is POST /api/admin/transfer/send: it offers files to
// another server on the same tunnel server. Body: {"to": "socket-name",
// "files": [{"type": "quizzes|logs|csv", "name": "..."}]}.
func (s *Server) adminTransferSend(w http.ResponseWriter, r *http.Request) error {
	data, err := readJSON(r)
	if err != nil {
		return writeError(w, http.StatusBadRequest, "Invalid JSON")
	}
	fields, ok := data.(map[string]any)
	if !ok {
		return writeError(w, http.StatusBadRequest, "Invalid request")
	}
	// The server's context, not the request's: aiohttp went on when the
	// admin's page went away.
	offer, err := s.transfer.Send(s.ctx, fields["to"], fields["files"])
	if err != nil {
		return writeTransferError(w, err)
	}
	return writeJSON(w, http.StatusOK, struct {
		Success bool               `json:"success"`
		Offer   transfer.OfferView `json:"offer"`
	}{true, offer})
}

// adminTransferAccept is POST /api/admin/transfer/{request_id}/accept: it
// downloads the files of an incoming request. Optional body: {"files":
// [{"type", "name", "action": "save|replace|rename|skip", "new_name"}]};
// files without a choice get their default action. On invalid choices
// nothing is downloaded and the request stays open (400).
func (s *Server) adminTransferAccept(w http.ResponseWriter, r *http.Request) error {
	var choices any
	if r.ContentLength != 0 { // aiohttp's can_read_body
		data, err := readJSON(r)
		if err != nil {
			return writeError(w, http.StatusBadRequest, "Invalid JSON")
		}
		if fields, ok := data.(map[string]any); ok {
			choices = fields["files"]
		}
	}
	result, err := s.transfer.Accept(s.ctx, r.PathValue("request_id"), choices)
	if err != nil {
		return writeTransferError(w, err)
	}
	return writeJSON(w, http.StatusOK, struct {
		Success bool `json:"success"`
		transfer.AcceptResult
	}{true, result})
}

// adminTransferReject is POST /api/admin/transfer/{request_id}/reject.
func (s *Server) adminTransferReject(w http.ResponseWriter, r *http.Request) error {
	if err := s.transfer.Reject(s.ctx, r.PathValue("request_id")); err != nil {
		return writeTransferError(w, err)
	}
	return writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// transferOffer is POST /api/transfer/offer: another server offers files.
// Other servers call the public routes through the tunnel: the offer token
// protects them, not the address.
func (s *Server) transferOffer(w http.ResponseWriter, r *http.Request) error {
	return s.transfer.HandleOffer(w, r)
}

// transferDownload is GET /api/transfer/download/{token}/{type}/{filename}:
// another server downloads an offered file.
func (s *Server) transferDownload(w http.ResponseWriter, r *http.Request) error {
	return s.transfer.HandleDownload(w, r)
}

// transferResult is POST /api/transfer/result: another server reports what
// it did with the offer.
func (s *Server) transferResult(w http.ResponseWriter, r *http.Request) error {
	return s.transfer.HandleResult(w, r)
}

// joinAdminSocket adds a new admin page to the hub with its initial_state
// message. It holds the transfer manager's lock and the tunnel's status lock
// around the whole step, so the page gets a transfer request or a tunnel
// status change that comes at the same moment either in its initial state
// or as a message after it, never neither. Lock order: the transfer
// manager's lock, the tunnel's status lock, then mu.
func (s *Server) joinAdminSocket(c *wsClient) bool {
	joined := false
	s.transfer.WithIncoming(func(requests []transfer.RequestView, err error) {
		if err != nil {
			s.log.Error(fmt.Sprintf("Error sending initial state to admin WebSocket client: %v", err))
			return
		}
		s.withTunnelInfo(func(tunnelInfo any) {
			joined = s.joinHub(s.adminClients, c, func() any { return s.adminInitialStateLocked(requests, tunnelInfo) })
		})
	})
	return joined
}
