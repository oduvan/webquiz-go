package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeConfig writes a webquiz.yaml into a new temporary folder.
func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "webquiz.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// load reads a config file and fails the test when the file does not load.
func load(t *testing.T, text string) *Config {
	t.Helper()
	cfg, err := Load(writeConfig(t, text))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// noBinaryDir makes paths relative to the working folder, as in a source run.
func noBinaryDir(t *testing.T) {
	t.Helper()
	t.Setenv("WEBQUIZ_BINARY_DIR", "")
}

// py: tests/test_config_loading.py::test_ensure_directory_exists
func TestEnsureDirectoryExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test_dir")
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s should not exist yet", dir)
	}
	got, err := EnsureDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("%s is not a folder: %v", dir, err)
	}
	if got != dir {
		t.Errorf("EnsureDir returned %q, want %q", got, dir)
	}
}

// py: tests/test_config_loading.py::test_ensure_directory_exists_already_exists
func TestEnsureDirectoryExistsAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	got, err := EnsureDir(dir)
	if err != nil || got != dir {
		t.Errorf("EnsureDir(existing) = %q, %v", got, err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("%s is not a folder: %v", dir, err)
	}
}

// py: tests/test_config_loading.py::test_resolve_path_relative_to_binary_absolute_path
func TestResolvePathAbsolutePath(t *testing.T) {
	t.Setenv("WEBQUIZ_BINARY_DIR", "/test/binary/dir")
	absPath := "/absolute/path/to/something"
	if got := ResolvePath(absPath); got != absPath {
		t.Errorf("ResolvePath(%q) = %q", absPath, got)
	}
}

// py: tests/test_config_loading.py::test_resolve_path_relative_to_binary_empty_path
func TestResolvePathEmptyPath(t *testing.T) {
	// Python also passed None through; Go has only the empty string.
	t.Setenv("WEBQUIZ_BINARY_DIR", "/test/binary/dir")
	if got := ResolvePath(""); got != "" {
		t.Errorf(`ResolvePath("") = %q`, got)
	}
}

// py: tests/test_config_loading.py::test_resolve_path_relative_to_binary_no_binary_dir
func TestResolvePathNoBinaryDir(t *testing.T) {
	t.Setenv("WEBQUIZ_BINARY_DIR", "")
	os.Unsetenv("WEBQUIZ_BINARY_DIR")
	if got := ResolvePath("relative/path"); got != "relative/path" {
		t.Errorf("ResolvePath = %q", got)
	}
}

// py: tests/test_config_loading.py::test_resolve_path_relative_to_binary_with_binary_dir
func TestResolvePathWithBinaryDir(t *testing.T) {
	t.Setenv("WEBQUIZ_BINARY_DIR", "/test/binary/dir")
	want := filepath.Join("/test/binary/dir", "relative/path")
	if got := ResolvePath("relative/path"); got != want {
		t.Errorf("ResolvePath = %q, want %q", got, want)
	}
}

