package transfer

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// The limits of transfer.py.
const (
	offerLifetime         = 30 * time.Minute // a waiting offer expires after this
	finishedOfferLifetime = 24 * time.Hour   // a finished offer is forgotten after this
	maxFilesPerOffer      = 100
	maxFileSize           = 50 * 1024 * 1024 // 50 MB
	maxIncomingRequests   = 20
	httpTimeout           = 30 * time.Second // httpx's timeout: for connecting and for each wait for data
)

// The states of an outgoing offer and of an incoming request.
const (
	statusWaiting     = "waiting"     // offer: the receiver has not answered yet
	statusExpired     = "expired"     // offer: nobody answered within offerLifetime
	statusPending     = "pending"     // request: waits for the admin
	statusDownloading = "downloading" // request: the admin accepted it
)

// Error is a transfer failure with a message for the admin and an HTTP
// status (TransferError).
type Error struct {
	Message string
	Status  int
}

func (e *Error) Error() string { return e.Message }

// newError is TransferError(message) with the default status 400.
func newError(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Status: http.StatusBadRequest}
}

// Deps is what the manager needs from its server.
type Deps struct {
	// Directories returns the folder of every file type: "quizzes", "logs",
	// "csv", "images" and "attachments".
	Directories func() map[string]string
	// Endpoint returns the tunnel server's base URL and this server's own
	// socket name while the tunnel is connected; ok is false otherwise.
	Endpoint func() (baseURL, name string, ok bool)
	// Notify gets the messages for the admin WebSocket. It is called while
	// the manager holds its lock, right after the change it reports, so it
	// must not block and must not call the manager.
	Notify func(message map[string]any)
	// FilesInUse returns the paths the running server writes or uses (its
	// log, its CSV files, the active quiz); they cannot be replaced. An
	// empty path is ignored. Nil means none.
	FilesInUse func() []string
	// Logger gets the log lines; nil means slog.Default().
	Logger *slog.Logger
}

// Manager keeps the outgoing offers and incoming requests of one server
// (TransferManager). Its methods are safe to call from many goroutines.
//
// Locking: mu guards the offers and requests. The manager calls
// Deps.FilesInUse and Deps.Notify while holding mu, so a server must never
// call the manager while holding a lock that FilesInUse takes. HTTP calls
// to other servers happen without mu.
type Manager struct {
	deps   Deps
	log    *slog.Logger
	client *http.Client

	// Values of transfer.py that tests change.
	maxFilesPerOffer int
	maxFileSize      int64

	mu       sync.Mutex
	outgoing []*offer   // in the order they were made (Python's dict order)
	incoming []*request // in the order they came
}

// offer is an outgoing offer.
type offer struct {
	id      string
	token   string
	to      string
	files   []FileEntry
	created time.Time
	status  string // waiting, accepted, rejected, failed or expired
}

// request is an incoming request.
type request struct {
	id       string
	from     string
	token    string
	files    []incomingFile
	received time.Time
	status   string // pending or downloading
}

// incomingFile is a file of an incoming request as the sender described it.
type incomingFile struct {
	fileType string
	name     string
	size     json.Number // a whole number >= 0, kept exactly (Python's int)
	sha256   string      // "" when the sender sent no valid checksum (None)
}

// FileEntry is a file of an outgoing offer: what the sender sends in the
// offer and shows on its admin page.
type FileEntry struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// OfferView is an outgoing offer without its token.
type OfferView struct {
	ID      string      `json:"id"`
	To      string      `json:"to"`
	Files   []FileEntry `json:"files"`
	Created string      `json:"created"`
	Status  string      `json:"status"`
}

// FilePlan is a file of an incoming request with its case, the possible
// actions, the default action and the name suggested for "rename".
type FilePlan struct {
	Type          string      `json:"type"`
	Name          string      `json:"name"`
	Size          json.Number `json:"size"`
	Target        string      `json:"target"`
	Case          string      `json:"case"`
	Actions       []string    `json:"actions"`
	DefaultAction string      `json:"default_action"`
	SuggestedName string      `json:"suggested_name"`
}

