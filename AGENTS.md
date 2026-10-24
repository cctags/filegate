# Repository Guidelines

## Project Structure & Module Organization

Filegate uses Go 1.27.3+, Linux/IPv4, and only the standard library. `cmd/filegate/` contains startup, CLI parsing, build metadata, and tests. `internal/gate/` contains configuration/reload, state, local administration, HTTP handlers, previews, and tests. `internal/logging/` provides colored console output, file rotation and retention. HTML/CSS is embedded in the handler template.

## Build, Test, and Development Commands

- `make build`: produce the static executable `tmp/filegate` with `CGO_ENABLED=0`, `-trimpath`, and `-s -w`; embed version and UTC build time.
- `make build VERSION=v0.1.0`: override the release version.
- `make test`: run all tests with the race detector; requires a C compiler and permission to create local Unix sockets.
- `make vet`: run Go’s static checks.
- `make vulncheck`: scan product packages with separately installed `govulncheck` (network required; development tool only).
- `gofmt -w cmd internal`: format Go sources.

Keep caches and temporary files in repository `tmp/`, never `/tmp`. Keep runtime data and binaries untracked.

## CLI & Local Operation

Use bare action commands with double-dash options. Keep `--help` and `--version` as options; reject prefixed actions such as `--serve` and single-dash options:

```bash
./tmp/filegate --config tmp/demo/config.json init
./tmp/filegate --config tmp/demo/config.json serve
./tmp/filegate --config tmp/demo/config.json code ab --ttl 30m
./tmp/filegate --help
./tmp/filegate --version
```

Choose one action per invocation. Administration uses a restricted Unix socket, never Web management.

Production deployments use a dedicated non-login system user, normally `filegate`; development may use the developer account. Keep binaries and configuration administrator-owned, content read-only to the service, and state/logs writable. Blacklist replacement requires write access to its parent directory; prefer `state/blacklist.txt` in deployments. Reject `serve` with effective UID 0 after argument validation and before configuration or side effects; other actions remain available to root. Do not auto-drop privileges or require a specific non-root username.

## Coding Style & Testing

Use idiomatic Go, `gofmt`, descriptive names, explicit errors, and `*_test.go` files with `Test...` functions. Test CLI validation, concurrent code consumption/reload, directory isolation, expiry, blacklisting, persistence failures, log rotation/retention and credential redaction. Validate arguments before side effects. No coverage threshold is established.

Web UI and application errors use the embedded Chinese/English dictionaries in `internal/gate/language.go`. Keep translations complete, preserve template escaping, and test language negotiation and preference cookies without changing authentication or file contents. CLI and logs remain English.

## Security & State

Read `SECURITY_REVIEW.md` before security changes. Keep client reads/writes outside the state lock; recheck authorization and expiry after reading login bodies. Enforce IPv4-only listeners, clients and blacklists. Reject root, duplicate and nested actual directory mappings at startup/reload. Preserve `os.Root` confinement, `crypto/rand` credentials, HMAC storage, and authorization on every file request. Persist code consumption before issuing sessions. Keep sessions in memory; serialize state writes in one server process. Keep configuration, blacklist, state, logs, and TLS private keys outside the content root. Never log credentials, bodies or query strings. Link GET/HEAD requests must not consume codes or create sessions; keep preparation bounded and memory-only, store digests, require cookie/IP/CSRF on POST and reuse durable consumption. Keep URL credentials out of confirmation pages and language links; preserve the origin-only referrer policy on `/access`. Keep ANSI colors out of files. Logging changes require restart. Use HTTP only on trusted networks. Preserve connection/request/download limits, single-range checks and rolling write deadlines; keep their fixed values documented in help and README.

SIGHUP reloads mappings and runtime limits. Validate before switching; persist revocations first. Keep old configuration on failure. Deployment paths require restart. Recheck authorization when opening files during reload.

## Commit & Pull Request Guidelines

Use Linux kernel-style subjects, e.g. `cli: Add version reporting`. Explain the problem, change, and validation. Do not add `Signed-off-by`. Keep commits focused; describe compatibility changes in pull requests.

## Documentation Maintenance

Read `SESSION_STATE.md` when resuming development. Before handing off or committing relevant changes, update `README.md` for behavior, configuration, CLI/help and examples; `AGENTS.md` for development rules; and `SESSION_STATE.md` for milestones, decisions, validation and pending work. Update only affected documents. Distinguish proposals from implemented behavior, record skipped checks, and never copy runtime secrets. Record commit references as dated baselines; verify actual HEAD with Git instead of embedding a document’s own commit hash.