// py: tests/test_config_loading.py::test_read_package_resource
func TestReadPackageResource(t *testing.T) {
	// The Python test read server_config.yaml.example from the package; the
	// Go binary embeds the same file.
	content := ExampleConfig
	if len(content) == 0 {
		t.Fatal("ExampleConfig is empty")
	}
	if !strings.Contains(content, "server:") && !strings.Contains(content, "admin:") {
		t.Errorf("ExampleConfig does not look like the example config:\n%s", content)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_empty_file
func TestLoadConfigFromYAMLEmptyFile(t *testing.T) {
	cfg := load(t, "")
	if cfg.Server.Port != 8080 || cfg.Server.Host != "0.0.0.0" {
		t.Errorf("an empty file should give the defaults, got %+v", cfg.Server)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_with_server_config
func TestLoadConfigFromYAMLWithServerConfig(t *testing.T) {
	cfg := load(t, "server:\n  host: 127.0.0.1\n  port: 9090\n")
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 9090 {
		t.Errorf("Server = %+v", cfg.Server)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_with_paths
func TestLoadConfigFromYAMLWithPaths(t *testing.T) {
	cfg := load(t, "paths:\n  quizzes_dir: custom_quizzes\n  logs_dir: custom_logs\n  csv_dir: custom_data\n  static_dir: custom_static\n")
	want := PathsConfig{QuizzesDir: "custom_quizzes", LogsDir: "custom_logs", CSVDir: "custom_data", StaticDir: "custom_static"}
	if cfg.Paths != want {
		t.Errorf("Paths = %+v, want %+v", cfg.Paths, want)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_with_admin
func TestLoadConfigFromYAMLWithAdmin(t *testing.T) {
	cfg := load(t, "admin:\n  master_key: test_key_123\n  trusted_ips:\n  - 192.168.1.1\n  - 10.0.0.1\n")
	if cfg.Admin.MasterKey != "test_key_123" {
		t.Errorf("MasterKey = %q", cfg.Admin.MasterKey)
	}
	if !reflect.DeepEqual(cfg.Admin.TrustedIPs, []string{"192.168.1.1", "10.0.0.1"}) {
		t.Errorf("TrustedIPs = %v", cfg.Admin.TrustedIPs)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_with_registration
func TestLoadConfigFromYAMLWithRegistration(t *testing.T) {
	cfg := load(t, "registration:\n  approve: true\n  fields:\n  - grade\n  - school\n  username_label: Student Name\n")
	r := cfg.Registration
	if !reflect.DeepEqual(r.Fields, []string{"grade", "school"}) || !r.Approve || r.UsernameLabel != "Student Name" {
		t.Errorf("Registration = %+v", r)
	}
	// disabled_fields is an empty list when it is not set
	if r.DisabledFields == nil || len(r.DisabledFields) != 0 {
		t.Errorf("DisabledFields = %#v, want an empty list", r.DisabledFields)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_with_disabled_fields
func TestLoadConfigFromYAMLWithDisabledFields(t *testing.T) {
	cfg := load(t, "registration:\n  disabled_fields:\n  - School\n  - Teacher\n  fields:\n  - Grade\n")
	if !reflect.DeepEqual(cfg.Registration.Fields, []string{"Grade"}) ||
		!reflect.DeepEqual(cfg.Registration.DisabledFields, []string{"School", "Teacher"}) {
		t.Errorf("Registration = %+v", cfg.Registration)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_with_downloadable_quizzes
func TestLoadConfigFromYAMLWithDownloadableQuizzes(t *testing.T) {
	cfg := load(t, `quizzes:
- download_path: https://example.com/quiz1.zip
  folder: quiz1
  name: Test Quiz 1
- download_path: https://example.com/quiz2.zip
  folder: quiz2
  name: Test Quiz 2
`)
	if len(cfg.Quizzes) != 2 {
		t.Fatalf("Quizzes = %+v", cfg.Quizzes)
	}
	first := cfg.Quizzes[0]
	if first.Name != "Test Quiz 1" || first.DownloadPath != "https://example.com/quiz1.zip" || first.Folder != "quiz1" {
		t.Errorf("first quiz = %+v", first)
	}
	if cfg.Quizzes[1].Name != "Test Quiz 2" {
		t.Errorf("second quiz = %+v", cfg.Quizzes[1])
	}
}

// py: tests/test_config_loading.py::test_load_config_with_overrides_cli_overrides
func TestLoadConfigWithOverridesCLIOverrides(t *testing.T) {
	path := writeConfig(t, "admin:\n  master_key: file_key\nserver:\n  host: 0.0.0.0\n  port: 8080\n")
	port, key := 9999, "cli_key"
	cfg, err := LoadWithOverrides(path, Overrides{Port: &port, MasterKey: &key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 9999 || cfg.Admin.MasterKey != "cli_key" {
		t.Errorf("command line values should win: %+v %+v", cfg.Server, cfg.Admin)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Host = %q, the file value should stay", cfg.Server.Host)
	}
	if cfg.ConfigPath != path {
		t.Errorf("ConfigPath = %q, want %q", cfg.ConfigPath, path)
	}
}

// py: tests/test_config_loading.py::test_load_config_with_overrides_env_variable
func TestLoadConfigWithOverridesEnvVariable(t *testing.T) {
	noBinaryDir(t)
	t.Setenv("WEBQUIZ_MASTER_KEY", "env_key_123")
	t.Chdir(t.TempDir()) // a clean folder: no webquiz.yaml from elsewhere
	cfg, err := LoadWithOverrides("", Overrides{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Admin.MasterKey != "env_key_123" {
		t.Errorf("MasterKey = %q", cfg.Admin.MasterKey)
	}
}

// py: tests/test_config_loading.py::test_load_config_with_overrides_no_config_file
func TestLoadConfigWithOverridesNoConfigFile(t *testing.T) {
	// With no file found, a webquiz.yaml is made from the example (port 8080) and loaded.
	noBinaryDir(t)
	t.Setenv("WEBQUIZ_MASTER_KEY", "")
	dir := t.TempDir()
	t.Chdir(dir)
	cfg, err := LoadWithOverrides("", Overrides{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Server.Port != 8080 {
		t.Fatalf("config = %+v", cfg)
	}
	written, err := os.ReadFile(filepath.Join(dir, "webquiz.yaml"))
	if err != nil || string(written) != ExampleConfig {
		t.Errorf("webquiz.yaml should be the example config: %v", err)
	}
	if cfg.ConfigPath != filepath.Join(dir, "webquiz.yaml") {
		t.Errorf("ConfigPath = %q", cfg.ConfigPath)
	}
}

// py: tests/test_config_loading.py::test_dataclass_defaults
func TestDataclassDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Server.Host != "0.0.0.0" || cfg.Server.Port != 8080 {
		t.Errorf("Server = %+v", cfg.Server)
	}
	if cfg.Paths.QuizzesDir == "" || cfg.Paths.LogsDir == "" {
		t.Errorf("Paths = %+v", cfg.Paths)
	}
	if cfg.Admin.MasterKey != "" || !reflect.DeepEqual(cfg.Admin.TrustedIPs, []string{"127.0.0.1"}) {
		t.Errorf("Admin = %+v", cfg.Admin)
	}
	r := cfg.Registration
	if r.Fields == nil || len(r.Fields) != 0 || r.DisabledFields == nil || len(r.DisabledFields) != 0 || r.Approve || r.UsernameLabel != "" {
		t.Errorf("Registration = %#v", r)
	}
}

// py: tests/test_config_loading.py::test_load_config_from_yaml_with_private_quiz_tokens
func TestLoadConfigFromYAMLWithPrivateQuizTokens(t *testing.T) {
	cfg := load(t, `quizzes:
- auth_header_token: github_pat_example
  download_path: https://api.github.com/repos/me/quizzes/zipball/master
  folder: data
  name: Private Quiz
- download_path: https://example.com/quiz.zip
  folder: data
  name: Public Quiz
`)
	if cfg.Quizzes[0].AuthHeaderToken != "github_pat_example" {
		t.Errorf("token = %q", cfg.Quizzes[0].AuthHeaderToken)
	}
	if cfg.Quizzes[1].AuthHeaderToken != "" {
		t.Errorf("a quiz without a token should have none, got %q", cfg.Quizzes[1].AuthHeaderToken)
	}
}

// py: tests/test_language.py::test_default_language_is_uk
func TestDefaultLanguageIsUk(t *testing.T) {
	if Default().Language != "uk" {
		t.Errorf("Language = %q", Default().Language)
	}
}

// py: tests/test_language.py::test_load_language_from_yaml
func TestLoadLanguageFromYAML(t *testing.T) {
	if cfg := load(t, "language: en\n"); cfg.Language != "en" {
		t.Errorf("Language = %q", cfg.Language)
	}
}

// py: tests/test_language.py::test_load_language_uk_from_yaml
func TestLoadLanguageUkFromYAML(t *testing.T) {
	if cfg := load(t, "language: uk\n"); cfg.Language != "uk" {
		t.Errorf("Language = %q", cfg.Language)
	}
}

// py: tests/test_language.py::test_missing_language_defaults_to_uk
func TestMissingLanguageDefaultsToUk(t *testing.T) {
	if cfg := load(t, "server:\n  port: 9999\n"); cfg.Language != "uk" {
		t.Errorf("Language = %q", cfg.Language)
	}
}

func TestRussianLanguageFallsBackToUkrainian(t *testing.T) {
	cfg := load(t, "language: ru\n")
	if cfg.Language != "uk" {
		t.Errorf("Language = %q", cfg.Language)
	}
	if want := []string{"російська мова не підтримується, але ти москалику попався"}; !reflect.DeepEqual(cfg.LoadWarnings, want) {
		t.Errorf("LoadWarnings = %q", cfg.LoadWarnings)
	}
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want a not-found error", err)
	}
	if cfg == nil || cfg.Server.Port != 8080 {
		t.Errorf("a missing file should still give the defaults, got %+v", cfg)
	}
}

func TestLoadBrokenFileGivesDefaults(t *testing.T) {
	// Python logged the error and used the defaults for all of these.
	broken := []string{
		"server: [1\n",
		"- a\n- b\n",
		"just text\n",
		"server:\n",
		"server:\n  hots: 1\n",
		"registration:\n",
		"registration:\n  aprove: true\n",
		"paths: [a]\n",
		"quizzes:\n- name: a\n",
		"quizzes: {a: 1}\n",
		"tunnel:\n",
		"tunnel:\n  server: x\n  unknown: 1\n",
		"tunnel:\n  config:\n    username: u\n",
		"tunnel:\n  config:\n",
		"checker_templates:\n- name: x\n",
		"registration:\n  fields: Grade\n",
		"server:\n  port: abc\n",
		"server:\n  port: 80.5\n",
		"admin:\n  trusted_ips:\n  - [1, 2]\n",
	}
	for _, text := range broken {
		cfg, err := Load(writeConfig(t, "admin:\n  master_key: k\n"+text))
		if err == nil {
			t.Errorf("Load(%q) should fail", text)
		}
		if cfg == nil || cfg.Admin.MasterKey != "" || cfg.Server.Port != 8080 {
			t.Errorf("Load(%q) should give the defaults, got %+v", text, cfg)
		}
	}
}

func TestLoadConvertsValuesLikePython(t *testing.T) {
	noBinaryDir(t)
	cfg := load(t, `
language: en
extra_answers_with_users_csv: yes
server:
  host: null
  port: "9090"
  include_ipv6: on
  url_format: "http://{IP}/webquiz/"
paths:
  quizzes_dir: null
admin:
  master_key: 12345
  trusted_ips: null
registration:
  approve: 1
  fields: null
  username_label: null
quizzes: null
checker_templates:
- name: Whole number
  code: "assert int(user_answer)"
  extra: ignored
`)
	if !cfg.ExtraAnswersWithUsersCSV || !cfg.Server.IncludeIPv6 || !cfg.Registration.Approve {
		t.Errorf("YAML 1.1 words and numbers count as true: %+v", cfg)
	}
	if cfg.Server.Host != "0.0.0.0" || cfg.Server.Port != 9090 || cfg.Server.URLFormat != "http://{IP}/webquiz/" {
		t.Errorf("Server = %+v", cfg.Server)
	}
	if cfg.Paths.QuizzesDir != "quizzes" {
		t.Errorf("a null path gets the default: %q", cfg.Paths.QuizzesDir)
	}
	if cfg.Admin.MasterKey != "12345" {
		t.Errorf("a number as master key is read as text: %q", cfg.Admin.MasterKey)
	}
	if !reflect.DeepEqual(cfg.Admin.TrustedIPs, []string{"127.0.0.1"}) {
		t.Errorf("TrustedIPs = %v", cfg.Admin.TrustedIPs)
	}
	if cfg.Registration.Fields == nil || cfg.Registration.UsernameLabel != "" {
		t.Errorf("Registration = %#v", cfg.Registration)
	}
	if len(cfg.Quizzes) != 0 {
		t.Errorf("Quizzes = %v", cfg.Quizzes)
	}
	if !reflect.DeepEqual(cfg.CheckerTemplates, []CheckerTemplate{{Name: "Whole number", Code: "assert int(user_answer)"}}) {
		t.Errorf("CheckerTemplates = %+v", cfg.CheckerTemplates)
	}
}

func TestLoadEmptyLists(t *testing.T) {
	cfg := load(t, "admin:\n  trusted_ips: []\n")
	if cfg.Admin.TrustedIPs == nil || len(cfg.Admin.TrustedIPs) != 0 {
		t.Errorf("an empty trusted_ips list stays empty, as in Python: %#v", cfg.Admin.TrustedIPs)
	}
}

func TestDefaultPathsUseBinaryDir(t *testing.T) {
	t.Setenv("WEBQUIZ_BINARY_DIR", "/opt/webquiz")
	cfg := Default()
	want := PathsConfig{
		QuizzesDir: filepath.Join("/opt/webquiz", "quizzes"),
		LogsDir:    filepath.Join("/opt/webquiz", "logs"),
		CSVDir:     filepath.Join("/opt/webquiz", "data"),
		StaticDir:  filepath.Join("/opt/webquiz", "static"),
	}
	if cfg.Paths != want {
		t.Errorf("Paths = %+v, want %+v", cfg.Paths, want)
	}
	// A path written in the file is not changed (it is relative to the working folder).
	if loaded := load(t, "paths:\n  quizzes_dir: my_quizzes\n"); loaded.Paths.QuizzesDir != "my_quizzes" || loaded.Paths.LogsDir != want.LogsDir {
		t.Errorf("Paths = %+v", loaded.Paths)
	}
}

func TestApplyOverrides(t *testing.T) {
	t.Setenv("WEBQUIZ_MASTER_KEY", "")
	cfg := Default()
	host, urlFormat, quizzes, logs, csvDir, static, key := "127.0.0.1", "http://{IP}/q/", "q", "l", "c", "s", "k"
	port := 7000
	ApplyOverrides(cfg, Overrides{Host: &host, Port: &port, URLFormat: &urlFormat, QuizzesDir: &quizzes,
		LogsDir: &logs, CSVDir: &csvDir, StaticDir: &static, MasterKey: &key})
	if cfg.Server != (ServerConfig{Host: "127.0.0.1", Port: 7000, URLFormat: "http://{IP}/q/"}) {
		t.Errorf("Server = %+v", cfg.Server)
	}
	if cfg.Paths != (PathsConfig{QuizzesDir: "q", LogsDir: "l", CSVDir: "c", StaticDir: "s"}) {
		t.Errorf("Paths = %+v", cfg.Paths)
	}
	if cfg.Admin.MasterKey != "k" {
		t.Errorf("MasterKey = %q", cfg.Admin.MasterKey)
	}

	// The environment variable wins over the file, but not over the command line.
	t.Setenv("WEBQUIZ_MASTER_KEY", "from-env")
	cfg = Default()
	cfg.Admin.MasterKey = "from-file"
	ApplyOverrides(cfg, Overrides{})
	if cfg.Admin.MasterKey != "from-env" {
		t.Errorf("MasterKey = %q, want from-env", cfg.Admin.MasterKey)
	}
	ApplyOverrides(cfg, Overrides{MasterKey: &key})
	if cfg.Admin.MasterKey != "k" {
		t.Errorf("MasterKey = %q, want the command line value", cfg.Admin.MasterKey)
	}
	// An empty --master-key does not count, so the environment variable wins (as in Python).
	empty := ""
	ApplyOverrides(cfg, Overrides{MasterKey: &empty})
	if cfg.Admin.MasterKey != "from-env" {
		t.Errorf("MasterKey = %q, want from-env", cfg.Admin.MasterKey)
	}
}

func TestLoadWithOverridesMakesAMissingNamedFile(t *testing.T) {
	t.Setenv("WEBQUIZ_MASTER_KEY", "")
	path := filepath.Join(t.TempDir(), "custom.yaml")
	cfg, err := LoadWithOverrides(path, Overrides{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != ExampleConfig {
		t.Errorf("the named config file should be made from the example: %v", err)
	}
	if cfg.Admin.MasterKey != "a1" {
		t.Errorf("the example config sets master_key a1, got %q", cfg.Admin.MasterKey)
	}
	if _, err := LoadWithOverrides(filepath.Join(t.TempDir(), "no", "such", "dir", "x.yaml"), Overrides{}, nil); err == nil {
		t.Error("a config file that cannot be made should give an error")
	}
}

func TestDefaultConfigPathUsesBinaryDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WEBQUIZ_BINARY_DIR", dir)
	path := DefaultConfigPath(nil)
	if path != filepath.Join(dir, "webquiz.yaml") {
		t.Errorf("DefaultConfigPath = %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the default config file should exist now: %v", err)
	}
	// An existing file is left as it is.
	if err := os.WriteFile(path, []byte("language: en\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if DefaultConfigPath(nil) != path {
		t.Error("DefaultConfigPath changed")
	}
	if data, _ := os.ReadFile(path); string(data) != "language: en\n" {
		t.Errorf("an existing config was overwritten: %q", data)
	}
	// A folder that cannot hold the file gives "".
	t.Setenv("WEBQUIZ_BINARY_DIR", filepath.Join(dir, "missing", "folder"))
	if got := DefaultConfigPath(nil); got != "" {
		t.Errorf("DefaultConfigPath = %q, want \"\"", got)
	}
}
