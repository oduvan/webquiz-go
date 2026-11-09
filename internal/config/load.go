package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// russianLanguage is what the Python version logged for "language: ru".
const russianLanguage = "російська мова не підтримується, але ти москалику попався"

// Load reads a config file like load_config_from_yaml. It always returns a
// usable config: when the file is missing (the error wraps fs.ErrNotExist)
// or cannot be used, it returns the defaults and the error, and the caller
// logs it, as the Python version did.
//
// Values are read with PyYAML's rules. Like the Python version, Load fails
// (giving the defaults) for a section that is not a mapping, an unknown key
// in a section, and a quiz or checker template without its required keys.
// Where Python kept a value of the wrong type, Load converts it when the
// meaning is clear (a number as master_key, text as port, truth rules for
// true/false settings) and fails otherwise. ConfigPath is left empty.
func Load(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Default(), err
	}
	data, err := pyval.LoadYAML(content)
	if err != nil {
		return Default(), err
	}
	cfg, err := fromData(data)
	if err != nil {
		return Default(), err
	}
	return cfg, nil
}

// fromData builds a Config from the decoded file.
func fromData(data any) (*Config, error) {
	cfg := Default()
	if !pyval.Truthy(data) {
		return cfg, nil // an empty file
	}
	m, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the config must be a mapping of sections, not %s", pyval.KindOf(data))
	}

	if value, ok := m["language"]; ok {
		cfg.Language = stringOr(value, "uk")
	}
	if cfg.Language == "ru" {
		cfg.LoadWarnings = append(cfg.LoadWarnings, russianLanguage)
		cfg.Language = "uk"
	}
	cfg.ExtraAnswersWithUsersCSV = pyval.Truthy(m["extra_answers_with_users_csv"])

	if err := readSection(m, "server", func(s *section) {
		s.str("host", &cfg.Server.Host)
		s.integer("port", &cfg.Server.Port)
		s.boolean("include_ipv6", &cfg.Server.IncludeIPv6)
		s.str("url_format", &cfg.Server.URLFormat)
	}); err != nil {
		return nil, err
	}
	if err := readSection(m, "paths", func(s *section) {
		s.str("quizzes_dir", &cfg.Paths.QuizzesDir)
		s.str("logs_dir", &cfg.Paths.LogsDir)
		s.str("csv_dir", &cfg.Paths.CSVDir)
		s.str("static_dir", &cfg.Paths.StaticDir)
	}); err != nil {
		return nil, err
	}
	if err := readSection(m, "admin", func(s *section) {
		s.str("master_key", &cfg.Admin.MasterKey)
		s.strings("trusted_ips", &cfg.Admin.TrustedIPs)
	}); err != nil {
		return nil, err
	}
	if err := readSection(m, "registration", func(s *section) {
		s.strings("fields", &cfg.Registration.Fields)
		s.strings("disabled_fields", &cfg.Registration.DisabledFields)
		s.boolean("approve", &cfg.Registration.Approve)
		s.str("username_label", &cfg.Registration.UsernameLabel)
	}); err != nil {
		return nil, err
	}

	quizzes, err := readList(m, "quizzes", []string{"name", "download_path", "folder"})
	if err != nil {
		return nil, err
	}
	for _, q := range quizzes {
		cfg.Quizzes = append(cfg.Quizzes, DownloadableQuiz{
			Name:            stringOr(q["name"], ""),
			DownloadPath:    stringOr(q["download_path"], ""),
			Folder:          stringOr(q["folder"], ""),
			AuthHeaderToken: stringOr(q["auth_header_token"], ""),
		})
	}
	templates, err := readList(m, "checker_templates", []string{"name", "code"})
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		cfg.CheckerTemplates = append(cfg.CheckerTemplates, CheckerTemplate{Name: stringOr(t["name"], ""), Code: stringOr(t["code"], "")})
	}

	if err := readTunnel(m, &cfg.Tunnel); err != nil {
		return nil, err
	}
	cfg.Tunnel.ResolveKeyPaths()
	return cfg, nil
}

// section reads the keys of one config section; the first problem is kept in err.
type section struct {
	name   string
	values map[string]any
	known  map[string]bool
	err    error
}

// readSection reads an optional section with the given reader. Like the
// Python dataclasses, a section that is given must be a mapping with known keys.
func readSection(m map[string]any, name string, read func(*section)) error {
	value, ok := m[name]
	if !ok {
		return nil
	}
	values, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("the '%s' section must be a mapping, not %s", name, pyval.KindOf(value))
	}
	s := &section{name: name, values: values, known: map[string]bool{}}
	read(s)
	if s.err != nil {
		return s.err
	}
	for key := range values {
		if !s.known[key] {
			return fmt.Errorf("unknown key '%s' in the '%s' section", key, name)
		}
	}
	return nil
}

