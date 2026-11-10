// Package web holds the built frontend pages. Run "make web" before any Go
// build, because web/dist is not committed.
package web

import "embed"

// Dist is the content of web/dist: the five HTML pages and assets/, with a
// gzip copy (name.gz) of most files (web/build/gzip.ts).
//
//go:embed all:dist
var Dist embed.FS
