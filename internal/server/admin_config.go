package server

import (
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
	"github.com/oduvan/webquiz-go/internal/version"
)

// Config save, version check and the tunnel buttons (server.py
// admin_update_config, apply_config_changes, admin_version_check,
// tunnel_connect, tunnel_disconnect). Downloads and attachments are in
// download.go.

// unknownVersion is what get_package_version and get_file_version gave when
// they could not tell the version.
const unknownVersion = "unknown"

// adminVersionCheck is GET /api/admin/version-check (local network only):
// it compares the version of the running program with the version of the
// program file on disk, which differ when the program was updated while the
// server runs. Python read __init__.py; here the file is run with --version.
func (s *Server) adminVersionCheck(w http.ResponseWriter, r *http.Request) error {
	running := version.Version
	if running == "" {
		running = unknownVersion
	}
	file := unknownVersion
	if s.executable != "" {
		found, err := version.FileVersion(s.executable)
		if err != nil {
			s.log.Error(fmt.Sprintf("Failed to read the version of %s: %v", s.executable, err))
		} else {
			file = found
		}
	}
	restartRequired := running != unknownVersion && file != unknownVersion && running != file
	return writeJSON(w, http.StatusOK, map[string]any{
		"running_version":  running,
		"file_version":     file,
		"restart_required": restartRequired,
	})
}

// pyContainsKey is Python's "key in request_data" for a decoded request
// body: true or false for an object, a list or a text, and Python's
// TypeError for any other value.
func pyContainsKey(data any, key string) (bool, error) {
	switch d := data.(type) {
	case map[string]any:
		_, ok := d[key]
		return ok, nil
	case []any:
		for _, item := range d {
			if text, ok := item.(string); ok && text == key {
				return true, nil
			}
		}
		return false, nil
	case string:
		return strings.Contains(d, key), nil
	}
	return false, fmt.Errorf("argument of type '%s' is not iterable", pyval.KindOf(data))
}

// adminUpdateConfig is PUT /api/admin/config (admin_update_config). The
// body is {"content": "<yaml text>"} or {"data": {section: {...}}}, a
// partial update that keeps the comments of the file.
func (s *Server) adminUpdateConfig(w http.ResponseWriter, r *http.Request) error {
	requestData, err := readJSON(r)
	if err != nil {
		return err
	}
	s.configMu.Lock() // one save at a time: a save reads the file and writes it back
	defer s.configMu.Unlock()

	var configPath string
	s.withLock(func() { configPath = s.cfg.ConfigPath })
	if configPath == "" {
		return writeError(w, http.StatusBadRequest, "No config file was specified. Server started without --config parameter.")
	}

	var content string
	hasData, err := pyContainsKey(requestData, "data")
	if err != nil {
		return err
	}
	if hasData {
		raw, err := pyGetItem(requestData, "data")
		if err != nil {
			return err
		}
		mergeData, ok := raw.(map[string]any)
		if !ok {
			return writeError(w, http.StatusBadRequest, "data must be a dictionary")
		}
		current, _ := os.ReadFile(configPath) // a missing or unreadable file counts as empty
		merged, err := config.MergeYAML(current, mergeData)
		if err != nil {
			return err
		}
		content = string(merged)
	} else {
		raw, err := pyGet(requestData, "content", "")
		if err != nil {
			return err
		}
		if content, err = pyStrip(raw); err != nil {
			return err
		}
	}

	var parsed any
	if content != "" {
		if parsed, err = pyval.LoadYAML([]byte(content)); err != nil {
			return writeError(w, http.StatusBadRequest, "Invalid YAML syntax: "+err.Error())
		}
	}
	if problems := config.Validate(parsed); len(problems) > 0 {
		return writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Configuration validation failed", "errors": problems})
	}

	original, readErr := os.ReadFile(configPath)
	hasOriginal := readErr == nil
	if err := writeFileSynced(configPath, []byte(content)); err != nil {
		return err
	}
	s.log.Info("Configuration file updated: " + configPath)

	restartReasons := []string{}
	if err := s.reloadConfig(configPath, parsed, &restartReasons); err != nil {
		s.log.Error(fmt.Sprintf("Failed to apply config changes: %v", err))
		if hasOriginal {
			if err := writeFileSynced(configPath, original); err != nil {
				return err
			}
			s.log.Info("Configuration file rolled back to previous state")
		}
		return writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success":     false,
			"error":       fmt.Sprintf("Failed to apply configuration: %v. Changes rolled back.", err),
			"config_path": configPath,
		})
	}

	var message string
	var quizRunning bool
	s.withLock(func() { quizRunning = s.currentQuizFile != "" })
	switch {
	case len(restartReasons) > 0:
		message = fmt.Sprintf("Configuration saved. Server restart required for: %s. Other changes applied.", strings.Join(restartReasons, ", "))
	case quizRunning:
		message = "Configuration saved and applied. Quiz restarted."
	default:
		message = "Configuration saved and applied."
	}

	savedContent, savedData := s.savedConfig(configPath)
	return writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"message":          message,
		"config_path":      configPath,
		"restart_required": restartReasons,
		"config_content":   savedContent,
		"config_data":      savedData,
	})
}

