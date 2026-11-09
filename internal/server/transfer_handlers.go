package server

import "net/http"

// File transfer between servers on the same tunnel server. Task 10 builds
// internal/transfer and these handlers (server.py admin_transfer_* and the
// TransferManager's handle_offer, handle_download, handle_result).

// adminTransferState is GET /api/admin/transfer.
func (s *Server) adminTransferState(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminTransferSend is POST /api/admin/transfer/send.
func (s *Server) adminTransferSend(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminTransferAccept is POST /api/admin/transfer/{request_id}/accept.
func (s *Server) adminTransferAccept(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// adminTransferReject is POST /api/admin/transfer/{request_id}/reject.
func (s *Server) adminTransferReject(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// transferOffer is POST /api/transfer/offer: another server offers files.
func (s *Server) transferOffer(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// transferDownload is GET /api/transfer/download/{token}/{type}/{filename}:
// another server downloads an offered file.
func (s *Server) transferDownload(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}

// transferResult is POST /api/transfer/result: another server reports what
// it did with the offer.
func (s *Server) transferResult(w http.ResponseWriter, r *http.Request) error {
	return notImplemented(w, r)
}
