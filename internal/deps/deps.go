// Package deps lists the modules the project depends on, so that
// "go mod tidy" keeps them in go.mod while the packages that use them
// are still being written in separate branches. Nothing imports this package.
// Task 20 of the migration plan removes it.
package deps

import (
	_ "github.com/coder/websocket"
	_ "github.com/yuin/goldmark"
	_ "github.com/yuin/goldmark/extension"
	_ "go.starlark.net/lib/math"
	_ "go.starlark.net/starlark"
	_ "go.starlark.net/syntax"
	_ "golang.org/x/crypto/ssh"
	_ "gopkg.in/yaml.v3"
)
