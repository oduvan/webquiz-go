// Package config holds the server configuration (webquiz.yaml).
//
// It is a port of webquiz/config.py and of the config parts of
// webquiz/server.py (WebQuiz 1.19):
//
//   - Config and its parts have the same fields as WebQuizConfig; the yaml
//     tags are the keys of webquiz.yaml. Default gives the defaults.
//   - Load reads a config file like load_config_from_yaml. A missing or broken
//     file gives the defaults together with the error, so the caller can log
//     it and go on, as the Python version did. Values are read with PyYAML's
//     rules (pyval.LoadYAML), so "approve: yes" is true.
//   - LoadWithOverrides is load_config_with_overrides: it finds or makes the
//     config file (DefaultConfigPath, CreateDefaultConfigFile), loads it and
//     applies the command line values and WEBQUIZ_MASTER_KEY (ApplyOverrides).
//   - Validate is _validate_config_data: the checks run before the admin saves
//     a config.
//   - MergeYAML puts a partial update from the config form into the file's
//     text and keeps everything else of the file as it was: comments, blank
//     lines, quotes, spacing (the Python version used ruamel.yaml for this).
//   - ResolvePath makes a default path relative to the program's folder
//     (WEBQUIZ_BINARY_DIR), EnsureDir makes a folder, ExampleConfig is the
//     text of a new webquiz.yaml.
package config
