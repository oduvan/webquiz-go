# WebQuiz build commands. The Go binary embeds the built web pages, so
# "web" must run before any Go build or test that needs the pages.

VERSION := $(shell cat VERSION)
GO_LDFLAGS := -s -w
DIST := dist

.PHONY: web build test test-web e2e dist dist-binaries release-check clean

web:
	npm --prefix web ci
	npm --prefix web run build

build: web
	CGO_ENABLED=0 go build -ldflags "$(GO_LDFLAGS)" -o bin/webquiz ./cmd/webquiz

test: web
	go vet ./...
	go test -race ./...

test-web:
	npm --prefix web run typecheck
	npm --prefix web test

e2e: build
	npm --prefix e2e ci
	npm --prefix e2e test

# Release binaries. Every one is built with CGO_ENABLED=0 and the version set
# from the VERSION file (or VERSION=... on the command line, as the release
# workflow does).
LDFLAGS_RELEASE := -s -w -X github.com/oduvan/webquiz-go/internal/version.Version=$(VERSION)
GO_RELEASE = CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS_RELEASE)"
RELEASE_BINARIES := webquiz-linux webquiz-linux-arm64 webquiz-linux-arm webquiz-macos-intel webquiz-macos-apple-silicon webquiz-windows.exe

# "dist" builds the web pages first. "dist-binaries" expects web/dist to exist
# already (the release workflow builds it once).
dist: web dist-binaries

dist-binaries:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 $(GO_RELEASE) -o $(DIST)/webquiz-linux ./cmd/webquiz
	GOOS=linux GOARCH=arm64 $(GO_RELEASE) -o $(DIST)/webquiz-linux-arm64 ./cmd/webquiz
	GOOS=linux GOARCH=arm GOARM=6 $(GO_RELEASE) -o $(DIST)/webquiz-linux-arm ./cmd/webquiz
	GOOS=darwin GOARCH=amd64 $(GO_RELEASE) -o $(DIST)/webquiz-macos-intel ./cmd/webquiz
	GOOS=darwin GOARCH=arm64 $(GO_RELEASE) -o $(DIST)/webquiz-macos-apple-silicon ./cmd/webquiz
	GOOS=windows GOARCH=amd64 $(GO_RELEASE) -o $(DIST)/webquiz-windows.exe ./cmd/webquiz
	cd $(DIST) && for f in $(RELEASE_BINARIES); do zip -q -FS $$f.zip $$f; done

# Run each binary that can run on this machine with --version and compare the
# output with the version in VERSION.
release-check:
	@test -f $(DIST)/webquiz-linux || { echo "run 'make dist' first"; exit 1; }
	@if [ "$$(go env GOOS)/$$(go env GOARCH)" = "linux/amd64" ]; then \
	  out="$$($(DIST)/webquiz-linux --version)"; echo "$$out"; \
	  test "$$out" = "webquiz $(VERSION)" || { echo "wrong version output"; exit 1; }; \
	else echo "skipping webquiz-linux: this is not a linux/amd64 machine"; fi
	@echo "The other binaries cannot run on this machine; the build checked them."

clean:
	rm -rf bin $(DIST) web/dist e2e/.bin
