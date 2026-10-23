# Repository Guidelines

## Project Structure & Module Organization

Filegate uses Go 1.25+, Linux, and only the standard library. `cmd/filegate/` contains startup, CLI parsing, build metadata, and tests. `internal/gate/` contains configuration/reload, state, local administration, HTTP handlers, previews, and tests. `internal/logging/` provides colored console output, file rotation and retention. HTML/CSS is embedded in the handler template.

## Build, Test, and Development Commands

- `make build`: produce the static executable `tmp/filegate` with `CGO_ENABLED=0`, `-trimpath`, and `-s -w`; embed version and UTC build time.
- `make build VERSION=v0.1.0`: override the release version.
- `make test`: run all tests with the race detector; requires a C compiler and permission to create local Unix sockets.
- `make vet`: run Go’s static checks.
- `gofmt -w cmd internal`: format Go sources.

Keep caches and temporary files in repository `tmp/`, never `/tmp`. Keep runtime data and binaries untracked.

## CLI & Local Operation

Use double-dash long options; positional commands and single-dash options are unsupported:

```bash
./tmp/filegate --config tmp/demo/config.json --init
./tmp/filegate --config tmp/demo/config.json --serve
./tmp/filegate --config tmp/demo/config.json --code ab --ttl 30m
./tmp/filegate --help
./tmp/filegate --version
```

Choose one action per invocation. Administration uses a restricted Unix socket, never Web management.

## Coding Style & Testing

Use idiomatic Go, `gofmt`, descriptive names, explicit errors, and `*_test.go` files with `Test...` functions. Test CLI validation, concurrent code consumption/reload, directory isolation, expiry, blacklisting, persistence failures, log rotation/retention and credential redaction. Validate arguments before side effects. No coverage threshold is established.

## Security & State

Preserve `os.Root` confinement, `crypto/rand` credentials, HMAC storage, and authorization on every file request. Persist code consumption before issuing sessions. Keep sessions in memory; serialize state writes in one server process. Keep configuration, blacklist, state, logs, and TLS private keys outside the content root. Never log credentials, bodies or query strings. Keep ANSI colors out of files. Logging changes require restart. Use HTTP only on trusted networks.

SIGHUP reloads mappings and runtime limits. Validate before switching; persist revocations first. Keep old configuration on failure. Deployment paths require restart. Recheck authorization when opening files during reload.

## Commit & Pull Request Guidelines

Use Linux kernel-style subjects, e.g. `cli: Add version reporting`. Explain the problem, change, and validation. Do not add `Signed-off-by`. Keep commits focused; describe compatibility changes in pull requests.

## Documentation Maintenance

Read `SESSION_STATE.md` when resuming development. Before handing off or committing relevant changes, update `README.md` for behavior, configuration, CLI/help and examples; `AGENTS.md` for development rules; and `SESSION_STATE.md` for milestones, decisions, validation and pending work. Update only affected documents. Distinguish proposals from implemented behavior, record skipped checks, and never copy runtime secrets. Record commit references as dated baselines; verify actual HEAD with Git instead of embedding a document’s own commit hash.