func (s *section) take(key string) (any, bool) {
	s.known[key] = true
	value, ok := s.values[key]
	if !ok || value == nil {
		return nil, false // a missing or empty value keeps the default
	}
	return value, true
}

func (s *section) fail(key string, format string, args ...any) {
	if s.err == nil {
		s.err = fmt.Errorf("'%s.%s' %s", s.name, key, fmt.Sprintf(format, args...))
	}
}

func (s *section) str(key string, target *string) {
	value, ok := s.take(key)
	if !ok {
		return
	}
	text, ok := scalarText(value)
	if !ok {
		s.fail(key, "must be text, not %s", pyval.KindOf(value))
		return
	}
	*target = text
}

func (s *section) integer(key string, target *int) {
	value, ok := s.take(key)
	if !ok {
		return
	}
	if pyval.IsInt(value) {
		if i, ok := pyval.Int(value); ok && int64(int(i)) == i {
			*target = int(i)
			return
		}
	}
	if text, isText := value.(string); isText {
		if i, err := strconv.Atoi(pyval.Strip(text)); err == nil {
			*target = i
			return
		}
	}
	s.fail(key, "must be a whole number, not %s", pyval.Repr(value))
}

// boolean reads a true/false setting with Python's truth rules ("if value").
func (s *section) boolean(key string, target *bool) {
	s.known[key] = true
	if value, ok := s.values[key]; ok {
		*target = pyval.Truthy(value)
	}
}

func (s *section) strings(key string, target *[]string) {
	value, ok := s.take(key)
	if !ok {
		return
	}
	items, ok := pyval.List(value)
	if !ok {
		s.fail(key, "must be a list, not %s", pyval.KindOf(value))
		return
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := scalarText(item)
		if !ok {
			s.fail(key, "must be a list of text values")
			return
		}
		texts = append(texts, text)
	}
	*target = texts
}

// scalarText reads a text setting: text as it is, numbers and booleans as
// Python's str() writes them. ok is false for lists and mappings.
func scalarText(value any) (string, bool) {
	switch pyval.KindOf(value) {
	case pyval.KindStr, pyval.KindBool, pyval.KindInt, pyval.KindFloat, pyval.KindNone:
		return pyval.Str(value), true
	}
	if t, ok := value.(pyval.Timestamp); ok {
		return string(t), true
	}
	return "", false
}

// stringOr reads a text value; nil and values that are not text-like give fallback.
func stringOr(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	if text, ok := scalarText(value); ok {
		return text
	}
	return fallback
}

