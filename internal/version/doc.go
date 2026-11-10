// Package version tells which WebQuiz version is running and where the
// project lives.
//
//   - Version is the running version. It comes from the VERSION file at the
//     root of the repository (embedded at build time). A release build may set
//     it with -ldflags "-X github.com/oduvan/webquiz-go/internal/version.Version=2.1".
//   - RepoURL and PagesURL are the addresses of the repository and of its
//     GitHub Pages site. This is the only place in the code that names them.
//   - FileVersion runs a WebQuiz binary from disk with --version and reads the
//     version it prints ("webquiz 2.0"). The server compares it with Version
//     to show "restart required" when the binary was replaced while running
//     (the Python version read webquiz/__init__.py from disk for this).
package version
