package config

import (
	"fmt"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// Validate checks config data before the admin saves it, like
// _validate_config_data. Every section is optional, and an empty config
// (nil) is valid. data is what the config file holds (pyval.LoadYAML) or
// what a JSON body holds (decoded with UseNumber). It returns error messages.
func Validate(data any) []string {
	if data == nil {
		return nil
	}
	m, ok := pyval.Dict(data)
	if !ok {
		return []string{"Config must be a dictionary"}
	}

	var errors []string
	add := func(format string, args ...any) {
		errors = append(errors, fmt.Sprintf(format, args...))
	}

	if language, ok := m["language"]; ok {
		switch {
		case pyval.Equal(language, "ru"):
			add("%s", russianLanguage)
		case !pyval.Equal(language, "uk") && !pyval.Equal(language, "en"):
			add("'language' must be 'uk' or 'en'")
		}
	}

	if value, ok := m["extra_answers_with_users_csv"]; ok && !pyval.IsBool(value) {
		add("'extra_answers_with_users_csv' must be a boolean")
	}

	if value, ok := m["server"]; ok {
		if server, isDict := pyval.Dict(value); !isDict {
			add("'server' section must be a dictionary")
		} else {
			if host, ok := server["host"]; ok && !pyval.IsStr(host) {
				add("'server.host' must be a string")
			}
			if port, ok := server["port"]; ok {
				// isinstance(port, int) lets True through, as 1, like in Python.
				if !pyval.IsInt(port) {
					add("'server.port' must be an integer")
				} else if low, _ := pyval.Compare(port, 1); low < 0 {
					add("'server.port' must be between 1 and 65535")
				} else if high, _ := pyval.Compare(port, 65535); high > 0 {
					add("'server.port' must be between 1 and 65535")
				}
			}
		}
	}

	if value, ok := m["paths"]; ok {
		if paths, isDict := pyval.Dict(value); !isDict {
			add("'paths' section must be a dictionary")
		} else {
			for _, field := range []string{"quizzes_dir", "logs_dir", "csv_dir", "static_dir"} {
				if path, ok := paths[field]; ok && !pyval.IsStr(path) {
					add("'paths.%s' must be a string", field)
				}
			}
		}
	}

	if value, ok := m["admin"]; ok {
		if admin, isDict := pyval.Dict(value); !isDict {
			add("'admin' section must be a dictionary")
		} else {
			if key, ok := admin["master_key"]; ok && key != nil && !pyval.IsStr(key) {
				add("'admin.master_key' must be a string or null")
			}
			if ips, ok := admin["trusted_ips"]; ok {
				checkStringList(ips, "'admin.trusted_ips'", add)
			}
		}
	}

	if value, ok := m["registration"]; ok {
		if registration, isDict := pyval.Dict(value); !isDict {
			add("'registration' section must be a dictionary")
		} else {
			if fields, ok := registration["fields"]; ok {
				checkStringList(fields, "'registration.fields'", add)
			}
			if fields, ok := registration["disabled_fields"]; ok {
				checkStringList(fields, "'registration.disabled_fields'", add)
			}
			if approve, ok := registration["approve"]; ok && !pyval.IsBool(approve) {
				add("'registration.approve' must be a boolean")
			}
			if label, ok := registration["username_label"]; ok && !pyval.IsStr(label) {
				add("'registration.username_label' must be a string")
			}
		}
	}

	if value, ok := m["quizzes"]; ok {
		quizzes, isList := pyval.List(value)
		if !isList {
			add("'quizzes' section must be a list")
		}
		for i, item := range quizzes {
			quiz, isDict := pyval.Dict(item)
			if !isDict {
				add("Quiz %d must be a dictionary", i+1)
				continue
			}
			for _, field := range []string{"name", "download_path", "folder"} {
				fieldValue, ok := quiz[field]
				switch {
				case !ok:
					add("Quiz %d missing required field: '%s'", i+1, field)
				case !pyval.IsStr(fieldValue):
					add("Quiz %d field '%s' must be a string", i+1, field)
				}
			}
			if token, ok := quiz["auth_header_token"]; ok && !pyval.IsStr(token) {
				add("Quiz %d field 'auth_header_token' must be a string", i+1)
			}
		}
	}
	return errors
}

// checkStringList adds an error when value is not a list of text values.
func checkStringList(value any, name string, add func(string, ...any)) {
	items, isList := pyval.List(value)
	if !isList {
		add("%s must be a list", name)
		return
	}
	for _, item := range items {
		if !pyval.IsStr(item) {
			add("%s must contain only strings", name)
			return
		}
	}
}
