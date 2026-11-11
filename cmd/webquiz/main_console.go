//go:build !desktop

package main

import "os"

// The console version: the program as it always was (see the package
// comment).
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