// readList reads "quizzes" or "checker_templates": a list of mappings that
// must have the required keys. A missing or empty value gives no entries.
func readList(m map[string]any, name string, required []string) ([]map[string]any, error) {
	value := m[name]
	if !pyval.Truthy(value) {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("'%s' must be a list, not %s", name, pyval.KindOf(value))
	}
	entries := make([]map[string]any, 0, len(items))
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("'%s' entry %d must be a mapping", name, i+1)
		}
		for _, key := range required {
			if _, ok := entry[key]; !ok {
				return nil, fmt.Errorf("'%s' entry %d has no '%s'", name, i+1, key)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// readTunnel reads the "tunnel" section with its optional "config" subsection.
func readTunnel(m map[string]any, tunnel *TunnelConfig) error {
	var serverConfig map[string]any
	err := readSection(m, "tunnel", func(s *section) {
		s.str("server", &tunnel.Server)
		s.str("public_key", &tunnel.PublicKey)
		s.str("private_key", &tunnel.PrivateKey)
		s.str("socket_name", &tunnel.SocketName)
		s.known["config"] = true
		if value, ok := s.values["config"]; ok {
			values, isMap := value.(map[string]any)
			if !isMap {
				s.fail("config", "must be a mapping, not %s", pyval.KindOf(value))
				return
			}
			serverConfig = values
		}
	})
	if err != nil || serverConfig == nil {
		return err
	}
	// All three keys are required, as in the Python TunnelServerConfig.
	for _, key := range []string{"username", "socket_directory", "base_url"} {
		if _, ok := serverConfig[key]; !ok {
			return fmt.Errorf("'tunnel.config' has no '%s'", key)
		}
	}
	tunnel.Config = &TunnelServerConfig{}
	return readSection(map[string]any{"tunnel.config": serverConfig}, "tunnel.config", func(s *section) {
		s.str("username", &tunnel.Config.Username)
		s.str("socket_directory", &tunnel.Config.SocketDirectory)
		s.str("base_url", &tunnel.Config.BaseURL)
	})
}

// Overrides are command line values that win over the config file. A nil
// field keeps the file value; an empty string still counts as given (as
// with Python's "is not None").
type Overrides struct {
	Host       *string
	Port       *int
	URLFormat  *string
	QuizzesDir *string
	LogsDir    *string
	CSVDir     *string
	StaticDir  *string
	MasterKey  *string
}

// ApplyOverrides puts the command line values into cfg, then the
// WEBQUIZ_MASTER_KEY environment variable when it is set and no master key
// was given on the command line (an empty --master-key does not count).
// It returns true when the master key came from the environment.
func ApplyOverrides(cfg *Config, o Overrides) bool {
	setString := func(target *string, value *string) {
		if value != nil {
			*target = *value
		}
	}
	setString(&cfg.Server.Host, o.Host)
	if o.Port != nil {
		cfg.Server.Port = *o.Port
	}
	setString(&cfg.Server.URLFormat, o.URLFormat)
	setString(&cfg.Paths.QuizzesDir, o.QuizzesDir)
	setString(&cfg.Paths.LogsDir, o.LogsDir)
	setString(&cfg.Paths.CSVDir, o.CSVDir)
	setString(&cfg.Paths.StaticDir, o.StaticDir)
	setString(&cfg.Admin.MasterKey, o.MasterKey)

	envKey := os.Getenv("WEBQUIZ_MASTER_KEY")
	if envKey != "" && (o.MasterKey == nil || *o.MasterKey == "") {
		cfg.Admin.MasterKey = envKey
		return true
	}
	return false
}

// DefaultConfigPath is get_default_config_path: webquiz.yaml in the program's
// folder (WEBQUIZ_BINARY_DIR) or else in the working folder. When the file
// does not exist it is made from ExampleConfig. It returns "" when the file
// cannot be made (and logs a warning).
func DefaultConfigPath(log *slog.Logger) string {
	log = orDiscard(log)
	dir := os.Getenv("WEBQUIZ_BINARY_DIR")
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Warn(fmt.Sprintf("Could not create default config file: %v", err))
			return ""
		}
		dir = wd
	}
	path := filepath.Join(dir, "webquiz.yaml")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if err := CreateDefaultConfigFile(path); err != nil {
		log.Warn(fmt.Sprintf("Could not create default config file at %s: %v", path, err))
		return ""
	}
	log.Info(fmt.Sprintf("Created default config file: %s", path))
	return path
}

// CreateDefaultConfigFile writes ExampleConfig to path.
func CreateDefaultConfigFile(path string) error {
	return os.WriteFile(path, []byte(ExampleConfig), 0o644)
}

// LoadWithOverrides is load_config_with_overrides. With an empty path it uses
// DefaultConfigPath; a named file that does not exist is made from
// ExampleConfig first. Then it applies the overrides (ApplyOverrides) and
// sets ConfigPath. Problems in the file are logged and the defaults are used,
// as in the Python version; the error is only for a named config file that
// cannot be made. log may be nil.
func LoadWithOverrides(path string, o Overrides, log *slog.Logger) (*Config, error) {
	log = orDiscard(log)
	if path == "" {
		path = DefaultConfigPath(log)
	}

	var cfg *Config
	switch {
	case path == "":
		cfg = Default()
		log.Info("Using default configuration")
	default:
		created := false
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			if err := CreateDefaultConfigFile(path); err != nil {
				return nil, fmt.Errorf("create the config file %s: %w", path, err)
			}
			log.Info(fmt.Sprintf("Created default config file: %s", path))
			created = true
		}
		loaded, err := Load(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			log.Warn(fmt.Sprintf("Config file not found: %s, using defaults", path))
		case err != nil:
			log.Error(fmt.Sprintf("Error loading config from %s: %v", path, err))
		case created:
			log.Info(fmt.Sprintf("Loaded configuration from newly created: %s", path))
		default:
			log.Info(fmt.Sprintf("Loaded configuration from: %s", path))
		}
		for _, warning := range loaded.LoadWarnings {
			log.Error(warning)
		}
		cfg = loaded
	}

	if ApplyOverrides(cfg, o) {
		log.Info("Master key loaded from environment variable")
	}
	cfg.ConfigPath = path
	return cfg, nil
}

func orDiscard(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return log
}
