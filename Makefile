export TMPDIR := $(CURDIR)/tmp/test-tmp
export GOTMPDIR := $(CURDIR)/tmp/build
export GOCACHE := $(CURDIR)/tmp/go-cache

.PHONY: prepare build test vet
prepare:
	mkdir -p tmp/test-tmp tmp/build tmp/go-cache

build: prepare
	CGO_ENABLED=0 go build -trimpath -o tmp/filegate ./cmd/filegate

test: prepare
	go test -race ./...

vet: prepare
	go vet ./...
