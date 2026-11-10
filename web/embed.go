// Package web holds the built frontend pages. Run "make web" before any Go
// build, because web/dist is not committed.
package web

import "embed"

// Dist is the content of web/dist: the five HTML pages and assets/.
//
//go:embed all:dist
var Dist embed.FS
