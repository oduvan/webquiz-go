package config

import (
	_ "embed"
	"os"
	"path/filepath"
)

// ExampleConfig is the text of a new webquiz.yaml (server_config.yaml.example
// of WebQuiz 1.19).
//
//go:embed server_config.yaml.example
var ExampleConfig string

// ServerConfig is the "server" section.
type ServerConfig struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	IncludeIPv6 bool   `yaml:"include_ipv6"`
	URLFormat   string `yaml:"url_format"`
}

// PathsConfig is the "paths" section.
type PathsConfig struct {
	QuizzesDir string `yaml:"quizzes_dir"`
	LogsDir    string `yaml:"logs_dir"`
	CSVDir     string `yaml:"csv_dir"`
	StaticDir  string `yaml:"static_dir"`
}

// AdminConfig is the "admin" section. An empty MasterKey means the admin
// pages are off (Python None).
type AdminConfig struct {
	MasterKey  string   `yaml:"master_key"`
	TrustedIPs []string `yaml:"trusted_ips"`
}

// RegistrationConfig is the "registration" section. An empty UsernameLabel
// means the label of the interface language.
type RegistrationConfig struct {
	Fields         []string `yaml:"fields"`
	DisabledFields []string `yaml:"disabled_fields"`
	Approve        bool     `yaml:"approve"`
	UsernameLabel  string   `yaml:"username_label"`
}

// DownloadableQuiz is one entry of the "quizzes" list. An empty
// AuthHeaderToken means the archive is downloaded without a token.
type DownloadableQuiz struct {
	Name            string `yaml:"name"`
	DownloadPath    string `yaml:"download_path"`
	Folder          string `yaml:"folder"`
	AuthHeaderToken string `yaml:"auth_header_token,omitempty"`
}

// CheckerTemplate is one entry of the "checker_templates" list.
type CheckerTemplate struct {
	Name string `yaml:"name"`
	Code string `yaml:"code"`
}

// TunnelServerConfig is the "tunnel.config" subsection: the tunnel server's
// settings given locally instead of fetched from the server.
type TunnelServerConfig struct {
	Username        string `yaml:"username"`
	SocketDirectory string `yaml:"socket_directory"`
	BaseURL         string `yaml:"base_url"`
}

// TunnelConfig is the "tunnel" section. Empty strings mean "not set".
type TunnelConfig struct {
	Server     string              `yaml:"server"`
	PublicKey  string              `yaml:"public_key"`
	PrivateKey string              `yaml:"private_key"`
	SocketName string              `yaml:"socket_name"`
	Config     *TunnelServerConfig `yaml:"config"`
}

// ResolveKeyPaths makes relative key paths relative to the program's folder
// (WEBQUIZ_BINARY_DIR). Load calls it; the Python TunnelConfig did it when
// it was made.
func (t *TunnelConfig) ResolveKeyPaths() {
	t.PublicKey = ResolvePath(t.PublicKey)
	t.PrivateKey = ResolvePath(t.PrivateKey)
}

// Config is the whole server configuration (WebQuizConfig). The Python
// version kept the quizzes and checker templates in small wrapper classes;
// here they are plain lists.
type Config struct {
	Language                 string             `yaml:"language"`
	ExtraAnswersWithUsersCSV bool               `yaml:"extra_answers_with_users_csv"`
	Server                   ServerConfig       `yaml:"server"`
	Paths                    PathsConfig        `yaml:"paths"`
	Admin                    AdminConfig        `yaml:"admin"`
	Registration             RegistrationConfig `yaml:"registration"`
	Quizzes                  []DownloadableQuiz `yaml:"quizzes"`
	Tunnel                   TunnelConfig       `yaml:"tunnel"`
	CheckerTemplates         []CheckerTemplate  `yaml:"checker_templates"`

	// ConfigPath is the file the config was loaded from ("" when none).
	ConfigPath string `yaml:"-"`
	// LoadWarnings are messages for the log that Load found (the Python
	// version logged them while loading; for example about "language: ru").
	LoadWarnings []string `yaml:"-"`
}

// Default returns the default configuration. The default folders are
// relative to the program's folder when WEBQUIZ_BINARY_DIR is set.
func Default() *Config {
	return &Config{
		Language: "uk",
		Server: ServerConfig{
			Host:      "0.0.0.0",
			Port:      8080,
			URLFormat: "http://{IP}:{PORT}/",
		},
		Paths: PathsConfig{
			QuizzesDir: ResolvePath("quizzes"),
			LogsDir:    ResolvePath("logs"),
			CSVDir:     ResolvePath("data"),
			StaticDir:  ResolvePath("static"),
		},
		Admin:            AdminConfig{TrustedIPs: []string{"127.0.0.1"}},
		Registration:     RegistrationConfig{Fields: []string{}, DisabledFields: []string{}},
		Quizzes:          []DownloadableQuiz{},
		CheckerTemplates: []CheckerTemplate{},
	}
}

// ResolvePath returns a relative path inside the program's folder when
// WEBQUIZ_BINARY_DIR is set (the binary sets it to its own folder). Empty and
// absolute paths, and every path in a source run, stay as they are.
func ResolvePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if binaryDir := os.Getenv("WEBQUIZ_BINARY_DIR"); binaryDir != "" {
		return filepath.Join(binaryDir, path)
	}
	return path
}

// EnsureDir makes the folder (and its parents) when it does not exist and
// returns the path.
func EnsureDir(path string) (string, error) {
	return path, os.MkdirAll(path, 0o755)
}
