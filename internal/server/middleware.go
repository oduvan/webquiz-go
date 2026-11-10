package server

import (
	"errors"
	"fmt"
	"github.com/oduvan/webquiz-go/internal/netinfo"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// handlerFunc is an HTTP handler that may fail. An error it returns, or a
// panic, becomes a 500 answer with {"error": <text>}, like Python's
// error_middleware; a *statusError keeps its own status.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// guard says who may call a route.
type guard int

const (
	public    guard = iota // anybody: students, and other servers through the tunnel
	localOnly              // local_network_only: private addresses only
	adminOnly              // admin_auth_required: private addresses with an admin session (or a trusted IP)
)

// wrap gives a handler its guard and the error handling.
func (s *Server) wrap(g guard, h handlerFunc) http.Handler {
	switch g {
	case localOnly:
		h = s.localNetworkOnly(h)
	case adminOnly:
		h = s.adminAuthRequired(h)
	}
	return s.serveErrors(h)
}

// serveErrors is error_middleware: it logs a failed request with the error
// (and the stack for a panic) and answers 500 {"error": text}.
func (s *Server) serveErrors(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			value := recover()
			if value == nil {
				return
			}
			if value == http.ErrAbortHandler {
				panic(value) // net/http's way to drop the connection
			}
			text := fmt.Sprint(value)
			s.log.Error(fmt.Sprintf("Error handling %s %s from %s: panic: %s\n%s", r.Method, r.URL.Path, remoteIP(r), text, debug.Stack()))
			_ = writeError(w, http.StatusInternalServerError, text)
		}()
		err := h(w, r)
		if err == nil {
			return
		}
		var status *statusError
		if errors.As(err, &status) {
			writeText(w, status.status, status.text)
			return
		}
		s.log.Error(fmt.Sprintf("Error handling %s %s from %s: %v", r.Method, r.URL.Path, remoteIP(r), err))
		_ = writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// localNetworkOnly is local_network_only: only clients with a private
// address get through (Python's ipaddress is_private: 127.0.0.0/8,
// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, ::1, fc00::/7, fe80::/10 and
// the other not globally reachable ranges).
func (s *Server) localNetworkOnly(h handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if ok, err := s.checkLocalNetwork(w, r); !ok {
			return err
		}
		return h(w, r)
	}
}

// checkLocalNetwork answers 403 and returns false when the client is not on
// a private network.
func (s *Server) checkLocalNetwork(w http.ResponseWriter, r *http.Request) (bool, error) {
	addr, err := netip.ParseAddr(getClientIP(r))
	if err != nil {
		return false, writeError(w, http.StatusForbidden, s.text("server_access_denied_ip"))
	}
	if !isPrivateAddress(addr) {
		return false, writeError(w, http.StatusForbidden, s.text("server_access_denied_local"))
	}
	return true, nil
}

// adminAuthRequired is admin_auth_required: a private address, then either a
// trusted IP or a master key with a valid admin_session cookie.
func (s *Server) adminAuthRequired(h handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if ok, err := s.checkLocalNetwork(w, r); !ok {
			return err
		}
		if s.isTrustedIP(getClientIP(r)) {
			return h(w, r)
		}
		if s.masterKey == "" {
			return writeError(w, http.StatusForbidden, noMasterKeyText)
		}
		if !s.validSession(r) {
			return writeError(w, http.StatusUnauthorized, s.text("server_invalid_session"))
		}
		return h(w, r)
	}
}

// noMasterKeyText is the answer of the admin routes when no master key is
// set. Python had no translation for it.
const noMasterKeyText = "Admin functionality disabled - no master key set"

// isTrustedIP tells whether the address is in admin.trusted_ips. Like
// Python's "client_ip in trusted_ips" it compares the text exactly. It takes
// mu: inside a locked section read s.cfg.Admin.TrustedIPs instead.
func (s *Server) isTrustedIP(clientIP string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.cfg.Admin.TrustedIPs, clientIP)
}