// RequestView is an incoming request without its token, with the plan of
// every file.
type RequestView struct {
	ID       string     `json:"id"`
	From     string     `json:"from"`
	Files    []FilePlan `json:"files"`
	Received string     `json:"received"`
	Status   string     `json:"status"`
}

// State is the state for the admin pages (get_state).
type State struct {
	Available bool          `json:"available"`
	Name      *string       `json:"name"` // this server's socket name; null without a tunnel
	Outgoing  []OfferView   `json:"outgoing"`
	Incoming  []RequestView `json:"incoming"`
}

// AcceptResult tells what happened to every file of an accepted request.
type AcceptResult struct {
	From    string       `json:"from"`
	Saved   []SavedFile  `json:"saved"`
	Skipped []FileAction `json:"skipped"`
	Failed  []FailedFile `json:"failed"`
}

// FileAction is a file with the action that was chosen for it.
type FileAction struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Action string `json:"action"`
}

// SavedFile is a file that was saved, with the name it got.
type SavedFile struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Action  string `json:"action"`
	SavedAs string `json:"saved_as"`
}

// FailedFile is a file that could not be saved, with the reason.
type FailedFile struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Error  string `json:"error"`
}

// New makes a manager.
func New(deps Deps) *Manager {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		deps:             deps,
		log:              log,
		client:           newHTTPClient(),
		maxFilesPerOffer: maxFilesPerOffer,
		maxFileSize:      maxFileSize,
	}
}

// notifyLocked sends a message to the admin pages. A failure is logged, as
// in Python. The caller holds mu.
func (m *Manager) notifyLocked(message map[string]any) {
	if m.deps.Notify == nil {
		return
	}
	defer func() {
		if err := recover(); err != nil {
			m.log.Error(fmt.Sprintf("Error sending transfer notification: %v", err))
		}
	}()
	m.deps.Notify(message)
}

func (m *Manager) directories() map[string]string { return m.deps.Directories() }

// requireEndpoint returns the tunnel server's base URL and this server's
// name, or a 409 error when the tunnel is not connected.
func (m *Manager) requireEndpoint() (string, string, error) {
	baseURL, name, ok := m.endpoint()
	if !ok {
		return "", "", &Error{Message: "SSH tunnel is not connected", Status: http.StatusConflict}
	}
	return baseURL, name, nil
}

func (m *Manager) endpoint() (string, string, bool) {
	if m.deps.Endpoint == nil {
		return "", "", false
	}
	return m.deps.Endpoint()
}

// removeExpiredLocked expires waiting offers and forgets old requests. The
// caller holds mu.
func (m *Manager) removeExpiredLocked() {
	now := time.Now()
	m.outgoing = slices.DeleteFunc(m.outgoing, func(o *offer) bool {
		age := now.Sub(o.created)
		if o.status == statusWaiting && age > offerLifetime {
			o.status = statusExpired
		}
		return o.status != statusWaiting && age > finishedOfferLifetime
	})
	m.incoming = slices.DeleteFunc(m.incoming, func(r *request) bool {
		return r.status == statusPending && now.Sub(r.received) > offerLifetime
	})
}

// view is the offer without its token.
func (o *offer) view() OfferView {
	return OfferView{ID: o.id, To: o.to, Files: o.files, Created: isoformat(o.created), Status: o.status}
}

// requestViewLocked is the request without its token, with the case and
// actions of every file. The caller holds mu.
func (m *Manager) requestViewLocked(r *request) (RequestView, error) {
	plans, err := m.planFiles(r)
	if err != nil {
		return RequestView{}, err
	}
	return RequestView{ID: r.id, From: r.from, Files: plans, Received: isoformat(r.received), Status: r.status}, nil
}

