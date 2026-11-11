# WebQuiz build commands. The Go binary embeds the built web pages, so
# "web" must run before any Go build or test that needs the pages.

VERSION := $(shell cat VERSION)
GO_LDFLAGS := -s -w
DIST := dist

.PHONY: web build build-desktop test test-web e2e dist dist-binaries dist-macos-app release-check clean

web:
	npm --prefix web ci
	npm --prefix web run build

build: web
	CGO_ENABLED=0 go build -ldflags "$(GO_LDFLAGS)" -o bin/webquiz ./cmd/webquiz

# The desktop version (tray icon) for this computer, to try it. The tray
# needs cgo on macOS only.
DESKTOP_CGO := $(if $(filter Darwin,$(shell uname)),1,0)

build-desktop: web
	CGO_ENABLED=$(DESKTOP_CGO) go build -tags desktop -ldflags "$(GO_LDFLAGS)" -o bin/webquiz-desktop ./cmd/webquiz

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
# workflow does). Each binary in RELEASE_BINARIES gets its own zip.
#
# Two versions come from the same code: the console version, and the desktop
# version with a tray icon (-tags desktop). webquiz-linux-desktop is zipped;
# webquiz-windows-desktop.exe is a Windows GUI program (no console window)
# that goes into the Windows installer. The macOS desktop app needs cgo, so
# dist-macos-app builds it on a Mac.
LDFLAGS_RELEASE := -s -w -X github.com/oduvan/webquiz-go/internal/version.Version=$(VERSION)
GO_RELEASE = CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS_RELEASE)"
GO_RELEASE_WINDOWS_GUI = CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS_RELEASE) -H windowsgui"
RELEASE_BINARIES := webquiz-linux webquiz-linux-arm64 webquiz-linux-arm webquiz-macos-intel webquiz-macos-apple-silicon webquiz-windows.exe webquiz-linux-desktop

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
	GOOS=linux GOARCH=amd64 $(GO_RELEASE) -tags desktop -o $(DIST)/webquiz-linux-desktop ./cmd/webquiz
	GOOS=windows GOARCH=amd64 $(GO_RELEASE_WINDOWS_GUI) -tags desktop -o $(DIST)/webquiz-windows-desktop.exe ./cmd/webquiz
	cd $(DIST) && for f in $(RELEASE_BINARIES); do zip -q -FS $$f.zip $$f; done

# The macOS desktop app: dist/WebQuiz.app with one universal program (Apple
# Silicon and Intel, joined with lipo), signed ad hoc, and its zip
# dist/webquiz-macos-app.zip. The tray needs cgo on macOS, so this runs only
# on a Mac with the Xcode command line tools. Like dist-binaries, it expects
# web/dist to exist.
MACOS_APP := $(DIST)/WebQuiz.app
MACOS_BUILD := $(DIST)/macos-build
GO_MACOS = CGO_ENABLED=1 GOOS=darwin MACOSX_DEPLOYMENT_TARGET=12.0 go build -trimpath -tags desktop -ldflags "$(LDFLAGS_RELEASE)"

dist-macos-app:
	@test "$$(uname)" = Darwin || { echo "dist-macos-app runs only on macOS"; exit 1; }
	rm -rf $(MACOS_APP) $(MACOS_BUILD) $(DIST)/webquiz-macos-app.zip
	mkdir -p $(MACOS_APP)/Contents/MacOS $(MACOS_APP)/Contents/Resources $(MACOS_BUILD)
	GOARCH=arm64 CC="clang -arch arm64" $(GO_MACOS) -o $(MACOS_BUILD)/webquiz-arm64 ./cmd/webquiz
	GOARCH=amd64 CC="clang -arch x86_64" $(GO_MACOS) -o $(MACOS_BUILD)/webquiz-amd64 ./cmd/webquiz
	lipo -create -output $(MACOS_APP)/Contents/MacOS/webquiz $(MACOS_BUILD)/webquiz-arm64 $(MACOS_BUILD)/webquiz-amd64
	sed 's/__VERSION__/$(VERSION)/g' packaging/macos/Info.plist > $(MACOS_APP)/Contents/Info.plist
	iconutil -c icns -o $(MACOS_APP)/Contents/Resources/WebQuiz.icns packaging/macos/WebQuiz.iconset
	codesign --force --deep -s - $(MACOS_APP)
	cd $(DIST) && ditto -c -k --keepParent WebQuiz.app webquiz-macos-app.zip
	rm -rf $(MACOS_BUILD)

# Run each binary that can run on this machine with --version and compare the
# output with the version in VERSION.
release-check:
	@test -f $(DIST)/webquiz-linux || { echo "run 'make dist' first"; exit 1; }
	@if [ "$$(go env GOOS)/$$(go env GOARCH)" = "linux/amd64" ]; then \
	  for f in webquiz-linux webquiz-linux-desktop; do \
	    out="$$($(DIST)/$$f --version)"; echo "$$f: $$out"; \
	    test "$$out" = "webquiz $(VERSION)" || { echo "wrong version output"; exit 1; }; \
	  done; \
	else echo "skipping the Linux binaries: this is not a linux/amd64 machine"; fi
	@echo "The other binaries cannot run on this machine; the build checked them."

clean:
	rm -rf bin $(DIST) web/dist e2e/.bin