// text returns a text of the interface language (translations.py). It
// takes mu: inside a locked section use s.translations[key] instead.
func (s *Server) text(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.translations[key]
}

// remoteIP is the address of the TCP peer (aiohttp's request.remote), or
// 127.0.0.1 when there is none.
func remoteIP(r *http.Request) string {
	if r.RemoteAddr == "" {
		return "127.0.0.1"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // no port, as in some tests
	}
	return host
}

// getClientIP is get_client_ip: the client's address. The proxy headers
// count only when the connection comes from loopback: a reverse proxy on the
// same machine, or the SSH tunnel, which forwards public traffic to
// 127.0.0.1. A client that connects directly cannot fake its address. From
// X-Forwarded-For the last entry is used: a proxy adds the address it saw at
// the end, earlier entries may come from the client.
func getClientIP(r *http.Request) string {
	clientIP := remoteIP(r)
	if !isLoopbackAddress(clientIP) {
		return clientIP
	}
	var forwarded []string
	for _, part := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		if part = pyval.Strip(part); part != "" {
			forwarded = append(forwarded, part)
		}
	}
	if len(forwarded) > 0 {
		return forwarded[len(forwarded)-1]
	}
	if realIP := pyval.Strip(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	return clientIP
}

// isLoopbackAddress is is_loopback_address: 127.0.0.0/8, ::1 and their
// IPv4-mapped forms. Text that is not an address counts as loopback (Python
// did this to be safe).
func isLoopbackAddress(ip string) bool { return netinfo.IsLoopbackAddress(ip) }

// The ranges of Python 3.13's ipaddress is_private: addresses that are not
// globally reachable (the IANA special registries), minus the exceptions.
var (
	privateIPv4 = prefixes(
		"0.0.0.0/8", "10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.0.170/31", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32",
	)
	privateIPv4Exceptions = prefixes("192.0.0.9/32", "192.0.0.10/32")
	privateIPv6           = prefixes(
		"::1/128", "::/128", "::ffff:0:0/96", "64:ff9b:1::/48", "100::/64", "2001::/23",
		"2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10",
	)
	privateIPv6Exceptions = prefixes(
		"2001:1::1/128", "2001:1::2/128", "2001:3::/32", "2001:4:112::/48", "2001:20::/28", "2001:30::/28",
	)
)

func prefixes(texts ...string) []netip.Prefix {
	list := make([]netip.Prefix, len(texts))
	for i, text := range texts {
		list[i] = netip.MustParsePrefix(text)
	}
	return list
}

// isPrivateAddress is ipaddress.ip_address(ip).is_private (Python 3.13). An
// IPv4-mapped IPv6 address follows the rules of its IPv4 address.
func isPrivateAddress(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	inAny := func(list []netip.Prefix) bool {
		return slices.ContainsFunc(list, func(p netip.Prefix) bool { return p.Contains(addr) })
	}
	if addr.Is4() {
		return inAny(privateIPv4) && !inAny(privateIPv4Exceptions)
	}
	return inAny(privateIPv6) && !inAny(privateIPv6Exceptions)
}

// statusRecorder notes the status and size of a response for the access
// log. Unwrap lets WebSockets reach the connection underneath.
type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.size += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// accessLog writes a line for every request, like aiohttp's access log
// (format '%a %t "%r" %s %b "%{Referer}i" "%{User-Agent}i"').
func (s *Server) accessLog(next http.Handler) http.Handler {
	log := s.log.With(loggerKey, loggerAccess)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Info(fmt.Sprintf(`%s [%s] "%s %s %s" %d %d "%s" "%s"`,
			remoteIP(r), time.Now().Format("02/Jan/2006:15:04:05 -0700"),
			r.Method, r.URL.RequestURI(), r.Proto, status, recorder.size,
			headerOrDash(r, "Referer"), headerOrDash(r, "User-Agent")))
	})
}

func headerOrDash(r *http.Request, name string) string {
	if value := r.Header.Get(name); value != "" {
		return value
	}
	return "-"
}