// ListIncoming returns the incoming requests that wait for the admin
// (list_incoming).
func (m *Manager) ListIncoming() ([]RequestView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listIncomingLocked()
}

// WithIncoming calls fn with the incoming requests that wait for the admin
// while holding the manager's lock: no transfer_request or
// transfer_request_removed message can be sent while fn runs. The admin
// WebSocket uses it to send these requests in its initial message without
// missing one that arrives at the same moment. fn must not call the manager.
func (m *Manager) WithIncoming(fn func(requests []RequestView, err error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.listIncomingLocked())
}

func (m *Manager) listIncomingLocked() ([]RequestView, error) {
	m.removeExpiredLocked()
	views := []RequestView{}
	for _, r := range m.incoming {
		if r.status != statusPending {
			continue
		}
		view, err := m.requestViewLocked(r)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// State returns the state for the admin pages: this server's name, the
// sent offers (newest first) and the incoming requests (get_state).
func (m *Manager) State() (State, error) {
	_, name, ok := m.endpoint()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeExpiredLocked()
	sorted := slices.Clone(m.outgoing)
	slices.SortStableFunc(sorted, func(a, b *offer) int { return b.created.Compare(a.created) })
	state := State{Available: ok, Outgoing: []OfferView{}}
	if ok {
		state.Name = &name
	}
	for _, o := range sorted {
		state.Outgoing = append(state.Outgoing, o.view())
	}
	incoming, err := m.listIncomingLocked()
	if err != nil {
		return State{}, err
	}
	state.Incoming = incoming
	return state, nil
}

// parseFileList checks the list of files of an offer: [{"type", "name"}].
func (m *Manager) parseFileList(files any) ([]mediaFile, error) {
	list, ok := files.([]any)
	if !ok || len(list) == 0 {
		return nil, newError("No files selected")
	}
	if len(list) > m.maxFilesPerOffer {
		return nil, newError("Too many files (maximum %d)", m.maxFilesPerOffer)
	}
	result := make([]mediaFile, 0, len(list))
	for _, item := range list {
		var fileType, name any
		if entry, ok := item.(map[string]any); ok {
			fileType, name = entry["type"], entry["name"]
		}
		if err := unhashable(fileType); err != nil {
			return nil, err // Python failed looking the type up in FILE_TYPES
		}
		if !isAllowedFile(fileType, name) {
			return nil, newError("This file cannot be transferred: %s", pyval.Str(name))
		}
		result = append(result, mediaFile{fileType.(string), name.(string)})
	}
	return result, nil
}

// unhashable is Python's TypeError for a list or dict used as a dict key.
func unhashable(v any) error {
	switch kind := pyval.KindOf(v); kind {
	case pyval.KindList, pyval.KindDict:
		return fmt.Errorf("unhashable type: '%s'", kind)
	}
	return nil
}

// ----- Sender side -----

// Send offers files to another server (send_offer). target is the
// receiver's socket name, files the list of {"type", "name"} that the admin
// selected. Images and attachments used by offered quizzes are added
// automatically. It returns the outgoing offer.
func (m *Manager) Send(ctx context.Context, target, files any) (OfferView, error) {
	baseURL, ownName, err := m.requireEndpoint()
	if err != nil {
		return OfferView{}, err
	}
	if !isValidServerName(target) {
		return OfferView{}, newError("Invalid server name. Use letters, digits, '-', '_' and '.'")
	}
	targetName := target.(string)
	if targetName == ownName {
		return OfferView{}, newError("This is the name of this server")
	}

	directories := m.directories()
	selected, err := m.parseFileList(files)
	if err != nil {
		return OfferView{}, err
	}
	type entry struct {
		mediaFile
		path string
	}
	var entries []entry
	for _, item := range selected {
		path := filepath.Join(directories[item.Type], item.Name)
		if !isFile(path) {
			return OfferView{}, &Error{Message: "File not found: " + item.Name, Status: http.StatusNotFound}
		}
		entries = append(entries, entry{item, path})
	}
	// Files used by the quizzes; a missing file is skipped (the quiz shows
	// it as broken anyway).
	for _, item := range selected {
		if item.Type != "quizzes" {
			continue
		}
		for _, media := range quizMediaFiles(filepath.Join(directories["quizzes"], item.Name)) {
			path := filepath.Join(directories[media.Type], media.Name)
			if isFile(path) {
				entries = append(entries, entry{media, path})
			}
		}
	}
	var unique []entry
	for _, e := range entries {
		if !slices.ContainsFunc(unique, func(u entry) bool { return u.mediaFile == e.mediaFile }) {
			unique = append(unique, e)
		}
	}
	if len(unique) > m.maxFilesPerOffer {
		return OfferView{}, newError("Too many files with images and attachments (maximum %d)", m.maxFilesPerOffer)
	}
	// The checksum lets the receiver see if it already has the same file.
	offered := make([]FileEntry, 0, len(unique))
	for _, e := range unique {
		info, err := os.Stat(e.path)
		if err != nil {
			return OfferView{}, err
		}
		sum, err := fileSHA256(e.path)
		if err != nil {
			return OfferView{}, err
		}
		offered = append(offered, FileEntry{Type: e.Type, Name: e.Name, Size: info.Size(), SHA256: sum})
	}

	o := &offer{token: randomToken(), id: randomID(), to: targetName, files: offered, created: time.Now(), status: statusWaiting}
	// Registered before sending: the receiver may download right after accepting.
	m.mu.Lock()
	m.removeExpiredLocked()
	m.outgoing = append(m.outgoing, o)
	m.mu.Unlock()

	body := offerBody{From: ownName, Token: o.token, Files: offered}
	status, responseBody, err := m.postJSON(ctx, baseURL+"/"+targetName+"/api/transfer/offer", body)
	if err != nil {
		m.forget(o)
		return OfferView{}, &Error{Message: "Cannot reach the tunnel server: " + err.Error(), Status: http.StatusBadGateway}
	}
	if status >= 400 {
		m.forget(o)
		switch status {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return OfferView{}, &Error{Message: fmt.Sprintf("Server '%s' is not online", targetName), Status: http.StatusNotFound}
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			return OfferView{}, &Error{
				Message: fmt.Sprintf("Server '%s' does not support file transfer. Update WebQuiz there", targetName),
				Status:  http.StatusNotFound,
			}
		}
		return OfferView{}, &Error{
			Message: fmt.Sprintf("Server '%s' refused the request: %s", targetName, responseError(status, responseBody)),
			Status:  http.StatusBadGateway,
		}
	}

	m.log.Info(fmt.Sprintf("Offered %d file(s) to '%s'", len(offered), targetName))
	m.mu.Lock()
	defer m.mu.Unlock()
	return o.view(), nil
}

// offerBody is the JSON of POST /api/transfer/offer, with the keys in
// Python's order.
type offerBody struct {
	From  string      `json:"from"`
	Token string      `json:"token"`
	Files []FileEntry `json:"files"`
}

// resultBody is the JSON of POST /api/transfer/result.
type resultBody struct {
	Token  string `json:"token"`
	Status string `json:"status"`
}

// forget removes an offer that could not be sent.
func (m *Manager) forget(o *offer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outgoing = slices.DeleteFunc(m.outgoing, func(other *offer) bool { return other == o })
}

// responseError is the receiver's reason for refusing: its "error" text,
// else its status (_response_error).
func responseError(status int, body []byte) string {
	value, err := decodeJSON(body)
	if err != nil {
		return fmt.Sprintf("HTTP %d", status)
	}
	data, ok := value.(map[string]any)
	if !ok {
		return fmt.Sprintf("HTTP %d", status) // Python's .get failed on a list
	}
	if text := data["error"]; pyval.Truthy(text) {
		return pyval.Str(text)
	}
	return strconv.Itoa(status)
}

// findWaitingOfferLocked returns the waiting offer with this token. The
// caller holds mu.
func (m *Manager) findWaitingOfferLocked(token any) *offer {
	text, ok := token.(string)
	if !ok {
		return nil
	}
	m.removeExpiredLocked()
	for _, o := range m.outgoing {
		if o.token == text {
			if o.status != statusWaiting {
				return nil
			}
			return o
		}
	}
	return nil
}

// ----- Receiver side -----

// addIncoming checks and stores an offer from another server
// (add_incoming). The caller holds mu.
func (m *Manager) addIncomingLocked(data any) (*request, error) {
	_, ownName, err := m.requireEndpoint()
	if err != nil {
		return nil, err
	}
	fields, ok := data.(map[string]any)
	if !ok {
		return nil, newError("Invalid request")
	}
	sender, token := fields["from"], fields["token"]
	if !isValidServerName(sender) || sender == ownName {
		return nil, newError("Invalid sender name")
	}
	if text, ok := token.(string); !ok || !tokenPattern.MatchString(text) {
		return nil, newError("Invalid token")
	}
	selected, err := m.parseFileList(fields["files"])
	if err != nil {
		return nil, err
	}
	items := fields["files"].([]any)
	entries := make([]incomingFile, len(selected))
	for i, f := range selected {
		item := items[i].(map[string]any)
		entries[i] = incomingFile{fileType: f.Type, name: f.Name, size: "0"}
		if size, ok := wholeNumber(item["size"]); ok && size.Sign() >= 0 {
			entries[i].size = json.Number(size.String())
		}
		if checksum, ok := item["sha256"].(string); ok && sha256Pattern.MatchString(checksum) {
			entries[i].sha256 = checksum
		}
	}

	m.removeExpiredLocked()
	if len(m.incoming) >= maxIncomingRequests {
		return nil, &Error{Message: "Too many waiting requests on this server", Status: http.StatusTooManyRequests}
	}
	r := &request{
		id:       randomID(),
		from:     sender.(string),
		token:    token.(string),
		files:    entries,
		received: time.Now(),
		status:   statusPending,
	}
	m.incoming = append(m.incoming, r)
	return r, nil
}

// addIncoming is addIncomingLocked for tests.
func (m *Manager) addIncoming(data any) (*request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addIncomingLocked(data)
}

// wholeNumber is a Python int (not a bool) from decoded JSON.
func wholeNumber(v any) (*big.Int, bool) {
	if !pyval.IsIntNotBool(v) {
		return nil, false
	}
	n, ok := new(big.Int).SetString(fmt.Sprint(v), 10)
	return n, ok
}

// isInUse tells whether the running server uses this path.
func (m *Manager) isInUse(path string) bool {
	if m.deps.FilesInUse == nil {
		return false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, p := range m.deps.FilesInUse() {
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil && abs == target {
			return true
		}
	}
	return false
}

// freeName is the first name for a renamed file that is not used yet:
// "{sender}_{name}", then "_2", "_3", ... (_free_name).
func freeName(directory, name, sender string, taken map[string]bool) string {
	prefixed := name
	if !strings.HasPrefix(name, sender+"_") {
		prefixed = sender + "_" + name
	}
	stem, extension := splitExt(prefixed)
	candidate := prefixed
	for counter := 2; taken[candidate] || exists(filepath.Join(directory, candidate)); counter++ {
		candidate = fmt.Sprintf("%s_%d%s", stem, counter, extension)
	}
	return candidate
}

// planFiles gives every incoming file its case, possible actions, default
// action and suggested new name (_plan_files):
//
//	new        no file with this name: save (default), rename, skip
//	same       a file with the same content exists: skip (default), replace, rename
//	different  another file with this name exists: rename (default), replace, skip
//	in_use     the running server uses this file: rename (default), skip
func (m *Manager) planFiles(r *request) ([]FilePlan, error) {
	directories := m.directories()
	targets := make([]string, len(r.files))
	taken := map[string]map[string]bool{} // file type -> target names
	for i, f := range r.files {
		targets[i] = f.name
		if isPrefixedType(f.fileType) {
			targets[i] = r.from + "_" + f.name
		}
		if taken[f.fileType] == nil {
			taken[f.fileType] = map[string]bool{}
		}
		taken[f.fileType][targets[i]] = true
	}

	plans := make([]FilePlan, 0, len(r.files))
	for i, f := range r.files {
		directory := directories[f.fileType]
		path := filepath.Join(directory, targets[i])
		plan := FilePlan{Type: f.fileType, Name: f.name, Size: f.size, Target: targets[i]}
		switch {
		case !exists(path):
			plan.Case, plan.Actions, plan.DefaultAction = "new", []string{"save", "rename", "skip"}, "save"
		case !isFile(path) || m.isInUse(path):
			plan.Case, plan.Actions, plan.DefaultAction = "in_use", []string{"rename", "skip"}, "rename"
		default:
			same := false
			if f.sha256 != "" {
				sum, err := fileSHA256(path)
				if err != nil {
					return nil, err
				}
				same = sum == f.sha256
			}
			if same {
				plan.Case, plan.Actions, plan.DefaultAction = "same", []string{"replace", "rename", "skip"}, "skip"
			} else {
				plan.Case, plan.Actions, plan.DefaultAction = "different", []string{"replace", "rename", "skip"}, "rename"
			}
		}
		plan.SuggestedName = freeName(directory, targets[i], r.from, taken[f.fileType])
		plans = append(plans, plan)
	}
	return plans, nil
}

// plannedFile is a file with the action the admin chose and the name to
// save it as ("" for skip).
type plannedFile struct {
	file   incomingFile
	action string
	saveAs string
}

// buildPlan checks the admin's choices against the current files
// (_build_plan). Nothing is changed on disk here, so on an error the request
// stays open and the admin can choose again. choices is a list of {"type",
// "name", "action", "new_name"}; files without a choice get their default
// action.
func (m *Manager) buildPlan(r *request, choices any) ([]plannedFile, error) {
	byFile := map[[2]string]map[string]any{}
	if list, ok := choices.([]any); ok {
		for _, item := range list {
			choice, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if err := unhashable(choice["type"]); err != nil {
				return nil, err
			}
			if err := unhashable(choice["name"]); err != nil {
				return nil, err
			}
			fileType, typeOK := choice["type"].(string)
			name, nameOK := choice["name"].(string)
			if typeOK && nameOK { // other keys can never match a file
				byFile[[2]string{fileType, name}] = choice
			}
		}
	}

	directories := m.directories()
	plans, err := m.planFiles(r)
	if err != nil {
		return nil, err
	}
	var plan []plannedFile
	var errs []string
	chosen := map[[2]string]bool{}
	for i, f := range r.files {
		filePlan := plans[i]
		choice := byFile[[2]string{f.fileType, f.name}]
		var action any = choice["action"]
		if !pyval.Truthy(action) {
			action = filePlan.DefaultAction
		}
		actionText, _ := action.(string)
		if !slices.Contains(filePlan.Actions, actionText) || pyval.KindOf(action) != pyval.KindStr {
			errs = append(errs, fmt.Sprintf("%s: '%s' is not possible for this file now", f.name, pyval.Str(action)))
			continue
		}
		if actionText == "skip" {
			plan = append(plan, plannedFile{file: f, action: actionText})
			continue
		}

		var name any = filePlan.Target
		if actionText == "rename" {
			name = choice["new_name"]
			if !pyval.Truthy(name) {
				name = filePlan.SuggestedName
			}
			if !isAllowedFile(f.fileType, name) {
				errs = append(errs, fmt.Sprintf("%s: '%s' is not a valid name for this file", f.name, pyval.Str(name)))
				continue
			}
			if exists(filepath.Join(directories[f.fileType], name.(string))) {
				errs = append(errs, fmt.Sprintf("%s: a file named '%s' already exists", f.name, name))
				continue
			}
		}
		saveAs := name.(string)
		if chosen[[2]string{f.fileType, saveAs}] {
			errs = append(errs, fmt.Sprintf("'%s' is chosen for two files", saveAs))
			continue
		}
		chosen[[2]string{f.fileType, saveAs}] = true
		plan = append(plan, plannedFile{file: f, action: actionText, saveAs: saveAs})
	}
	if len(errs) > 0 {
		return nil, newError("%s", strings.Join(errs, "; "))
	}
	return plan, nil
}

// takePendingLocked returns the request that waits for the admin. The
// caller holds mu.
func (m *Manager) takePendingLocked(requestID string) (*request, error) {
	for _, r := range m.incoming {
		if r.id == requestID && r.status == statusPending {
			return r, nil
		}
	}
	return nil, &Error{Message: "Request not found or expired", Status: http.StatusNotFound}
}

// removeRequestLocked forgets a request and tells the admin pages. The
// caller holds mu.
func (m *Manager) removeRequestLocked(r *request) {
	m.incoming = slices.DeleteFunc(m.incoming, func(other *request) bool { return other == r })
	m.notifyLocked(map[string]any{"type": "transfer_request_removed", "id": r.id})
}

// Accept downloads the files of an incoming request as the admin chose
// (accept). choices is a list of {"type", "name", "action":
// "save"|"replace"|"rename"|"skip", "new_name"}; files without a choice get
// their default action. When a choice is not possible, nothing is
// downloaded, the request stays open and the error has status 400.
func (m *Manager) Accept(ctx context.Context, requestID string, choices any) (AcceptResult, error) {
	m.mu.Lock()
	r, err := m.takePendingLocked(requestID)
	var baseURL string
	var plan []plannedFile
	if err == nil {
		baseURL, _, err = m.requireEndpoint()
	}
	if err == nil {
		plan, err = m.buildPlan(r, choices)
	}
	if err == nil {
		r.status = statusDownloading
	}
	m.mu.Unlock()
	if err != nil {
		return AcceptResult{}, err
	}

	senderURL := baseURL + "/" + r.from
	result := AcceptResult{From: r.from, Saved: []SavedFile{}, Skipped: []FileAction{}, Failed: []FailedFile{}}
	renamedMedia := map[mediaFile]string{}
	// Images and attachments first, so quizzes can point to their saved names.
	slices.SortStableFunc(plan, func(a, b plannedFile) int {
		return boolOrder(!isMediaType(a.file.fileType)) - boolOrder(!isMediaType(b.file.fileType))
	})
	err = m.downloadAll(ctx, r, senderURL, plan, renamedMedia, &result)
	if err == nil {
		status := "accepted"
		if len(result.Failed) > 0 && len(result.Saved) == 0 {
			status = "failed"
		}
		m.reportResult(ctx, senderURL, r.token, status)
	}
	m.mu.Lock()
	m.removeRequestLocked(r)
	m.mu.Unlock()
	if err != nil {
		return AcceptResult{}, err
	}
	m.log.Info(fmt.Sprintf("File transfer from '%s': %d saved, %d skipped, %d failed",
		r.from, len(result.Saved), len(result.Skipped), len(result.Failed)))
	return result, nil
}

func boolOrder(b bool) int {
	if b {
		return 1
	}
	return 0
}

// downloadAll downloads the planned files in order. A transfer error fails
// only its file; any other error (the disk) stops the whole download, as an
// exception did in Python.
func (m *Manager) downloadAll(ctx context.Context, r *request, senderURL string, plan []plannedFile,
	renamedMedia map[mediaFile]string, result *AcceptResult) error {
	for _, p := range plan {
		info := FileAction{Type: p.file.fileType, Name: p.file.name, Action: p.action}
		if p.action == "skip" {
			result.Skipped = append(result.Skipped, info)
			continue
		}
		err := m.downloadFile(ctx, senderURL, r.token, r.from, p.file, p.saveAs, p.action == "replace", renamedMedia)
		var transferError *Error
		switch {
		case err == nil:
			if isMediaType(p.file.fileType) && p.saveAs != p.file.name {
				renamedMedia[mediaFile{p.file.fileType, p.file.name}] = p.saveAs
			}
			result.Saved = append(result.Saved, SavedFile{Type: info.Type, Name: info.Name, Action: info.Action, SavedAs: p.saveAs})
		case errors.As(err, &transferError):
			result.Failed = append(result.Failed, FailedFile{Type: info.Type, Name: info.Name, Action: info.Action, Error: transferError.Message})
		default:
			return err
		}
	}
	return nil
}

// downloadFile downloads one file and saves it under the chosen name
// (_download_file). overwrite replaces an existing file with this name.
// renamedMedia holds the images and attachments saved under a new name;
// references in a downloaded quiz are pointed to them.
func (m *Manager) downloadFile(ctx context.Context, senderURL, token, sender string, f incomingFile, saveAs string,
	overwrite bool, renamedMedia map[mediaFile]string) error {
	url := fmt.Sprintf("%s/api/transfer/download/%s/%s/%s", senderURL, token, f.fileType, pyQuote(f.name))
	directory := m.directories()[f.fileType]
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".transfer-*.part")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		if exists(tempPath) {
			_ = os.Remove(tempPath)
		}
	}()
	err = m.fetch(ctx, url, sender, temp)
	if closeErr := temp.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if f.fileType == "quizzes" && len(renamedMedia) > 0 {
		if _, err := rewriteQuizMedia(tempPath, renamedMedia); err != nil {
			var bad *loadError
			if !errors.As(err, &bad) {
				return err
			}
			m.log.Warn(fmt.Sprintf("Cannot update file references in received quiz: %v", err))
		}
	}
	finalPath := filepath.Join(directory, saveAs)
	if !overwrite && exists(finalPath) {
		return newError("A file named '%s' appeared meanwhile", saveAs)
	}
	if overwrite && m.isInUse(finalPath) {
		return newError("'%s' is used by the running server", saveAs)
	}
	return os.Rename(tempPath, finalPath)
}

// reportResult tells the sender what happened to its offer. A failure is
// only logged.
func (m *Manager) reportResult(ctx context.Context, senderURL, token, status string) {
	if _, _, err := m.postJSON(ctx, senderURL+"/api/transfer/result", resultBody{Token: token, Status: status}); err != nil {
		m.log.Warn(fmt.Sprintf("Could not report transfer result to sender: %v", err))
	}
}

// Reject forgets an incoming request and tells the sender (reject).
func (m *Manager) Reject(ctx context.Context, requestID string) error {
	m.mu.Lock()
	r, err := m.takePendingLocked(requestID)
	if err == nil {
		m.removeRequestLocked(r)
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if baseURL, _, ok := m.endpoint(); ok {
		m.reportResult(ctx, baseURL+"/"+r.from, r.token, "rejected")
	}
	m.log.Info(fmt.Sprintf("File transfer from '%s' rejected", r.from))
	return nil
}

// isoformat is Python's datetime.isoformat() of a local time:
// microseconds only when they are not zero.
func isoformat(t time.Time) string {
	t = t.Local()
	text := t.Format("2006-01-02T15:04:05")
	if microseconds := t.Nanosecond() / 1000; microseconds != 0 {
		text += fmt.Sprintf(".%06d", microseconds)
	}
	return text
}

// randomToken is secrets.token_urlsafe(32): 43 URL-safe characters.
func randomToken() string {
	data := make([]byte, 32)
	_, _ = rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}

// randomID is secrets.token_hex(8): 16 hex characters.
func randomID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return hex.EncodeToString(data)
}

// exists is os.path.exists: false for any error.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isFile is os.path.isfile.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