// reloadConfig is the "try" block of admin_update_config: it reads the saved
// file, finds the changes that need a restart (into *restart), applies the
// others and restarts the running quiz. A panic counts as a failure, so the
// caller can roll the file back.
func (s *Server) reloadConfig(configPath string, raw any, restart *[]string) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("%v", value)
		}
	}()
	// Like load_config_from_yaml: a file that cannot be read as a config
	// gives the defaults, and the problem goes to the log.
	newConfig, loadErr := config.Load(configPath)
	switch {
	case errors.Is(loadErr, fs.ErrNotExist):
		s.log.Warn(fmt.Sprintf("Config file not found: %s, using defaults", configPath))
	case loadErr != nil:
		s.log.Error(fmt.Sprintf("Error loading config from %s: %v", configPath, loadErr))
	}
	for _, warning := range newConfig.LoadWarnings {
		s.log.Error(warning)
	}
	newConfig.ConfigPath = configPath

	*restart = s.configRequiresRestart(newConfig, raw)
	if s.configHook != nil {
		if err := s.configHook(newConfig); err != nil {
			return err
		}
	}
	s.applyConfigChanges(newConfig)
	s.restartCurrentQuiz()
	return nil
}

// configRequiresRestart is _config_requires_restart: the settings that the
// saved text names and that cannot change while the server runs. Sections
// the text does not name count as unchanged.
func (s *Server) configRequiresRestart(newConfig *config.Config, raw any) []string {
	reasons := []string{}
	rawConfig, _ := pyval.Dict(raw)
	section := func(name string) map[string]any {
		data, _ := pyval.Dict(rawConfig[name])
		return data
	}
	s.withLock(func() {
		if data := section("server"); hasKey(rawConfig, "server") {
			if hasKey(data, "host") && s.cfg.Server.Host != newConfig.Server.Host {
				reasons = append(reasons, "server.host")
			}
			if hasKey(data, "port") && s.cfg.Server.Port != newConfig.Server.Port {
				reasons = append(reasons, "server.port")
			}
		}
		if data := section("paths"); hasKey(rawConfig, "paths") {
			for _, path := range []struct{ key, current, changed string }{
				{"quizzes_dir", s.quizzesDir, newConfig.Paths.QuizzesDir},
				{"logs_dir", s.logsDir, newConfig.Paths.LogsDir},
				{"csv_dir", s.csvDir, newConfig.Paths.CSVDir},
				{"static_dir", s.staticDir, newConfig.Paths.StaticDir},
			} {
				if hasKey(data, path.key) && path.current != path.changed {
					reasons = append(reasons, "paths."+path.key)
				}
			}
		}
		if data := section("admin"); hasKey(rawConfig, "admin") {
			if hasKey(data, "master_key") && s.masterKey != newConfig.Admin.MasterKey {
				reasons = append(reasons, "admin.master_key")
			}
		}
	})
	return reasons
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// applyConfigChanges is apply_config_changes: the settings that can change
// while the server runs. server, paths and admin.master_key stay as they
// are. An open tunnel is closed; the admin can connect again with the new
// settings.
func (s *Server) applyConfigChanges(newConfig *config.Config) {
	s.withLock(func() {
		s.cfg.Language = newConfig.Language
		s.translations = i18n.Get(newConfig.Language)
		s.cfg.ExtraAnswersWithUsersCSV = newConfig.ExtraAnswersWithUsersCSV
		s.cfg.Registration = newConfig.Registration
		s.cfg.Admin.TrustedIPs = slices.Clone(newConfig.Admin.TrustedIPs)
		s.cfg.Quizzes = slices.Clone(newConfig.Quizzes)
		s.cfg.Tunnel = newConfig.Tunnel
	})
	if s.tunnel != nil {
		s.applyTunnelConfig(newConfig.Tunnel)
		if s.tunnel.Status().Connected {
			s.log.Info("Disconnecting tunnel due to config change")
			s.tunnel.Disconnect()
		}
	}
	s.log.Info("Configuration changes applied to running server")
}

// restartCurrentQuiz is restart_current_quiz: it starts the running quiz
// again as a new run, so the pages are made with the new settings. It does
// nothing when no quiz runs; a failure is only logged.
func (s *Server) restartCurrentQuiz() {
	var current string
	s.withLock(func() { current = s.currentQuizFile })
	if current == "" {
		return
	}
	if _, err := os.Stat(current); err != nil {
		return
	}
	name := filepath.Base(current)
	if err := s.SwitchQuiz(name, nil); err != nil {
		s.log.Error(fmt.Sprintf("Failed to restart quiz after config change: %v", err))
		return
	}
	s.log.Info("Quiz restarted after config change: " + name)
}

// savedConfig reads the saved file back for the answer: its text and the
// values of the config form.
func (s *Server) savedConfig(path string) (string, map[string]any) {
	var data map[string]any
	s.withLock(func() {
		data = map[string]any{
			"language":                     s.cfg.Language,
			"extra_answers_with_users_csv": s.cfg.ExtraAnswersWithUsersCSV,
			"registration":                 map[string]any{},
		}
	})
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", data
	}
	content := string(raw)
	if pyval.Strip(content) == "" {
		return content, data
	}
	parsed, err := pyval.LoadYAML(raw)
	if err != nil {
		return content, data
	}
	if m, ok := parsed.(map[string]any); ok && len(m) > 0 {
		language, ok := m["language"]
		if !ok {
			language = "uk"
		}
		registration, ok := m["registration"].(map[string]any)
		if !ok {
			registration = map[string]any{}
		}
		data = map[string]any{
			"language":                     language,
			"extra_answers_with_users_csv": pyval.Truthy(m["extra_answers_with_users_csv"]),
			"registration":                 registration,
		}
	}
	return content, data
}

