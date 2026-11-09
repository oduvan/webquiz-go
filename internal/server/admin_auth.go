package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
)

// sessionCookie is the cookie that holds an admin session token.
const sessionCookie = "admin_session"

// newSessionToken is secrets.token_urlsafe(32): 32 random bytes in URL-safe
// base64 without padding (43 characters).
func newSessionToken() string {
	data := make([]byte, 32)
	_, _ = rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}

// adminAuth is POST /api/admin/auth (local network only): it checks the
// master key from the JSON body ({"master_key": "..."}) and answers with a
// new session cookie. A trusted IP needs no key.
func (s *Server) adminAuth(w http.ResponseWriter, r *http.Request) error {
	if !s.isTrustedIP(getClientIP(r)) {
		masterKey := s.masterKey
		invalidText := s.text("server_invalid_master_key")
		if masterKey == "" {
			return writeError(w, http.StatusForbidden, noMasterKeyText)
		}
		provided := s.providedMasterKey(r)
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(masterKey)) != 1 {
			return writeError(w, http.StatusUnauthorized, invalidText)
		}
	}

	token := newSessionToken()
	s.mu.Lock()
	s.adminSessions[token] = s.now()
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "message": "Admin authentication successful"})
}

// providedMasterKey reads "master_key" from the request body. A body that is
// not JSON, not an object, or a key that is not text gives "" (Python
// compared the value with the key and found it different).
func (s *Server) providedMasterKey(r *http.Request) string {
	data, err := readJSON(r)
	if err != nil {
		return ""
	}
	body, err := pyObject(data)
	if err != nil {
		s.log.Error(fmt.Sprintf("Unexpected error reading admin auth request body: %v", err))
		return ""
	}
	key, _ := body["master_key"].(string)
	return key
}

// adminCheckSession is GET /api/admin/check-session (local network only):
// whether the admin_session cookie names a live session. The admin page uses
// it to log in again after a reload.
func (s *Server) adminCheckSession(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return writeJSON(w, http.StatusUnauthorized, map[string]any{"valid": false, "reason": "No session cookie"})
	}
	s.mu.Lock()
	_, valid := s.adminSessions[cookie.Value]
	s.mu.Unlock()
	if !valid {
		return writeJSON(w, http.StatusUnauthorized, map[string]any{"valid": false, "reason": "Invalid session"})
	}
	return writeJSON(w, http.StatusOK, map[string]any{"valid": true, "message": "Session is valid"})
}
