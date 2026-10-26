MODULE  := github.com/benyamin-git/kumiho
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT) -X $(MODULE)/internal/version.Date=$(DATE)
GRADLE ?= ./gradlew
GRADLE_FLAGS ?=

.PHONY: all build build-arm64 build-windows build-gui build-probe build-web android-setup android-aar android-aar-emulator android-debug android-test android-emulator-test android-tv-emulator-test android-release android-verify test test-integration vet fmt lint clean

all: build

build:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/kumiho-linux-amd64 ./cmd/kumiho

build-arm64:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/kumiho-linux-arm64 ./cmd/kumiho

build-windows:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/kumiho-windows-amd64.exe ./cmd/kumiho
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS) -H=windowsgui" -o dist/kumiho-gui-windows-amd64.exe ./cmd/kumiho-gui

build-gui:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS) -H=windowsgui" -o dist/kumiho-gui-windows-amd64.exe ./cmd/kumiho-gui

build-probe:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/kumiho-gui-probe-windows-amd64.exe ./cmd/kumiho-gui-probe

build-web:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/kumiho-gui-web-linux-amd64 ./cmd/kumiho-gui-web

android-setup:
	bash scripts/android-setup.sh

# Gomobile AAR for the Android app. The output is git-ignored build state that
# Gradle consumes from android/app/libs/.
android-aar:
	bash scripts/android-build-aar.sh

android-aar-emulator:
	bash scripts/android-build-aar.sh --emulator

android-debug:
	bash scripts/android-build-aar.sh
	. scripts/android-env.sh >/dev/null && cd android && $(GRADLE) $(GRADLE_FLAGS) :app:assembleDebug

android-test:
	bash scripts/android-build-aar.sh
	. scripts/android-env.sh >/dev/null && cd android && $(GRADLE) $(GRADLE_FLAGS) :app:testDebugUnitTest :app:lintDebug :app:assembleDebug

android-emulator-test:
	bash scripts/android-build-aar.sh --emulator
	GRADLE="$(GRADLE)" GRADLE_FLAGS="$(GRADLE_FLAGS)" bash scripts/android-connected-test.sh

android-tv-emulator-test:
	bash scripts/android-build-aar.sh --emulator
	GRADLE="$(GRADLE)" GRADLE_FLAGS="$(GRADLE_FLAGS)" bash scripts/android-tv-connected-test.sh

android-release:
	@test -n "$(KUMIHO_VERSION)" || { echo "usage: make android-release KUMIHO_VERSION=0.6.0 KUMIHO_EXPECT_CERT_SHA256=<sha256>" >&2; exit 2; }
	KUMIHO_VERSION="$(KUMIHO_VERSION)" KUMIHO_EXPECT_CERT_SHA256="$(KUMIHO_EXPECT_CERT_SHA256)" GRADLE_FLAGS="$(GRADLE_FLAGS)" bash scripts/android-release.sh

android-verify:
	@if [ -n "$(APK)" ]; then \
		bash scripts/android-verify-apk.sh "$(APK)"; \
	else \
		found=0; \
		for apk in dist/kumiho-v*-android-*.apk; do \
			[ -f "$$apk" ] || continue; \
			found=1; \
			bash scripts/android-verify-apk.sh "$$apk" || exit 1; \
		done; \
		[ "$$found" = 1 ] || { echo "no release APKs in dist/; run make android-release KUMIHO_VERSION=<version>" >&2; exit 2; }; \
	fi

test:
	go test ./...

test-integration:
	go test -tags integration ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

lint:
	@files="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; if [ -n "$$files" ]; then echo "gofmt required for:"; echo "$$files"; exit 1; fi
	go vet ./...

clean:
	rm -rf dist
