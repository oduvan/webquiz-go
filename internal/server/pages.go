package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/i18n"
	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/quiz"
	"github.com/oduvan/webquiz-go/internal/timelimits"
	"github.com/oduvan/webquiz-go/internal/version"
)

// The pages are the HTML files of the web build (web/dist). Each has two
// markers that the server fills in: the title, HTML-escaped, and the boot
// data, JSON in which <, > and & are written as <, > and & so
// that text like "</script>" cannot end the script. The structs below are
// the boot data of each page; web/src/shared/boot.ts has the same shapes.
//
// They carry what the Python templates got through {{PLACEHOLDERS}} and the
// JavaScript constants injected into admin.html and files.html.

// The page files of the web build.
const (
	quizPageFile      = "index.html"
	adminPageFile     = "admin.html"
	filesPageFile     = "files.html"
	liveStatsPageFile = "live-stats.html"
	csvTablePageFile  = "csv-table.html"
)

// The states of the quiz page.
const (
	quizStateQuiz              = "quiz"                    // a quiz runs
	quizStateSelectionRequired = "quiz_selection_required" // several quizzes and no default.yaml
)

// The page titles of the Python templates.
const (
	selectionPageTitle = "WebQuiz - Admin Selection Required"
	adminPageTitle     = "Панель Адміністратора WebQuiz"
	filesPageTitle     = "WebQuiz File Manager"
	liveStatsPageTitle = "WebQuiz Live Stats"
	csvTablePageTitle  = "CSV Table - WebQuiz"
)

// QuizBoot is the boot data of the quiz page: the file static_dir/index.html,
// written when a quiz starts and served at "/". Python built the same page
// from templates/index.html (create_default_index_html), or wrote
// templates/quiz_selection_required.html instead.
type QuizBoot struct {
	// State is "quiz", or "quiz_selection_required" when several quiz files
	// exist and none is default.yaml: the page then lists Quizzes and asks
	// the admin to choose one in the admin panel.
	State        string            `json:"state"`
	Language     string            `json:"language"`     // the interface language: "uk" or "en"
	Translations map[string]string `json:"translations"` // TRANSLATIONS_JSON: every text of translations.py
	Version      string            `json:"version"`      // WEBQUIZ_VERSION

	Title string `json:"title"` // QUIZ_TITLE: shown as plain text
	// Questions is QUESTIONS_DATA: the questions without their answers (see
	// clientQuestions).
	Questions          []map[string]any    `json:"questions"`
	UsernameLabel      string              `json:"username_label"`      // USERNAME_LABEL: config label or the translated one
	RegistrationFields []RegistrationField `json:"registration_fields"` // REGISTRATION_FIELDS: the extra fields of the form
	ShowRightAnswer    bool                `json:"show_right_answer"`   // SHOW_RIGHT_ANSWER
	ShowFinalList      bool                `json:"show_final_list"`     // SHOW_FINAL_LIST
	QuizTimeLimit      int                 `json:"quiz_time_limit"`     // QUIZ_TIME_LIMIT: whole-quiz seconds, 0 = none

	Quizzes []QuizChoice `json:"quizzes"` // QUIZ_LIST: the quiz files (state quiz_selection_required only)
}

// RegistrationField is an extra field of the registration form. Name is the
// key the student's value is sent under: the label in lower case with "_"
// for spaces.
type RegistrationField struct {
	Label string `json:"label"`
	Name  string `json:"name"`
}

// QuizChoice is a quiz file on the "select a quiz first" page. Title is the
// quiz title as text (Python's str()), or null when the file has none.
type QuizChoice struct {
	Filename string  `json:"filename"`
	Title    *string `json:"title"`
}

// AdminBoot is the boot data of the admin page (/admin/): the constants
// Python injected into admin.html, plus the project addresses.
type AdminBoot struct {
	IsTrustedIP         bool                   `json:"is_trusted_ip"`        // IS_TRUSTED_IP: log in without the key
	NetworkInfo         NetworkInfo            `json:"network_info"`         // NETWORK_INFO
	DownloadableQuizzes []DownloadableQuizInfo `json:"downloadable_quizzes"` // DOWNLOADABLE_QUIZZES (never the tokens)
	Version             string                 `json:"version"`              // WEBQUIZ_VERSION
	TunnelPublicKey     string                 `json:"tunnel_public_key"`    // TUNNEL_PUBLIC_KEY: "" without a tunnel
	// RepoURL and PagesURL are internal/version's addresses: release
	// downloads, the "new version" check and the online docs. The Python page
	// had its own addresses in the code; the Go project keeps them in one place.
	RepoURL  string `json:"repo_url"`
	PagesURL string `json:"pages_url"`
}

// NetworkInfo lists the addresses where students reach the quiz.
type NetworkInfo struct {
	URLs []NetworkURL `json:"urls"`
}

