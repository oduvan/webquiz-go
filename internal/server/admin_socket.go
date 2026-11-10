package server

import (
	"net/http"

	"github.com/oduvan/webquiz-go/internal/transfer"
)

// The admin WebSocket (/ws/admin) tells the admin page about students who
// wait for approval (new_registration, registration_updated,
// user_approved), the tunnel status (tunnel_status) and file transfer
// requests. A page gets an initial_state message when it connects.

// registrationFieldsLocked is _extract_registration_fields: the student's
// values of the extra registration fields of the config, without the system
// fields. The caller holds mu.
func (s *Server) registrationFieldsLocked(user *User) map[string]any {
	fields := map[string]any{}
	for _, label := range s.cfg.Registration.Fields {
		name := registrationFieldName(label)
		if value, ok := user.Fields[name]; ok {
			fields[name] = value
		}
	}
	return fields
}

// adminInitialStateLocked is the initial_state message of websocket_admin,
// with the transfer requests that wait for the admin. The caller holds mu
// (and the transfer manager's lock, see joinAdminSocket).
func (s *Server) adminInitialStateLocked(transferRequests []transfer.RequestView) map[string]any {
	pending := map[string]any{}
	if s.cfg.Registration.Approve {
		for userID, user := range s.users {
			if !user.Approved {
				pending[userID] = map[string]any{
					"username":            user.Username,
					"registration_fields": s.registrationFieldsLocked(user),
				}
			}
		}
	}
	var tunnelInfo any
	if s.tunnel != nil {
		tunnelInfo = s.tunnelInfo(s.tunnel.Status())
	}
	return map[string]any{
		"type":              "initial_state",
		"pending_users":     pending,
		"requires_approval": s.cfg.Registration.Approve,
		"tunnel":            tunnelInfo,
		"transfer_requests": transferRequests,
	}
}

// websocketAdmin is /ws/admin (local network only).
func (s *Server) websocketAdmin(w http.ResponseWriter, r *http.Request) error {
	return s.serveWebSocketJoin(w, r, s.adminClients, s.joinAdminSocket)
}