// writeFileSynced writes a file and makes sure it reached the disk before
// it returns (SD cards and exFAT lose data when the power goes soon after a
// save).
func writeFileSynced(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// tunnelConnect is POST /api/admin/tunnel/connect.
func (s *Server) tunnelConnect(w http.ResponseWriter, r *http.Request) error {
	if s.tunnel == nil {
		return writeError(w, http.StatusBadRequest, "Tunnel not configured")
	}
	// aiohttp did not stop a handler when its client went away, and the tunnel
	// must outlive the request: it lives as long as the server.
	if err := s.tunnel.Connect(s.ctx); err != nil {
		s.log.Error(fmt.Sprintf("Tunnel connection failed: %v", err))
		return writeError(w, http.StatusInternalServerError, err.Error())
	}
	url := string(s.tunnel.Status().URL)
	s.log.Info("Tunnel connected: " + url)
	return writeJSON(w, http.StatusOK, map[string]any{"success": true, "url": url})
}

// tunnelDisconnect is POST /api/admin/tunnel/disconnect.
func (s *Server) tunnelDisconnect(w http.ResponseWriter, r *http.Request) error {
	if s.tunnel == nil {
		return writeError(w, http.StatusBadRequest, "Tunnel not configured")
	}
	s.tunnel.Disconnect()
	s.log.Info("Tunnel disconnected")
	return writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
