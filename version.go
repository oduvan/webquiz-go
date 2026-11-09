// Package webquiz holds files from the root of the repository that the
// program needs at run time. Use internal/version to read the version.
package webquiz

import _ "embed"

// VersionFile is the content of the VERSION file, as written (with its line end).
//
//go:embed VERSION
var VersionFile string
