# WebQuiz build commands. The Go binary embeds the built web pages, so
# "web" must run before any Go build or test that needs the pages.

VERSION := $(shell cat VERSION)
GO_LDFLAGS := -s -w
DIST := dist

.PHONY: web build test test-web e2e dist clean

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

# Cross-compile every release binary into dist/ (Task 18 adds zips and checks).
dist: web
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(GO_LDFLAGS)" -o $(DIST)/webquiz-linux ./cmd/webquiz
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(GO_LDFLAGS)" -o $(DIST)/webquiz-linux-arm64 ./cmd/webquiz
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "$(GO_LDFLAGS)" -o $(DIST)/webquiz-linux-arm ./cmd/webquiz
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(GO_LDFLAGS)" -o $(DIST)/webquiz-macos-intel ./cmd/webquiz
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(GO_LDFLAGS)" -o $(DIST)/webquiz-macos-apple-silicon ./cmd/webquiz
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(GO_LDFLAGS)" -o $(DIST)/webquiz-windows.exe ./cmd/webquiz

clean:
	rm -rf bin $(DIST) web/dist e2e/.bin