// NetworkURL is one address: label "Network Access (<ip>)" and the URL made
// from server.url_format.
type NetworkURL struct {
	Label   string `json:"label"`
	QuizURL string `json:"quiz_url"`
}

// DownloadableQuizInfo is an entry of the config's "quizzes" list without
// its auth_header_token.
type DownloadableQuizInfo struct {
	Name         string `json:"name"`
	DownloadPath string `json:"download_path"`
	Folder       string `json:"folder"`
}

// FilesBoot is the boot data of the file manager (/files/): the constants
// Python injected into files.html. The three config values are null when the
// server runs without a config file.
type FilesBoot struct {
	IsTrustedIP   bool    `json:"is_trusted_ip"`  // IS_TRUSTED_IP
	ConfigContent *string `json:"config_content"` // CONFIG_CONTENT: the config file text
	ConfigPath    *string `json:"config_path"`    // CONFIG_PATH
	// ConfigData is CONFIG_DATA for the config form:
	// {"language", "extra_answers_with_users_csv", "registration"}, or null
	// when the file is not valid YAML.
	ConfigData any `json:"config_data"`
}

// LiveStatsBoot is the boot data of the live stats page (/live-stats/).
// The page gets everything over its WebSocket.
type LiveStatsBoot struct{}

// CSVTableBoot is the boot data of the CSV table page (/files/csv/{name}).
// The page reads the file name from its own address.
type CSVTableBoot struct{}

// The two markers of a built page.
const (
	titleMarker = "<title>__WEBQUIZ_TITLE__</title>"
	bootOpen    = `<script id="webquiz-boot" type="application/json">`
	bootMarker  = bootOpen + "__WEBQUIZ_BOOT__</script>"
)

