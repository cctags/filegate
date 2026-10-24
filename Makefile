export TMPDIR := $(CURDIR)/tmp/test-tmp
export GOTMPDIR := $(CURDIR)/tmp/build
export GOCACHE := $(CURDIR)/tmp/go-cache

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
FUZZ ?= FuzzStripJSONComments
FUZZTIME ?= 30s
RELEASE_ARCH ?= amd64

.PHONY: prepare build test test-integration test-browser test-release dist fuzz vet vulncheck
prepare:
	mkdir -p tmp/test-tmp tmp/build tmp/go-cache

build: prepare
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" -o tmp/filegate ./cmd/filegate

test: prepare
	go test -race ./cmd/... ./internal/...

test-integration: prepare
	FILEGATE_INTEGRATION=1 go test -race -count=1 ./cmd/filegate -run '^TestProcess'

test-browser: build
	node --test tests/cdp_test.cjs
	node tests/browser.cjs

test-release: prepare
	bash tests/release.sh

dist: prepare
	bash scripts/package-release.sh "$(VERSION)" "$(RELEASE_ARCH)"

fuzz: prepare
	go test ./internal/gate -run '^$$' -fuzz '^$(FUZZ)$$' -fuzztime=$(FUZZTIME) -parallel=2

vet: prepare
	go vet ./cmd/... ./internal/...

# govulncheck is an optional development tool, not a runtime dependency.
vulncheck: prepare
	@command -v govulncheck >/dev/null || { echo "Install govulncheck and add it to PATH; see README.md" >&2; exit 1; }
	GOMODCACHE="$(CURDIR)/tmp/go-mod-cache" XDG_CACHE_HOME="$(CURDIR)/tmp/cache" govulncheck ./cmd/... ./internal/...
