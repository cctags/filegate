export TMPDIR := $(CURDIR)/tmp/test-tmp
export GOTMPDIR := $(CURDIR)/tmp/build
export GOCACHE := $(CURDIR)/tmp/go-cache

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: prepare build test vet vulncheck
prepare:
	mkdir -p tmp/test-tmp tmp/build tmp/go-cache

build: prepare
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" -o tmp/filegate ./cmd/filegate

test: prepare
	go test -race ./...

vet: prepare
	go vet ./...

# govulncheck is an optional development tool, not a runtime dependency.
vulncheck: prepare
	@command -v govulncheck >/dev/null || { echo "Install govulncheck and add it to PATH; see README.md" >&2; exit 1; }
	GOMODCACHE="$(CURDIR)/tmp/go-mod-cache" XDG_CACHE_HOME="$(CURDIR)/tmp/cache" govulncheck ./cmd/... ./internal/...