// templateErrorHTML is templates/template_error.html of the Python version:
// the page shown when a page of the web build is missing.
const templateErrorHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>WebQuiz Testing System</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 800px; margin: 0 auto; padding: 20px; }
        .container { background: white; padding: 30px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
    </style>
</head>
<body>
    <div class="container">
        <h1>WebQuiz Testing System</h1>
        <div id="error">
            <h2>Template Error</h2>
            <p>Unable to load the full interface template. Please ensure the WebQuiz package is properly installed.</p>
            <p>You can manually create an index.html file in the static directory with your custom interface.</p>
        </div>
    </div>
</body>
</html>
`

// renderPage makes a page from a page file of the web build: the title
// marker gets the escaped title and the boot marker the boot data. With
// absoluteAssets the page loads /assets/ instead of ./assets/ (the admin
// pages, which live below "/"). When the page file is missing or has no
// markers, it returns the template error page and an error for the log.
func renderPage(pages fs.FS, file, title string, boot any, absoluteAssets bool) ([]byte, error) {
	template, err := fs.ReadFile(pages, file)
	if err != nil {
		return []byte(templateErrorHTML), fmt.Errorf("Failed to load template %s: %w", file, err)
	}
	html := string(template)
	if !strings.Contains(html, titleMarker) || !strings.Contains(html, bootMarker) {
		return []byte(templateErrorHTML), fmt.Errorf("Failed to load template %s: the page has no title or boot marker", file)
	}
	bootJSON, err := json.Marshal(boot) // escapes <, > and & as <, >, &
	if err != nil {
		return nil, err
	}
	if absoluteAssets {
		// Only the page file is rewritten, before the title and the data go in.
		html = strings.NewReplacer(`src="./assets/`, `src="/assets/`, `href="./assets/`, `href="/assets/`).Replace(html)
	}
	html = strings.NewReplacer(
		titleMarker, "<title>"+escapeHTMLText(title)+"</title>",
		bootMarker, bootOpen+string(bootJSON)+"</script>",
	).Replace(html)
	return []byte(html), nil
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;")
)

// escapeHTMLText is Python's html.escape(text, quote=False).
func escapeHTMLText(text string) string { return textEscaper.Replace(text) }

// escapeHTMLAttr is Python's html.escape(text).
func escapeHTMLAttr(text string) string { return attrEscaper.Replace(text) }

// registrationFieldName is the key of a registration field:
// field_label.lower().replace(" ", "_").
func registrationFieldName(label string) string {
	return strings.ReplaceAll(strings.ToLower(label), " ", "_")
}

// interfaceLanguage is the language the texts come in: cfg.language when
// translations.py has it, else Ukrainian (get_translations' fallback).
func interfaceLanguage(language string) string {
	if slices.Contains(i18n.Languages, language) {
		return language
	}
	return i18n.Default
}

// clientQuestions is the question list of the quiz page, without the
// answers: for a choice question its options, is_multiple_choice and (for
// several answers) min_correct; for a text question (one with "checker")
// type "text" and default_value. Every question has id, points and
// time_limit (effective seconds, 0 = none); question, image and file only
// when set (file as "/attach/<name>").
func clientQuestions(q *quiz.Quiz) []map[string]any {
	list := make([]map[string]any, 0, len(q.Questions))
	for _, question := range q.Questions {
		item := map[string]any{"id": question.ID(), "points": question.Points()}
		if question.IsText() {
			item["type"] = "text"
			defaultValue, ok := question["default_value"]
			if !ok {
				defaultValue = ""
			}
			item["default_value"] = defaultValue
		} else {
			item["options"] = question["options"]
			item["is_multiple_choice"] = question.IsMultipleChoice()
			if question.IsMultipleChoice() {
				minCorrect, ok := question["min_correct"]
				if !ok {
					correct, _ := pyval.List(question["correct_answer"])
					minCorrect = len(correct)
				}
				item["min_correct"] = minCorrect
			}
		}
		if text, ok := question["question"]; ok && pyval.Truthy(text) {
			item["question"] = text
		}
		if image, ok := question["image"]; ok && pyval.Truthy(image) {
			item["image"] = image
		}
		if file := question.File(); file != "" {
			item["file"] = file
		}
		item["time_limit"] = q.QuestionTimeLimits[question.ID()]
		list = append(list, item)
	}
	return list
}

// wholeQuizSeconds is _whole_quiz_seconds: the whole-quiz limit, 0 when the
// quiz has none.
func wholeQuizSeconds(q *quiz.Quiz) int {
	if q.TimeLimit.Mode == timelimits.ModeWholeQuiz {
		return q.TimeLimit.Seconds
	}
	return 0
}

// quizBootLocked is the boot data of the quiz page for the running quiz.
// The caller holds mu.
func (s *Server) quizBootLocked() QuizBoot {
	usernameLabel := s.translations["username_label"]
	if s.cfg.Registration.UsernameLabel != "" {
		usernameLabel = s.cfg.Registration.UsernameLabel
	}
	fields := make([]RegistrationField, 0, len(s.cfg.Registration.Fields))
	for _, label := range s.cfg.Registration.Fields {
		fields = append(fields, RegistrationField{Label: label, Name: registrationFieldName(label)})
	}
	return QuizBoot{
		State:              quizStateQuiz,
		Language:           interfaceLanguage(s.cfg.Language),
		Translations:       s.translations,
		Version:            version.Version,
		Title:              s.quiz.Title,
		Questions:          clientQuestions(s.quiz),
		UsernameLabel:      usernameLabel,
		RegistrationFields: fields,
		ShowRightAnswer:    s.quiz.ShowRightAnswer,
		ShowFinalList:      s.quiz.ShowFinalList,
		QuizTimeLimit:      wholeQuizSeconds(s.quiz),
		Quizzes:            []QuizChoice{},
	}
}

// writeQuizPageLocked is create_default_index_html: it writes the quiz page
// of the running quiz to static_dir/index.html. The caller holds mu.
func (s *Server) writeQuizPageLocked() error {
	boot := s.quizBootLocked()
	page, err := renderPage(s.pages, quizPageFile, boot.Title, boot, false)
	if err != nil && page == nil {
		return err
	}
	if err != nil {
		s.log.Warn(err.Error())
	}
	return s.writeIndex(page)
}

// writeSelectionPageLocked is create_admin_selection_page: the quiz page
// says that the admin must choose a quiz, and lists the quiz files. The
// caller holds mu.
func (s *Server) writeSelectionPageLocked(files []quizFile) error {
	boot := s.quizBootLocked()
	boot.State = quizStateSelectionRequired
	boot.Title = ""
	boot.Questions = []map[string]any{}
	for _, f := range files {
		choice := QuizChoice{Filename: f.Filename}
		if pyval.Truthy(f.Title) {
			title := pyval.Str(f.Title)
			choice.Title = &title
		}
		boot.Quizzes = append(boot.Quizzes, choice)
	}
	page, err := renderPage(s.pages, quizPageFile, selectionPageTitle, boot, false)
	if err != nil && page == nil {
		return err
	}
	if err != nil {
		s.log.Warn(err.Error())
	}
	if err := s.writeIndex(page); err != nil {
		return err
	}
	s.log.Info(fmt.Sprintf("Created admin selection page: %s", filepath.Join(s.staticDir, "index.html")))
	return nil
}

// writeIndex writes static_dir/index.html.
func (s *Server) writeIndex(page []byte) error {
	if err := os.MkdirAll(s.staticDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.staticDir, "index.html"), page, 0o644)
}

// servePage renders and sends one of the admin pages.
func (s *Server) servePage(w http.ResponseWriter, file, title string, boot any) error {
	page, err := renderPage(s.pages, file, title, boot, true)
	if err != nil && page == nil {
		return err
	}
	if err != nil {
		s.log.Warn(err.Error())
	}
	writeHTML(w, http.StatusOK, page)
	return nil
}

// serveAdminPage is serve_admin_page (/admin/).
func (s *Server) serveAdminPage(w http.ResponseWriter, r *http.Request) error {
	trusted := s.isTrustedIP(getClientIP(r))
	s.mu.Lock()
	serverConfig := s.cfg.Server
	quizzes := slices.Clone(s.cfg.Quizzes)
	publicKeyPath := s.cfg.Tunnel.PublicKey
	s.mu.Unlock()

	boot := AdminBoot{
		IsTrustedIP:         trusted,
		NetworkInfo:         NetworkInfo{URLs: networkURLs(serverConfig)},
		DownloadableQuizzes: make([]DownloadableQuizInfo, 0, len(quizzes)),
		Version:             version.Version,
		TunnelPublicKey:     s.tunnelPublicKey(publicKeyPath),
		RepoURL:             version.RepoURL,
		PagesURL:            version.PagesURL,
	}
	for _, q := range quizzes {
		boot.DownloadableQuizzes = append(boot.DownloadableQuizzes, DownloadableQuizInfo{Name: q.Name, DownloadPath: q.DownloadPath, Folder: q.Folder})
	}
	return s.servePage(w, adminPageFile, adminPageTitle, boot)
}

// networkURLs are the addresses for other devices: server.url_format with
// {IP} and {PORT} filled in for every network interface, ":80/" removed.
func networkURLs(serverConfig config.ServerConfig) []NetworkURL {
	urls := []NetworkURL{}
	for _, ip := range networkInterfaces(serverConfig.IncludeIPv6) {
		url := strings.ReplaceAll(serverConfig.URLFormat, "{IP}", ip)
		url = strings.ReplaceAll(url, "{PORT}", fmt.Sprint(serverConfig.Port))
		url = strings.ReplaceAll(url, ":80/", "/") // normalize_url
		urls = append(urls, NetworkURL{Label: fmt.Sprintf("Network Access (%s)", ip), QuizURL: url})
	}
	return urls
}

// tunnelPublicKey is the text of the tunnel's public key file, or "" when
// there is no tunnel or no key yet.
func (s *Server) tunnelPublicKey(path string) string {
	if s.tunnel == nil || path == "" {
		return ""
	}
	path = config.ResolvePath(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		s.log.Error(fmt.Sprintf("Error reading tunnel public key: %v", err))
		return ""
	}
	return pyval.Strip(string(data))
}

// serveFilesPage is serve_files_page (/files/).
func (s *Server) serveFilesPage(w http.ResponseWriter, r *http.Request) error {
	trusted := s.isTrustedIP(getClientIP(r))
	s.mu.Lock()
	path := s.cfg.ConfigPath
	s.mu.Unlock()
	boot := FilesBoot{IsTrustedIP: trusted}
	if path != "" {
		content, data := s.readConfigForForm(path)
		boot.ConfigContent = &content
		boot.ConfigPath = &path
		boot.ConfigData = data
	}
	return s.servePage(w, filesPageFile, filesPageTitle, boot)
}

// readConfigForForm reads the config file for the file manager: its text and
// the values of the config form (nil when the file is not valid YAML, an
// empty text when it cannot be read).
func (s *Server) readConfigForForm(path string) (string, any) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		s.log.Warn(fmt.Sprintf("Could not read config file: %v", err))
		return "", nil
	}
	content := string(raw)
	defaults := map[string]any{"language": "uk", "extra_answers_with_users_csv": false, "registration": map[string]any{}}
	if pyval.Strip(content) == "" {
		return content, defaults
	}
	parsed, err := pyval.LoadYAML(raw)
	if err != nil {
		s.log.Warn(fmt.Sprintf("Could not read config file: %v", err))
		return content, nil
	}
	m, ok := parsed.(map[string]any)
	if !ok || len(m) == 0 {
		return content, defaults
	}
	language, ok := m["language"]
	if !ok {
		language = "uk"
	}
	registration, ok := m["registration"].(map[string]any)
	if !ok {
		registration = map[string]any{}
	}
	return content, map[string]any{
		"language":                     language,
		"extra_answers_with_users_csv": pyval.Truthy(m["extra_answers_with_users_csv"]),
		"registration":                 registration,
	}
}

// serveLiveStatsPage is serve_live_stats_page (/live-stats/).
func (s *Server) serveLiveStatsPage(w http.ResponseWriter, _ *http.Request) error {
	return s.servePage(w, liveStatsPageFile, liveStatsPageTitle, LiveStatsBoot{})
}

// serveCSVTablePage is serve_csv_table_page (/files/csv/{filename}).
func (s *Server) serveCSVTablePage(w http.ResponseWriter, _ *http.Request) error {
	return s.servePage(w, csvTablePageFile, csvTablePageTitle, CSVTableBoot{})
}
