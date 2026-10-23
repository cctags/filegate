# Repository Guidelines

## Project Structure & Module Organization

Filegate is a Linux-only file server using Go 1.25+ and the standard library exclusively. `cmd/filegate/` contains startup, long-option parsing, build metadata, and CLI tests. `internal/gate/` contains configuration/reload, persistent state, Unix socket administration, HTTP handlers, and security tests. HTML/CSS lives in the handler’s template. `README.md` documents operation.

## Build, Test, and Development Commands

- `make build`: produce the static executable `tmp/filegate` with `CGO_ENABLED=0`, `-trimpath`, and `-s -w`; embed version and UTC build time.
- `make build VERSION=v0.1.0`: override the release version.
- `make test`: run all tests with the race detector; requires a C compiler and permission to create local Unix sockets.
- `make vet`: run Go’s static checks.
- `gofmt -w cmd internal`: format Go sources.

Use the Makefile to keep build caches and temporary files under this repository’s `tmp/`; do not use `/tmp`. Keep runtime data and binaries untracked.

## CLI & Local Operation

Use double-dash long options exclusively; positional commands and single-dash options are unsupported. Examples:

```bash
./tmp/filegate --config tmp/demo/config.json --init
./tmp/filegate --config tmp/demo/config.json --serve
./tmp/filegate --config tmp/demo/config.json --code ab --ttl 30m
./tmp/filegate --help
./tmp/filegate --version
```

Choose exactly one action per invocation. Administration uses a permission-restricted local Unix socket, never a Web management interface.

## Coding Style & Testing

Follow idiomatic Go and `gofmt`. Use descriptive names, error handling, and `*_test.go` files with `Test...` functions. No coverage threshold is established. Test CLI validation, concurrent code consumption/reload, directory isolation, session expiry, blacklisting, and persistence failures. Validate arguments before side effects. Update help and README examples with CLI changes.

## Security & State

Preserve `os.Root` directory confinement, `crypto/rand` credentials, HMAC code storage, and authorization on every file request. Persist code consumption before issuing sessions. Keep sessions in memory and state writes serialized by the single server process. Store configuration, blacklist, state, and TLS private keys outside the content root. Never log access codes, cookies, or session identifiers. HTTP is intended for trusted networks; HTTPS is optional.

SIGHUP reloads mappings and runtime limits. Validate before switching; persist revocations first. Keep old configuration on failure. Deployment paths require restart. Recheck authorization when opening files during reload.

## Commit & Pull Request Guidelines

Use Linux kernel-style subjects: `cli: Add version reporting` or `build: Strip debug information`. Explain the problem, change, and validation in the body. Do not add `Signed-off-by` trailers. Keep commits focused; describe relevant configuration or CLI compatibility changes in pull requests.
